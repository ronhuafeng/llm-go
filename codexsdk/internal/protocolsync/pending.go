package protocolsync

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// PendingRequest inspects native PR state before spending another generation or
// Agent attempt. BaseSHA is the immutable accepted checkout for this run.
type PendingRequest struct {
	ReadChecks                                                     bool
	RepoRoot, Repository, AppClientID, BaseBranch, BaseSHA, Remote string
	Target                                                         Target
	API                                                            PublicationAPI
}

// PendingPublication is an observation, not acceptance or a fresh proof.
// Head is carried to publication as the expected old revision.
type PendingPublication struct {
	Number                                                 int
	URL, Branch, Head, BaseSHA, Checks, Description, Title string
	Target                                                 BaselineIdentity
	Reusable                                               bool
}

func publicationPolicy(format string, args ...any) error {
	return &Failure{Category: FailurePolicy, Err: fmt.Errorf("needs-maintainer: "+format, args...)}
}

// InspectPending validates ownership and actual Git contents before reusing a
// pending publication. Unknown reads and ambiguous matches fail explicitly.
func InspectPending(req PendingRequest) (PendingPublication, error) {
	if req.Repository == "" || req.AppClientID == "" || req.BaseBranch == "" || !shaRE.MatchString(req.BaseSHA) {
		return PendingPublication{}, publicationPolicy("repository, App client ID, base branch and exact base SHA are required")
	}
	api := req.API
	if api == nil {
		api = githubPublicationAPI{repoRoot: req.RepoRoot}
	}
	prs, err := listPublicationPRs(api, req.Repository)
	if err != nil {
		return PendingPublication{}, err
	}
	var found []PendingPublication
	for _, pr := range prs {
		if !strings.HasPrefix(pr.Head.Ref, "codex/sync-upstream") {
			if len(parseSyncMetadata(pr.Body)) > 0 {
				return PendingPublication{}, publicationPolicy("sync PR #%d branch was renamed outside its publishing namespace", pr.Number)
			}
			continue
		}
		if pr.MergedAt != nil {
			continue
		}
		if pr.Head.Repo.FullName != req.Repository || pr.Base.Ref != req.BaseBranch {
			return PendingPublication{}, publicationPolicy("sync PR #%d has another repository or base", pr.Number)
		}
		if pr.State != "open" {
			// Closed PRs retain their exact head even after branch deletion. Read
			// its Git identity; a mutable description or live ref is unnecessary.
			if err := fetchPublicationCommit(req, pr.Head.SHA); err != nil {
				return PendingPublication{}, err
			}
			identity, err := gitBaselineIdentity(req.RepoRoot, pr.Head.SHA)
			if err != nil {
				return PendingPublication{}, err
			}
			if identity.SourceRefName == req.Target.RefName && identity.SourceRefKind == req.Target.RefKind && identity.SourceCommit != req.Target.PeeledCommitSHA {
				return PendingPublication{}, &Failure{Category: FailureSource, Err: fmt.Errorf("upstream ref %s changed since closed PR #%d", req.Target.RefName, pr.Number)}
			}
			if identity.SourceCommit != req.Target.PeeledCommitSHA || identity.SourceRefName != req.Target.RefName || identity.SourceRefKind != req.Target.RefKind {
				continue
			}
			return PendingPublication{}, publicationPolicy("sync PR #%d was closed; restore it explicitly before retrying this candidate", pr.Number)
		}
		if err := verifyAppActor(api, pr.User, req.AppClientID); err != nil {
			return PendingPublication{}, fmt.Errorf("sync PR #%d ownership: %w", pr.Number, err)
		}

		description, err := parsePublicationDescription(pr)
		if err != nil {
			return PendingPublication{}, err
		}
		pending, err := inspectPublicationHead(req, api, pr)
		if err != nil {
			return PendingPublication{}, err
		}
		pending.Reusable = pending.Reusable && description.target == pending.Target && description.head == pending.Head && description.base == req.BaseBranch
		found = append(found, pending)
	}
	if len(found) > 1 {
		return PendingPublication{}, publicationPolicy("multiple pending sync PRs require reconciliation")
	}
	if len(found) == 0 {
		return inspectOrphanPublication(req, api, prs)
	}
	pending := found[0]
	if pendingVersion, ok := parseRustTag(pending.Target.SourceRefName); ok {
		if requestedVersion, ok := parseRustTag(req.Target.RefName); ok && compareVersion(requestedVersion, pendingVersion) < 0 {
			return PendingPublication{}, publicationPolicy("pending PR already targets a newer stable version; refusing an older publication")
		}
	}

	var current publicationPR
	if err := api.Request("GET", fmt.Sprintf("repos/%s/pulls/%d", req.Repository, pending.Number), nil, &current); err != nil {
		return PendingPublication{}, err
	}
	if current.State != "open" || current.Head.SHA != pending.Head || current.Head.Ref != pending.Branch || current.Base.Ref != req.BaseBranch || current.Base.SHA != req.BaseSHA || current.Body != pending.Description || current.Title != pending.Title {
		return PendingPublication{}, publicationPolicy("sync PR #%d changed while inspecting it", pending.Number)
	}
	if pending.Reusable && req.ReadChecks {
		// GitHub attaches PR check runs to H even when jobs check out M.
		// Same-named checks alone cannot prove that current M was tested.
		pending.Checks = "PR head checks pending or missing; merge candidate unverified"
		if current.MergeSHA != "" {
			var checks struct {
				Runs []struct {
					Name       string `json:"name"`
					Status     string `json:"status"`
					Conclusion string `json:"conclusion"`
				} `json:"check_runs"`
			}
			if err := api.Request("GET", fmt.Sprintf("repos/%s/commits/%s/check-runs?per_page=100", req.Repository, pending.Head), nil, &checks); err != nil {
				return PendingPublication{}, err
			}
			passed := map[string]bool{}
			for _, c := range checks.Runs {
				if c.Name != "Root source verification" && c.Name != "Codex generated reproducibility / Generated reproducibility" {
					continue
				}
				if c.Status == "completed" && c.Conclusion != "" && c.Conclusion != "success" {
					pending.Checks = "PR head checks failed; merge candidate unverified"
					break
				}
				passed[c.Name] = c.Status == "completed" && c.Conclusion == "success"
			}
			if pending.Checks != "PR head checks failed; merge candidate unverified" && passed["Root source verification"] && passed["Codex generated reproducibility / Generated reproducibility"] {
				pending.Checks = "PR head checks succeeded; merge candidate unverified; no fresh proof acquired"
			}
		}
	}
	return pending, nil
}

