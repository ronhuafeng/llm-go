package protocolsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// PublishRequest publishes the current HEAD as a protected protocol-sync PR.
type PublishRequest struct {
	RepoRoot         string
	BaseBranch       string
	BranchPrefix     string
	TargetRef        string
	TargetKind       string
	TargetSHA        string
	Remote           string
	GitHubOutputPath string
}

// Publish pushes HEAD and creates or reuses the sync PR.
func Publish(req PublishRequest) (string, error) {
	if req.RepoRoot == "" || req.BaseBranch == "" || req.TargetRef == "" || req.TargetKind == "" || req.TargetSHA == "" {
		return "", fmt.Errorf("repo-root, base-branch, and target identity are required")
	}
	remote := req.Remote
	if remote == "" {
		remote = "origin"
	}
	prefix := req.BranchPrefix
	if prefix == "" {
		prefix = "codex/sync-upstream"
	}
	baseBranch := normalizeBranchRef(req.BaseBranch, remote)
	head, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	head = strings.TrimSpace(head)
	status, err := gitOutput(req.RepoRoot, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) != "" {
		return "", fmt.Errorf("worktree must be clean before publishing a sync PR")
	}
	if err := runGit(req.RepoRoot, "fetch", remote, "refs/heads/"+baseBranch+":refs/remotes/"+remote+"/"+baseBranch); err != nil {
		return "", err
	}
	landingSHA, err := gitOutput(req.RepoRoot, "rev-parse", remote+"/"+baseBranch)
	if err != nil {
		return "", err
	}
	parent, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD^")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(landingSHA) != strings.TrimSpace(parent) {
		return "", fmt.Errorf("landing ref %s moved to %s; HEAD parent is %s. Rerun protocol sync against current %s", baseBranch, strings.TrimSpace(landingSHA), strings.TrimSpace(parent), baseBranch)
	}
	resolved, err := ResolveUpstream(ResolveRequest{UpstreamRef: req.TargetRef})
	if err != nil {
		return "", err
	}
	if resolved.PeeledCommitSHA != req.TargetSHA {
		return "", fmt.Errorf("upstream target moved: %s resolved to %s, expected %s", req.TargetRef, resolved.PeeledCommitSHA, req.TargetSHA)
	}

	if prURL, ok, err := findExactExistingPR(baseBranch, req.TargetRef, req.TargetKind, req.TargetSHA, head); err != nil {
		return "", err
	} else if ok {
		if err := appendGitHubOutput(req.GitHubOutputPath, map[string]string{"pr_url": prURL}); err != nil {
			return "", err
		}
		return prURL, nil
	}

	syncBranch := syncBranchName(prefix, req.TargetRef, req.TargetSHA)
	if err := pushSyncBranch(req.RepoRoot, remote, syncBranch, head); err != nil {
		return "", err
	}
	prURL, prNumber, err := createOrUpdatePR(baseBranch, syncBranch, req.TargetRef, req.TargetKind, req.TargetSHA, head)
	if err != nil {
		return "", err
	}
	if err := appendGitHubOutput(req.GitHubOutputPath, map[string]string{
		"sync_branch": syncBranch,
		"sync_commit": head,
		"pr_number":   prNumber,
		"pr_url":      prURL,
	}); err != nil {
		return "", err
	}
	return prURL, nil
}

func normalizeBranchRef(ref, remote string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/remotes/"+remote+"/")
	ref = strings.TrimPrefix(ref, remote+"/")
	return ref
}

func syncBranchName(prefix, targetRef, targetSHA string) string {
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
	return strings.TrimRight(prefix, "-/") + "-" + name + "-" + targetSHA[:12]
}

func pushSyncBranch(repoRoot, remote, syncBranch, head string) error {
	_ = runGit(repoRoot, "fetch", remote, "refs/heads/"+syncBranch+":refs/remotes/"+remote+"/"+syncBranch)
	existing, err := gitOutput(repoRoot, "rev-parse", "refs/remotes/"+remote+"/"+syncBranch)
	if err == nil {
		if strings.TrimSpace(existing) == head {
			return nil
		}
		return fmt.Errorf("refusing to overwrite existing sync branch %s at %s", syncBranch, strings.TrimSpace(existing))
	}
	return runGit(repoRoot, "push", remote, "HEAD:refs/heads/"+syncBranch)
}

