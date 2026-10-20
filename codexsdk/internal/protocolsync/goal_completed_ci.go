package protocolsync

import (
	"context"
	"errors"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/checkacceptance"
	"time"
)

// goalMainForHandoff permits only monotonic main advancement. It is identity
// classification, not a claim that this newer M's contracts were verified on H.
// The integrator must construct H' and prove both original and current contracts.
func goalMainForHandoff(ctx context.Context, r GoalPublicationRequest) (string, error) {
	current, err := goalRemoteHead(ctx, r, r.BaseBranch)
	if err != nil {
		return "", err
	}
	previous := goalVerificationBase(r)
	if current == previous {
		return current, nil
	}
	if _, err = goalGit(ctx, r.RepoRoot, "fetch", "--no-tags", "--no-write-fetch-head", goalRemote(r), current); err != nil {
		return "", errors.New("current handoff main unavailable")
	}
	if _, err = goalGit(ctx, r.RepoRoot, "merge-base", "--is-ancestor", previous, current); err != nil {
		return "", errors.New("main no longer descends from verified accepted base")
	}
	decision, err := ResolveGoalRebinding(r.RepoRoot, r.BaseSHA, current, BaselineIdentity{SourceCommit: r.Target.PeeledCommitSHA, SourceRefName: r.Target.RefName, SourceRefKind: r.Target.RefKind})
	if err != nil || decision.Decision != GoalRebindingRun {
		return "", errors.New("main baseline blocks fixed-target handoff: " + decision.Reason)
	}
	return current, nil
}
func observeCompletedGoalCI(ctx context.Context, r GoalPublicationRequest, head string, _ time.Duration) (checkacceptance.Observation, error) {
	guard := func() error {
		current, err := goalMainForHandoff(ctx, r)
		if err != nil {
			return err
		}
		observationBinding := r
		observationBinding.VerificationBaseSHA = current
		return goalCurrent(ctx, observationBinding, head)
	}
	if err := guard(); err != nil {
		return checkacceptance.Observation{}, err
	}
	observation, err := checkacceptance.Observe(r.API, r.Repository, head)
	if err != nil {
		return observation, err
	}
	if err := guard(); err != nil {
		return observation, err
	}
	return observation, ctx.Err()
}