func verifyAppActor(api PublicationAPI, user publicationUser, clientID string) error {
	if user.Type != "Bot" || !strings.HasSuffix(user.Login, "[bot]") {
		return publicationPolicy("publication is not owned by a GitHub App bot")
	}
	var app struct {
		ClientID string `json:"client_id"`
	}
	slug := strings.TrimSuffix(user.Login, "[bot]")
	if err := api.Request("GET", "apps/"+url.PathEscape(slug), nil, &app); err != nil {
		return err
	}
	if app.ClientID != clientID {
		return publicationPolicy("publication belongs to a different GitHub App")
	}
	return nil
}

func inspectPublicationHead(req PendingRequest, api PublicationAPI, pr publicationPR) (PendingPublication, error) {
	head := pr.Head.SHA
	if err := fetchPublicationCommit(req, head); err != nil {
		return PendingPublication{}, err
	}
	parents, err := gitOutput(req.RepoRoot, "rev-list", "--parents", "-n", "1", head)
	if err != nil {
		return PendingPublication{}, err
	}
	fields := strings.Fields(parents)
	if len(fields) != 2 {
		return PendingPublication{}, publicationPolicy("PR #%d is not a single candidate commit", pr.Number)
	}
	base := fields[1]
	if err := runGit(req.RepoRoot, "merge-base", "--is-ancestor", base, req.BaseSHA); err != nil {
		return PendingPublication{}, publicationPolicy("PR #%d contains commits outside the accepted base history", pr.Number)
	}

	actor, err := publicationHeadActor(req, api, pr.Head.Ref, head)
	if err != nil {
		return PendingPublication{}, err
	}
	if actor.Login != pr.User.Login || actor.Type != "Bot" {
		return PendingPublication{}, publicationPolicy("sync branch head was changed outside its publishing App")
	}

	changed, err := gitNUL(req.RepoRoot, "diff", "--name-only", "--no-renames", "-z", base, head, "--")
	if err != nil {
		return PendingPublication{}, err
	}
	if err := validatePaths(changed, "final"); err != nil {
		return PendingPublication{}, err
	}
	metadata, err := gitBaselineIdentity(req.RepoRoot, head)
	if err != nil {
		return PendingPublication{}, err
	}

	if !shaRE.MatchString(metadata.SourceCommit) || metadata.SourceRefName == "" || !validKinds[metadata.SourceRefKind] {
		return PendingPublication{}, publicationPolicy("PR #%d has an invalid upstream identity", pr.Number)
	}
	if metadata.SourceRefName == req.Target.RefName && metadata.SourceRefKind == req.Target.RefKind && metadata.SourceCommit != req.Target.PeeledCommitSHA {
		return PendingPublication{}, &Failure{Category: FailureSource, Err: fmt.Errorf("upstream ref %s changed from pending commit %s to %s", metadata.SourceRefName, metadata.SourceCommit, req.Target.PeeledCommitSHA)}
	}
	return PendingPublication{Number: pr.Number, URL: pr.URL, Description: pr.Body, Title: pr.Title, Branch: pr.Head.Ref, Head: head, BaseSHA: base, Target: BaselineIdentity{SourceCommit: metadata.SourceCommit, SourceRefName: metadata.SourceRefName, SourceRefKind: metadata.SourceRefKind}, Reusable: base == req.BaseSHA && metadata.SourceCommit == req.Target.PeeledCommitSHA && metadata.SourceRefName == req.Target.RefName && metadata.SourceRefKind == req.Target.RefKind}, nil
}

