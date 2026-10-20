package protocolsync

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// GoalPreflightRequest selects a target without generating schemas or changing
// local or remote state. API is the existing read-only observation identity.
type GoalPreflightRequest struct {
	RepoRoot, Repository, BaseBranch, BaseSHA, UpstreamRef string
	AppBotID                                               int64
	API                                                    PublicationAPI
	Lookuper                                               RemoteLookuper
}

// GoalPreflightResult is coordination, never acceptance or permission to reuse
// another goal's candidate. Target is the exact newly resolved upstream identity.
type GoalPreflightResult struct {
	Outcome       string
	Reason        string
	Target        Target
	ExistingPRURL string
}

var nativeGoalBranch = regexp.MustCompile(`^codex/sync-upstream-[A-Za-z0-9._-]{1,64}-[0-9a-f]{12}-base-[0-9a-f]{12}-goal-[0-9a-f]{16}$`)

// GoalPreflight performs only bounded reads. The effect-owning preparation path
// must still revalidate its bindings after this advisory coordination snapshot.
func GoalPreflight(ctx context.Context, req GoalPreflightRequest) (result GoalPreflightResult, err error) {
	result.Outcome = "blocked"
	defer func() {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			result.Outcome = "blocked"
			result.Reason = err.Error()
		}
	}()
	if req.RepoRoot == "" || !goalRepositoryPattern.MatchString(req.Repository) || req.BaseBranch != "main" || !shaRE.MatchString(req.BaseSHA) || req.AppBotID <= 0 || req.API == nil {
		return result, errors.New("incomplete goal preflight binding")
	}
	head, e := goalGit(ctx, req.RepoRoot, "rev-parse", "HEAD")
	if e != nil || head != req.BaseSHA {
		return result, errors.New("preflight checkout differs from accepted base")
	}
	baseline, e := gitBaselineIdentity(req.RepoRoot, req.BaseSHA)
	if e != nil {
		return result, errors.New("cannot read accepted baseline identity")
	}
	result.Target, e = ResolveUpstream(ResolveRequest{UpstreamRef: req.UpstreamRef, Lookuper: goalLookupContext(ctx, req.Lookuper)})
	if e != nil {
		return result, errors.New("cannot resolve preflight upstream target")
	}
	policy := EvaluatePolicy(PolicyRequest{Baseline: baseline, TargetRef: result.Target.RefName, TargetKind: result.Target.RefKind, TargetSHA: result.Target.PeeledCommitSHA, TargetExplicit: result.Target.TargetExplicit})
	if policy.Decision == DecisionBlock {
		return result, errors.New(policy.Reason)
	}
	if policy.Decision == DecisionSkip {
		result.Outcome = "skip"
		result.Reason = policy.Reason
		return result, nil
	}
	seen := map[int]bool{}
	var candidate *publicationPR
	complete := false
	for page := 1; page <= 20; page++ {
		if e := ctx.Err(); e != nil {
			return result, e
		}
		var batch []publicationPR
		path := fmt.Sprintf("repos/%s/pulls?state=open&base=%s&per_page=100&page=%d", req.Repository, url.QueryEscape(req.BaseBranch), page)
		if req.API.Request("GET", path, nil, &batch) != nil {
			return result, errors.New("cannot list open preflight PRs")
		}
		if batch == nil || len(batch) > 100 {
			return result, errors.New("malformed open PR listing")
		}
		for _, pr := range batch {
			if pr.Number <= 0 || seen[pr.Number] || pr.State != "open" || pr.MergedAt != nil || pr.Base.Ref != req.BaseBranch || pr.Base.Repo.FullName != req.Repository || !shaRE.MatchString(pr.Base.SHA) || pr.Head.Ref == "" || pr.Head.Repo.FullName == "" || !shaRE.MatchString(pr.Head.SHA) || pr.User.ID <= 0 || pr.User.Type == "" || pr.URL != fmt.Sprintf("https://github.com/%s/pull/%d", req.Repository, pr.Number) {
				return result, errors.New("malformed or changing open PR identity")
			}
			seen[pr.Number] = true
			if !strings.HasPrefix(pr.Head.Ref, "codex/sync-upstream") {
				continue
			}
			if !nativeGoalBranch.MatchString(pr.Head.Ref) || pr.Head.Repo.FullName != req.Repository || verifyAppActor(pr.User, req.AppBotID) != nil {
				return result, errors.New("untrusted or unsupported open sync PR identity")
			}
			if candidate != nil {
				return result, errors.New("multiple open native goal PRs are ambiguous")
			}
			copy := pr
			candidate = &copy
		}
		if len(batch) < 100 {
			complete = true
			break
		}
	}
	if !complete {
		return result, errors.New("open PR listing exceeds bounded preflight")
	}
	if candidate != nil {
		pr := *candidate
		var ref struct {
			Ref    string `json:"ref"`
			Object struct {
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"object"`
		}
		if req.API.Request("GET", "repos/"+req.Repository+"/git/ref/heads/"+pr.Head.Ref, nil, &ref) != nil || ref.Ref != "refs/heads/"+pr.Head.Ref || ref.Object.Type != "commit" || !shaRE.MatchString(ref.Object.SHA) {
			return result, errors.New("cannot confirm native goal branch head")
		}
		actor, e := publicationHeadActor(PendingRequest{Repository: req.Repository}, req.API, pr.Head.Ref, ref.Object.SHA)
		if e != nil || verifyAppActor(actor, req.AppBotID) != nil {
			return result, errors.New("cannot confirm native goal head actor")
		}
		result.Outcome = "skip"
		result.Reason = "existing trusted native goal PR requires review or continuation"
		result.ExistingPRURL = pr.URL
		return result, nil
	}
	result.Outcome = "run"
	result.Reason = policy.Reason
	return result, nil
}
