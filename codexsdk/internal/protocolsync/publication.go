package protocolsync

import (
	"fmt"
	"strings"
)

func publishCandidate(req PublishRequest) (string, error) {
	if req.RepoRoot == "" || req.BaseBranch == "" || req.TargetRef == "" || req.TargetKind == "" || !shaRE.MatchString(req.TargetSHA) {
		return "", fmt.Errorf("repo-root, base-branch, and exact target identity are required")
	}
	remote := req.Remote
	if remote == "" {
		remote = "origin"
	}
	baseBranch := normalizeBranchRef(req.BaseBranch, remote)
	if err := AssertClean(req.RepoRoot); err != nil {
		return "", err
	}
	head, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	head = strings.TrimSpace(head)
	if err := runGit(req.RepoRoot, "fetch", remote, "refs/heads/"+baseBranch+":refs/remotes/"+remote+"/"+baseBranch); err != nil {
		return "", err
	}
	landing, err := gitOutput(req.RepoRoot, "rev-parse", remote+"/"+baseBranch)
	if err != nil {
		return "", err
	}
	parent, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD^")
	if err != nil {
		return "", err
	}
	parent = strings.TrimSpace(parent)
	if strings.TrimSpace(landing) != parent {
		return "", &Failure{Category: FailurePublication, Err: fmt.Errorf("landing ref %s moved; rebuild against the current base", baseBranch)}
	}
	if req.ExpectedHead != "absent" && !shaRE.MatchString(req.ExpectedHead) {
		return "", publicationPolicy("publication requires the original observed head or explicit absence")
	}
	target, err := ResolveUpstream(ResolveRequest{UpstreamRef: req.TargetRef, Lookuper: req.Lookuper})
	if err != nil {
		return "", err
	}
	if target.PeeledCommitSHA != req.TargetSHA || target.RefName != req.TargetRef || target.RefKind != req.TargetKind {
		return "", &Failure{Category: FailureSource, Err: fmt.Errorf("upstream target moved from %s to %s", req.TargetSHA, target.PeeledCommitSHA)}
	}

	identity, err := gitBaselineIdentity(req.RepoRoot, head)
	if err != nil {
		return "", err
	}
	if identity.SourceCommit != req.TargetSHA || identity.SourceRefName != req.TargetRef || identity.SourceRefKind != req.TargetKind {
		return "", &Failure{Category: FailureSource, Err: fmt.Errorf("candidate commit does not contain the selected upstream identity")}
	}
	changed, err := gitNUL(req.RepoRoot, "diff", "--name-only", "--no-renames", "-z", parent, head, "--")
	if err != nil {
		return "", err
	}
	if err := validatePaths(changed, "final"); err != nil {
		return "", err
	}
	api := req.API
	if api == nil {
		api = githubPublicationAPI{repoRoot: req.RepoRoot}
	}
	inspection := PendingRequest{RepoRoot: req.RepoRoot, Repository: req.Repository, AppClientID: req.AppClientID, BaseBranch: baseBranch, BaseSHA: parent, Target: target, API: api, Remote: remote}
	observed, err := InspectPending(inspection)
	if err != nil {
		return "", err
	}
	branch := req.ExpectedBranch
	if branch == "" || !strings.HasPrefix(branch, "codex/sync-upstream") {
		return "", publicationPolicy("publication requires its original sync branch")
	}
	publishHead := head
	alreadyPublished := false
	if observed.Head != "absent" && observed.BaseSHA == parent && observed.Target.SourceCommit == req.TargetSHA && observed.Target.SourceRefName == req.TargetRef && observed.Target.SourceRefKind == req.TargetKind {
		oldTree, err := gitOutput(req.RepoRoot, "rev-parse", observed.Head+"^{tree}")
		if err != nil {
			return "", err
		}
		newTree, err := gitOutput(req.RepoRoot, "rev-parse", head+"^{tree}")
		if err != nil {
			return "", err
		}
		if oldTree == newTree {
			// A prior attempt may have published these identical bytes before losing
			// its response. Keep the actual published revision, not this new timestamp.
			publishHead = observed.Head
			alreadyPublished = true
		}
	}
	if observed.Branch != branch || (!alreadyPublished && observed.Head != req.ExpectedHead) {
		return "", publicationPolicy("publication state changed since planning; expected %s at %s, observed %s at %s", branch, req.ExpectedHead, observed.Branch, observed.Head)
	}
	if !alreadyPublished {
		expected := req.ExpectedHead
		if expected == "absent" {
			expected = ""
		}
		pushErr := runGit(req.RepoRoot, "push", "--force-with-lease=refs/heads/"+branch+":"+expected, remote, head+":refs/heads/"+branch)
		actual, readErr := remotePublicationHead(req.RepoRoot, remote, branch)
		if readErr != nil {
			return "", fmt.Errorf("publication push result unknown (%v); readback: %w", pushErr, readErr)
		}
		if actual != head {
			return "", &Failure{Category: FailurePublication, Err: fmt.Errorf("publication push did not establish expected head %s (observed %s): %v", head, actual, pushErr)}
		}
	}
	body := publicationBody(baseBranch, req.TargetRef, req.TargetKind, req.TargetSHA, publishHead)
	title := publicationTitle(req.TargetRef)
	var pr publicationPR
	if observed.Number != 0 {
		path := fmt.Sprintf("repos/%s/pulls/%d", req.Repository, observed.Number)
		var before publicationPR
		if err := api.Request("GET", path, nil, &before); err != nil {
			return "", err
		}
		if before.State != "open" || before.Head.SHA != publishHead || before.Body != observed.Description || before.Title != observed.Title {
			return "", publicationPolicy("PR changed after push; retain the remote state for inspection")
		}
		if alreadyPublished && observed.Reusable {
			pr = before
		} else {
			writeErr := api.Request("PATCH", path, map[string]string{"title": title, "body": body}, nil)
			if err := api.Request("GET", path, nil, &pr); err != nil {
				return "", fmt.Errorf("PR update result unknown (%v): %w", writeErr, err)
			}
			if pr.Body != body || pr.Title != title {
				return "", fmt.Errorf("PR update was not confirmed: %v", writeErr)
			}
		}
	} else {
		createErr := api.Request("POST", "repos/"+req.Repository+"/pulls", map[string]string{"head": branch, "base": baseBranch, "title": title, "body": body}, &pr)
		// Even a successful response is followed by discovery. A lost response must
		// not create a second PR on retry.
		all, readErr := listPublicationPRs(api, req.Repository)
		if readErr != nil {
			return "", fmt.Errorf("PR creation result unknown (%v): %w", createErr, readErr)
		}
		matches := 0
		for _, item := range all {
			if item.Head.Ref == branch && item.State == "open" {
				pr = item
				matches++
			}
		}
		if matches != 1 {
			return "", fmt.Errorf("PR creation has %d confirmed matches: %v", matches, createErr)
		}
	}
	if pr.State != "open" || pr.Head.SHA != publishHead || pr.Head.Ref != branch || pr.Head.Repo.FullName != req.Repository || pr.Base.Ref != baseBranch || pr.Base.SHA != parent || pr.Body != body || pr.Title != title {
		return "", publicationPolicy("PR head or base changed during publication; no fresh acceptance is claimed")
	}
	if err := verifyAppActor(api, pr.User, req.AppClientID); err != nil {
		return "", err
	}
	latest, err := gitOutput(req.RepoRoot, "ls-remote", remote, "refs/heads/"+baseBranch)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(latest)
	if len(fields) != 2 || fields[0] != parent {
		return "", &Failure{Category: FailurePublication, Err: fmt.Errorf("base moved during publication; PR remains pending and must be rebuilt")}
	}

	finalHead, err := remotePublicationHead(req.RepoRoot, remote, branch)
	if err != nil {
		return "", err
	}
	if finalHead != publishHead {
		return "", publicationPolicy("remote head changed during publication; preserving concurrent changes")
	}
	return publicationResult(req, pr.URL, pr.Number, branch, publishHead)
}

func remotePublicationHead(root, remote, branch string) (string, error) {
	out, err := gitOutput(root, "ls-remote", remote, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	if len(fields) != 2 || !shaRE.MatchString(fields[0]) {
		return "", fmt.Errorf("invalid remote branch observation")
	}
	return fields[0], nil
}

func publicationResult(req PublishRequest, url string, number int, branch, head string) (string, error) {
	if err := appendGitHubOutput(req.GitHubOutputPath, map[string]string{"outcome": OutcomePRPending, "pr_url": url, "pr_number": fmt.Sprint(number), "sync_branch": branch, "sync_commit": head}); err != nil {
		return "", err
	}
	return url, nil
}