// An owned branch can survive a lost push/create response. Rebuild and validate
// before completing its publication; never interpret an unknown remote as absent.
func inspectOrphanPublication(req PendingRequest, api PublicationAPI, prs []publicationPR) (PendingPublication, error) {
	known := map[string]bool{}
	for _, pr := range prs {
		known[pr.Head.Ref] = true
	}
	refs, err := gitOutput(req.RepoRoot, "ls-remote", "--heads", publicationRemote(req), "refs/heads/codex/sync-upstream*")
	if err != nil {
		return PendingPublication{}, err
	}
	var found []PendingPublication
	for _, line := range strings.Split(strings.TrimSpace(refs), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || !shaRE.MatchString(fields[0]) {
			return PendingPublication{}, publicationPolicy("invalid remote sync branch observation")
		}
		branch := strings.TrimPrefix(fields[1], "refs/heads/")
		if known[branch] {
			continue
		}

		actor, err := publicationHeadActor(req, api, branch, fields[0])
		if err != nil {
			return PendingPublication{}, err
		}
		if err := verifyAppActor(api, actor, req.AppClientID); err != nil {
			return PendingPublication{}, fmt.Errorf("orphan sync branch %s: %w", branch, err)
		}
		pr := publicationPR{User: actor}

		pr.Head.SHA = fields[0]
		pr.Head.Ref = branch
		observation, err := inspectPublicationHead(req, api, pr)
		if err != nil {
			return PendingPublication{}, err
		}
		observation.Reusable = false
		found = append(found, observation)
	}
	if len(found) > 1 {
		return PendingPublication{}, publicationPolicy("multiple orphan sync branches require reconciliation")
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return PendingPublication{Head: "absent", Branch: syncBranchName(req.Target.RefName, req.Target.PeeledCommitSHA)}, nil
}

// GitHub's activity actor records who changed the ref. Commit author fields and
// editable PR metadata cannot establish that authority.
func publicationHeadActor(req PendingRequest, api PublicationAPI, branch, head string) (publicationUser, error) {
	var activities []struct {
		Ref   string          `json:"ref"`
		After string          `json:"after"`
		Actor publicationUser `json:"actor"`
	}
	path := fmt.Sprintf("repos/%s/activity?ref=%s&per_page=1", req.Repository, url.QueryEscape("refs/heads/"+branch))
	if err := api.Request("GET", path, nil, &activities); err != nil {
		return publicationUser{}, err
	}
	if len(activities) != 1 || activities[0].Ref != "refs/heads/"+branch || activities[0].After != head {
		return publicationUser{}, publicationPolicy("current push ownership of %s at %s is not confirmed", branch, head)
	}
	return activities[0].Actor, nil
}

func publicationRemote(req PendingRequest) string {
	if req.Remote != "" {
		return req.Remote
	}
	return "origin"
}

func gitBaselineIdentity(repoRoot, head string) (BaselineIdentity, error) {
	raw, err := gitOutput(repoRoot, "show", head+":"+filepath.ToSlash(filepath.Join("codexsdk", defaultBaselineRel, "baseline_metadata.json")))
	if err != nil {
		return BaselineIdentity{}, err
	}
	return decodeBaselineIdentity([]byte(raw))
}

func fetchPublicationCommit(req PendingRequest, head string) error {
	if !shaRE.MatchString(head) {
		return publicationPolicy("publication has no exact head")
	}
	if _, err := gitOutput(req.RepoRoot, "cat-file", "-e", head+"^{commit}"); err != nil {
		return runGit(req.RepoRoot, "fetch", "--no-tags", publicationRemote(req), head)
	}
	return nil
}
