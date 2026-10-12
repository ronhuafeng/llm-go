package protocolsync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GoalPreparedDraft retains the selected branch/head even when an effect is
// uncertain. A caller may reconcile this exact binding, never silently allocate
// a replacement PR. Binding contains only trusted controller inputs.
type GoalPreparedDraft struct {
	Binding GoalPublicationRequest
	URL     string
}

// PrepareGoalDraft creates a content-empty seed, never a prewritten SDK answer.
// GitHub's own CLI acceptance suite creates PRs using an allow-empty commit:
// https://github.com/cli/cli/blob/trunk/acceptance/testdata/pr/pr-view-status-respects-remote-pushdefault.txtar
// A supplied Branch/ExpectedHead is only for reconciling this operation's retained
// binding. It must still be an exact, content-empty, single-parent seed at B.
func PrepareGoalDraft(ctx context.Context, req GoalPublicationRequest) (GoalPreparedDraft, error) {
	result := GoalPreparedDraft{Binding: req}
	if req.API == nil || req.RepoRoot == "" || !goalRepositoryPattern.MatchString(req.Repository) || req.AppBotID <= 0 || req.BaseBranch != "main" || !shaRE.MatchString(req.BaseSHA) || !shaRE.MatchString(req.Target.PeeledCommitSHA) {
		return result, errors.New("incomplete trusted goal preparation binding")
	}
	if err := goalPrepareFresh(ctx, req); err != nil {
		return result, err
	}
	if req.Branch == "" {
		if req.Number != 0 || req.ExpectedHead != "" {
			return result, errors.New("partial preparation binding")
		}
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return result, errors.New("cannot allocate goal branch")
		}
		req.Branch = syncBranchName(req.Target.RefName, req.Target.PeeledCommitSHA, req.BaseSHA) + "-goal-" + hex.EncodeToString(random[:])
		tree, err := goalGit(ctx, req.RepoRoot, "rev-parse", req.BaseSHA+"^{tree}")
		if err != nil {
			return result, err
		}
		head, err := goalGit(ctx, req.RepoRoot, "-c", "user.name=Protocol goal controller", "-c", "user.email=protocol-goal@localhost", "commit-tree", tree, "-p", req.BaseSHA, "-m", "Prepare draft goal "+req.Branch)
		if err != nil {
			return result, err
		}
		req.ExpectedHead = head
		result.Binding = req
	}
	// Reuse the append binding validation without manufacturing a real PR number.
	validate := req
	if validate.Number == 0 {
		validate.Number = 1
	}
	if err := goalBinding(validate); err != nil {
		return result, err
	}
	prefix := syncBranchName(req.Target.RefName, req.Target.PeeledCommitSHA, req.BaseSHA) + "-goal-"
	suffix := strings.TrimPrefix(req.Branch, prefix)
	if suffix == req.Branch || len(suffix) != 16 {
		return result, errors.New("preparation branch is not a retained goal allocation")
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		return result, errors.New("invalid goal allocation")
	}
	parents, err := goalGit(ctx, req.RepoRoot, "show", "-s", "--format=%P", req.ExpectedHead)
	if err != nil || parents != req.BaseSHA {
		return result, errors.New("goal seed is not a single child of bound base")
	}
	tree, err := goalGit(ctx, req.RepoRoot, "rev-parse", req.ExpectedHead+"^{tree}")
	if err != nil {
		return result, err
	}
	baseTree, err := goalGit(ctx, req.RepoRoot, "rev-parse", req.BaseSHA+"^{tree}")
	if err != nil || tree != baseTree {
		return result, errors.New("goal seed contains file changes")
	}
	existing, err := goalPreparePRs(req)
	if err != nil {
		return result, err
	}
	if len(existing) > 1 {
		return result, errors.New("goal branch has ambiguous PR history")
	}
	remoteHead, err := goalPrepareRemote(ctx, req)
	if err != nil {
		return result, err
	}
	if remoteHead == "" {
		if len(existing) != 0 || req.Number != 0 {
			return result, errors.New("retained goal branch disappeared")
		}
		if err := goalPrepareFresh(ctx, req); err != nil {
			return result, err
		}
		_, pushErr := goalGit(ctx, req.RepoRoot, "push", "--porcelain", goalRemote(req), req.ExpectedHead+":refs/heads/"+req.Branch)
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		actual, readErr := goalPrepareRemote(readCtx, req)
		cancel()
		if readErr != nil || actual != req.ExpectedHead {
			_ = pushErr
			return result, errors.New("goal seed push outcome unconfirmed; retain binding")
		}
	} else if remoteHead != req.ExpectedHead {
		return result, errors.New("goal branch moved; preparation stopped")
	}
	if err := goalPrepareFresh(ctx, req); err != nil {
		return result, err
	}
	actor, err := publicationHeadActor(PendingRequest{Repository: req.Repository}, req.API, req.Branch, req.ExpectedHead)
	if err != nil || verifyAppActor(actor, req.AppBotID) != nil {
		return result, errors.New("goal seed push actor cannot be confirmed")
	}
	if len(existing) == 0 {
		if req.Number != 0 {
			return result, errors.New("retained PR disappeared")
		}
		// Exactly one create attempt; native discovery also reconciles a lost response.
		_ = req.API.Request("POST", "repos/"+req.Repository+"/pulls", map[string]any{"head": req.Branch, "base": req.BaseBranch, "title": "Draft protocol goal: " + req.Target.RefName, "body": "Trusted empty seed for a bounded protocol goal. No implementation or verification result is included. Remain draft; no automatic merge.", "draft": true}, nil)
		existing, err = goalPreparePRs(req)
		if err != nil {
			return result, errors.New("goal PR creation outcome unknown; retain binding")
		}
	}
	if len(existing) != 1 {
		return result, errors.New("goal PR creation is not uniquely confirmed; retain binding")
	}
	pr := existing[0]
	if req.Number != 0 && req.Number != pr.Number {
		return result, errors.New("goal PR number changed")
	}
	req.Number = pr.Number
	result.Binding = req
	if err := goalCurrent(ctx, req, req.ExpectedHead); err != nil {
		return result, err
	}
	expectedURL := fmt.Sprintf("https://github.com/%s/pull/%d", req.Repository, req.Number)
	if pr.URL != expectedURL {
		return result, errors.New("goal PR URL does not match native identity")
	}
	result.URL = pr.URL
	return result, nil
}

