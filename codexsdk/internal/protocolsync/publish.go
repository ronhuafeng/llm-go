package protocolsync

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// PublishRequest publishes the current HEAD as a protected protocol-sync PR.
type PublishRequest struct {
	Repository, ExpectedHead, ExpectedBranch string
	AppBotID                                 int64
	API                                      PublicationAPI
	Lookuper                                 RemoteLookuper
	RepoRoot                                 string
	BaseBranch                               string
	TargetRef                                string
	TargetKind                               string
	TargetSHA                                string
	Remote                                   string
	GitHubOutputPath                         string
	RepairPending                            bool
}

// Publish conditionally publishes the validated HEAD against the native state
// observed before generation. A pending PR is never reported as integrated.
func Publish(req PublishRequest) (url string, err error) {
	defer func() {
		if err != nil {
			_ = appendGitHubOutput(req.GitHubOutputPath, map[string]string{"outcome": OutcomeFailed, "stage": "publication", "failure_category": failureCategory(err)})
		}
	}()
	return publishCandidate(req)
}

func normalizeBranchRef(ref, remote string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/remotes/"+remote+"/")
	ref = strings.TrimPrefix(ref, remote+"/")
	return ref
}

func syncBranchName(targetRef, targetSHA, baseSHA string) string {
	name := regexp.MustCompile(`^refs/(heads|tags)/`).ReplaceAllString(targetRef, "")
	if name == "" {
		name = targetSHA[:12]
	}
	name = regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-.")
	if name == "" {
		name = targetSHA[:12]
	}
	if len(name) > 64 {
		name = name[:64]
	}
	// A publication branch is stable for the same accepted base/upstream pair,
	// but a later proposal for the same upstream target after that target has
	// already been integrated must not collide with the historical merged PR
	// branch. The accepted base therefore defines the publication epoch.
	return "codex/sync-upstream-" + name + "-" + targetSHA[:12] + "-base-" + baseSHA[:12]
}

func parseSyncMetadata(body string) map[string]string {
	start := strings.Index(body, "<!-- codexsdk-upstream-sync")
	if start < 0 {
		return map[string]string{}
	}
	end := strings.Index(body[start:], "-->")
	if end < 0 {
		return map[string]string{}
	}
	parsed := map[string]string{}
	for _, line := range strings.Split(body[start:start+end], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		switch key {
		case "upstream_ref", "upstream_ref_kind", "upstream_commit", "sync_commit", "base_branch":
			parsed[key] = strings.TrimSpace(value)
		}
	}
	return parsed
}

type publicationDescription struct {
	target     BaselineIdentity
	head, base string
}

func publicationProjection(body string) publicationDescription {
	meta := parseSyncMetadata(body)
	return publicationDescription{
		target: BaselineIdentity{
			SourceRefName: meta["upstream_ref"],
			SourceRefKind: meta["upstream_ref_kind"],
			SourceCommit:  meta["upstream_commit"],
		},
		head: meta["sync_commit"],
		base: meta["base_branch"],
	}
}

func publicationProjectionMatches(body, base, targetRef, targetKind, targetSHA, head string) bool {
	got := publicationProjection(body)
	return got.base == base &&
		got.head == head &&
		got.target.SourceRefName == targetRef &&
		got.target.SourceRefKind == targetKind &&
		got.target.SourceCommit == targetSHA
}

func publicationTitle(ref string) string { return "Sync Codex protocol baseline to " + ref }

func syncMetadataBlock(landRef, targetRef, targetKind, targetSHA, syncCommit string) string {
	return fmt.Sprintf(`<!-- codexsdk-upstream-sync
upstream_ref: %s
upstream_ref_kind: %s
upstream_commit: %s
sync_commit: %s
base_branch: %s
-->`, targetRef, targetKind, targetSHA, syncCommit, landRef)
}

// updateSyncMetadata changes only the machine projection. The rest of the PR
// body is operator-owned scratchboard text and is not a correctness authority.
func updateSyncMetadata(body, landRef, targetRef, targetKind, targetSHA, syncCommit string) string {
	block := syncMetadataBlock(landRef, targetRef, targetKind, targetSHA, syncCommit)
	start := strings.Index(body, "<!-- codexsdk-upstream-sync")
	if start < 0 {
		if strings.TrimSpace(body) == "" {
			return block + "\n"
		}
		return block + "\n\n" + strings.TrimLeft(body, "\n")
	}
	relEnd := strings.Index(body[start:], "-->")
	if relEnd < 0 {
		return block + "\n\n" + body
	}
	end := start + relEnd + len("-->")
	return body[:start] + block + body[end:]
}

func publicationBody(landRef, targetRef, targetKind, targetSHA, syncCommit string) string {
	return syncMetadataBlock(landRef, targetRef, targetKind, targetSHA, syncCommit) + `

Automated upstream protocol sync.

## Description

This PR advances the checked-in protocol baseline for the selected upstream target, regenerates deterministic SDK artifacts, includes any one-run handwritten updates required by real drift, and publishes only after native checks passed.

It does not merge itself, tag, or bypass branch protection.

The exact publication identity is owned by the current Git head and its checked-in baseline metadata. The hidden block above is a recoverable projection. This visible body is operator-owned scratchboard text and may be edited without changing protocol correctness.

Integration must use the trusted Auto-forward PR path only after the exact current head has all required checks and review policy satisfied; main is then advanced to that same head by non-force fast-forward.
`
}

func appendGitHubOutput(path string, values map[string]string) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for key, value := range values {
		if _, err := fmt.Fprintf(f, "%s=%s\n", key, value); err != nil {
			return err
		}
	}
	return nil
}