func findExactExistingPR(landRef, targetRef, targetKind, targetSHA, validatedCommit string) (string, bool, error) {
	cmd := exec.Command("gh", "pr", "list", "--state", "open", "--limit", "100", "--json", "number,url,body,baseRefName,headRefOid")
	out, err := cmd.Output()
	if err != nil {
		return "", false, fmt.Errorf("gh pr list: %w", err)
	}
	var prs []struct {
		Number      int    `json:"number"`
		URL         string `json:"url"`
		Body        string `json:"body"`
		BaseRefName string `json:"baseRefName"`
		HeadRefOid  string `json:"headRefOid"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return "", false, fmt.Errorf("decode gh pr list: %w", err)
	}
	var matches []string
	for _, pr := range prs {
		meta := parseSyncMetadata(pr.Body)
		if meta["upstream_commit"] != targetSHA {
			continue
		}
		if pr.BaseRefName != landRef || meta["base_branch"] != "" && meta["base_branch"] != landRef {
			return "", false, fmt.Errorf("existing sync PR for %s is not an exact publication of the validated commit: #%d", targetSHA, pr.Number)
		}
		if pr.HeadRefOid != validatedCommit || meta["sync_commit"] != "" && meta["sync_commit"] != validatedCommit {
			return "", false, fmt.Errorf("existing sync PR for %s is not an exact publication of the validated commit: #%d", targetSHA, pr.Number)
		}
		if meta["upstream_ref"] != "" && meta["upstream_ref"] != targetRef {
			return "", false, fmt.Errorf("existing sync PR for %s is not an exact publication of the validated commit: #%d", targetSHA, pr.Number)
		}
		if meta["upstream_ref_kind"] != "" && meta["upstream_ref_kind"] != targetKind {
			return "", false, fmt.Errorf("existing sync PR for %s is not an exact publication of the validated commit: #%d", targetSHA, pr.Number)
		}
		matches = append(matches, pr.URL)
	}
	if len(matches) > 1 {
		return "", false, fmt.Errorf("multiple exact sync PRs for %s", targetSHA)
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return "", false, nil
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

func createOrUpdatePR(landRef, syncBranch, targetRef, targetKind, targetSHA, syncCommit string) (string, string, error) {
	title := "Sync Codex protocol baseline to " + targetRef
	body := fmt.Sprintf(`<!-- codexsdk-upstream-sync
upstream_ref: %s
upstream_ref_kind: %s
upstream_commit: %s
sync_commit: %s
base_branch: %s
-->

Automated upstream protocol sync.

## Description

This PR advances the checked-in protocol baseline for the selected upstream target, regenerates deterministic SDK artifacts, includes any one-run handwritten updates required by real drift, and publishes only after native checks passed.

It does not merge itself, tag, or bypass branch protection.

## Sync Metadata

- Upstream ref: `+"`%s`"+`
- Upstream ref kind: `+"`%s`"+`
- Upstream commit: `+"`%s`"+`
- Sync commit: `+"`%s`"+`
- Base branch: `+"`%s`"+`

This PR was generated by the upstream protocol sync workflow. It stops at the protected PR boundary and does not tag or bypass branch protection. Merge should happen only after branch protection and the required `+"`PR verification`"+` check accept this head commit.
`, targetRef, targetKind, targetSHA, syncCommit, landRef, targetRef, targetKind, targetSHA, syncCommit, landRef)

	list := exec.Command("gh", "pr", "list", "--base", landRef, "--head", syncBranch, "--state", "open", "--json", "number", "--jq", ".[0].number // empty")
	out, err := list.Output()
	if err != nil {
		return "", "", fmt.Errorf("gh pr list: %w", err)
	}
	number := strings.TrimSpace(string(out))
	if number != "" {
		edit := exec.Command("gh", "pr", "edit", number, "--title", title, "--body", body)
		if err := edit.Run(); err != nil {
			return "", "", fmt.Errorf("gh pr edit: %w", err)
		}
		view := exec.Command("gh", "pr", "view", number, "--json", "url", "--jq", ".url")
		urlOut, err := view.Output()
		if err != nil {
			return "", "", fmt.Errorf("gh pr view: %w", err)
		}
		return strings.TrimSpace(string(urlOut)), number, nil
	}
	create := exec.Command("gh", "pr", "create", "--base", landRef, "--head", syncBranch, "--title", title, "--body", body)
	var stdout, stderr bytes.Buffer
	create.Stdout = &stdout
	create.Stderr = &stderr
	if err := create.Run(); err != nil {
		return "", "", fmt.Errorf("gh pr create: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	prURL := strings.TrimSpace(stdout.String())
	view := exec.Command("gh", "pr", "view", prURL, "--json", "number", "--jq", ".number")
	numOut, err := view.Output()
	if err != nil {
		return prURL, "", nil
	}
	return prURL, strings.TrimSpace(string(numOut)), nil
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
