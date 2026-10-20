package protocolsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// refreshGoalBase keeps original B immutable, but snapshots current accepted M
// for this one verification attempt. Publication and observation require M to
// remain unchanged. A later move fails that tool call; the same native goal may
// invoke verification again with fresh M. No candidate or remote ref is edited.
func refreshGoalBase(ctx context.Context, local GoalVerifyRequest, binding GoalPublicationRequest) (GoalVerifyRequest, GoalPublicationRequest, func(), error) {
	noop := func() {}
	current, err := goalRemoteHead(ctx, binding, binding.BaseBranch)
	if err != nil {
		return local, binding, noop, errors.New("current accepted main unavailable")
	}
	if current == binding.BaseSHA && goalVerificationBase(binding) != binding.BaseSHA {
		return local, binding, noop, errors.New("main rolled back from previously verified base")
	}
	if current == binding.BaseSHA {
		binding.VerificationBaseSHA = current
		return local, binding, noop, nil
	}
	if _, err = goalGit(ctx, binding.RepoRoot, "fetch", "--no-tags", "--no-write-fetch-head", goalRemote(binding), current); err != nil {
		return local, binding, noop, errors.New("cannot fetch exact current accepted main")
	}
	decision, err := ResolveGoalRebinding(binding.RepoRoot, binding.BaseSHA, current, BaselineIdentity{SourceCommit: binding.Target.PeeledCommitSHA, SourceRefName: binding.Target.RefName, SourceRefKind: binding.Target.RefKind})
	if err != nil || decision.Decision != GoalRebindingRun {
		return local, binding, noop, errors.New("current main cannot preserve fixed goal target: " + decision.Reason)
	}
	if _, err = goalGit(ctx, binding.RepoRoot, "merge-base", "--is-ancestor", goalVerificationBase(binding), current); err != nil {
		return local, binding, noop, errors.New("current main does not descend from previously verified accepted base")
	}
	work, err := os.MkdirTemp("", "goal-current-base-")
	if err != nil {
		return local, binding, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(work) }
	root := filepath.Join(work, "accepted")
	if _, err = goalGit(ctx, binding.RepoRoot, "clone", "--no-hardlinks", "--no-local", "--no-checkout", binding.RepoRoot, root); err != nil {
		cleanup()
		return local, binding, noop, err
	}
	remote := goalRemote(binding)
	if remote == "origin" {
		remote, err = goalGit(ctx, binding.RepoRoot, "remote", "get-url", "origin")
		if err != nil {
			cleanup()
			return local, binding, noop, err
		}
	}
	if _, err = goalGit(ctx, root, "fetch", "--no-tags", "--no-write-fetch-head", remote, current); err != nil {
		cleanup()
		return local, binding, noop, err
	}
	if _, err = goalGit(ctx, root, "checkout", "--detach", current); err != nil {
		cleanup()
		return local, binding, noop, err
	}
	local.OriginalBaseRoot = local.BaseRoot
	local.BaseRoot = root
	binding.VerificationBaseSHA = current
	return local, binding, cleanup, nil
}
