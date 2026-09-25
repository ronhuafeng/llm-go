package protocolsync

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

const defaultRemote = "https://github.com/openai/codex.git"

// Target is a resolved upstream Codex ref.
type Target struct {
	RefName         string
	RefKind         string
	TagSHA          string
	PeeledCommitSHA string
	TargetExplicit  bool
}

// ResolveRequest selects an upstream target.
type ResolveRequest struct {
	Remote       string
	UpstreamRef  string
	LatestStable bool
	Lookuper     RemoteLookuper
}

// RemoteLookuper runs git ls-remote.
type RemoteLookuper interface {
	LSRemote(remote string, patterns ...string) (string, error)
}

// GitLookuper calls git ls-remote.
type GitLookuper struct{}

// LSRemote implements RemoteLookuper.
func (GitLookuper) LSRemote(remote string, patterns ...string) (string, error) {
	args := append([]string{"ls-remote", remote}, patterns...)
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &Failure{Category: FailureExecution, Err: fmt.Errorf("git ls-remote: %w: %s", err, strings.TrimSpace(stderr.String()))}
	}
	return stdout.String(), nil
}

// ResolveUpstream resolves a tag, ref, or SHA against openai/codex.
func ResolveUpstream(req ResolveRequest) (Target, error) {
	remote := req.Remote
	if remote == "" {
		remote = defaultRemote
	}
	lookuper := req.Lookuper
	if lookuper == nil {
		lookuper = GitLookuper{}
	}
	requested := strings.TrimSpace(req.UpstreamRef)
	if req.LatestStable && requested != "" {
		return Target{}, fmt.Errorf("--latest-stable cannot be combined with --upstream-ref")
	}
	explicit := requested != ""
	if !explicit {
		output, err := lookuper.LSRemote(remote, "refs/tags/rust-v*")
		if err != nil {
			return Target{}, err
		}
		tag, err := latestStableRustTag(output)
		if err != nil {
			return Target{}, err
		}
		requested = tag
	}
	tagSHA, peeled, err := resolveRemoteRef(lookuper, remote, requested)
	if err != nil {
		return Target{}, err
	}
	return Target{
		RefName:         requested,
		RefKind:         inferRefKind(requested),
		TagSHA:          tagSHA,
		PeeledCommitSHA: peeled,
		TargetExplicit:  explicit,
	}, nil
}

func latestStableRustTag(lsRemoteOutput string) (string, error) {
	var best string
	var bestVersion [3]int
	found := false
	for _, line := range strings.Split(lsRemoteOutput, "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		ref := parts[1]
		const prefix = "refs/tags/"
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		tag := strings.TrimPrefix(ref, prefix)
		version, ok := parseRustTag(tag)
		if !ok {
			continue
		}
		if !found || compareVersion(version, bestVersion) > 0 {
			best = tag
			bestVersion = version
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("unable to resolve an upstream rust-vX.Y.Z tag")
	}
	return best, nil
}

func resolveRemoteRef(lookuper RemoteLookuper, remote, ref string) (tagSHA, peeled string, err error) {
	if shaRE.MatchString(ref) {
		return "", ref, nil
	}
	tagRef := "refs/tags/" + ref
	output, err := lookuper.LSRemote(remote, tagRef, tagRef+"^{}")
	if err != nil {
		return "", "", err
	}
	entries := remoteRefSHAs(output)
	tagSHA = entries[tagRef]
	peeled = entries[tagRef+"^{}"]
	if peeled == "" {
		peeled = tagSHA
	}
	if peeled != "" {
		if tagSHA == "" {
			tagSHA = peeled
		}
		return tagSHA, peeled, nil
	}
	for _, candidate := range []string{ref + "^{}", "refs/heads/" + ref, ref} {
		output, err = lookuper.LSRemote(remote, candidate)
		if err != nil {
			return "", "", err
		}
		if sha := firstRemoteSHA(output); sha != "" {
			return "", sha, nil
		}
	}
	return "", "", fmt.Errorf("unable to resolve upstream ref in openai/codex: %s", ref)
}

func remoteRefSHAs(output string) map[string]string {
	shas := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if _, ok := shas[parts[1]]; !ok {
			shas[parts[1]] = parts[0]
		}
	}
	return shas
}

func firstRemoteSHA(output string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[0] != "" {
			return parts[0]
		}
	}
	return ""
}