func goalPrepareFresh(ctx context.Context, req GoalPublicationRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var repo struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := req.API.Request("GET", "repos/"+req.Repository, nil, &repo); err != nil || repo.FullName != req.Repository || repo.DefaultBranch != req.BaseBranch {
		return errors.New("goal repository/default branch cannot be confirmed")
	}
	base, err := goalRemoteHead(ctx, req, req.BaseBranch)
	if err != nil || base != req.BaseSHA {
		return errors.New("main moved before goal preparation")
	}
	target, err := ResolveUpstream(ResolveRequest{UpstreamRef: req.Target.RefName, Lookuper: goalLookupContext(ctx, req.Lookuper)})
	if err != nil || target.RefName != req.Target.RefName || target.RefKind != req.Target.RefKind || target.PeeledCommitSHA != req.Target.PeeledCommitSHA {
		return errors.New("upstream target moved before goal preparation")
	}
	return ctx.Err()
}
func goalPrepareRemote(ctx context.Context, req GoalPublicationRequest) (string, error) {
	out, err := goalGit(ctx, req.RepoRoot, "ls-remote", goalRemote(req), "refs/heads/"+req.Branch)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", nil
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != "refs/heads/"+req.Branch || !shaRE.MatchString(fields[0]) {
		return "", errors.New("goal branch readback is ambiguous")
	}
	return fields[0], nil
}
func goalPreparePRs(req GoalPublicationRequest) ([]goalDraftPR, error) {
	var all []goalDraftPR
	owner := strings.SplitN(req.Repository, "/", 2)[0]
	for page := 1; page <= 20; page++ {
		var batch []goalDraftPR
		path := fmt.Sprintf("repos/%s/pulls?state=all&head=%s&per_page=100&page=%d", req.Repository, url.QueryEscape(owner+":"+req.Branch), page)
		if err := req.API.Request("GET", path, nil, &batch); err != nil {
			return nil, errors.New("cannot reconcile goal PR")
		}
		for _, pr := range batch {
			if pr.Head.Ref != req.Branch || pr.Head.Repo.FullName != req.Repository {
				return nil, errors.New("goal PR listing does not match bound branch")
			}
			all = append(all, pr)
		}
		if len(batch) < 100 {
			return all, nil
		}
	}
	return nil, errors.New("goal PR listing exceeds bounded reconciliation")
}
