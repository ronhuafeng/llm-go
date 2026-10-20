package protocolsync

import (
	"context"
	"errors"
)

// ValidateGoalIntegrationHandoff is the minimal FF-only consumer of a completed
// handoff. CI/review/remote-ref checks still belong to existing Auto-forward.
func ValidateGoalIntegrationHandoff(ctx context.Context, root, original, verified, current, head string, target Target) error {
	return validateGoalIntegrationHandoff(ctx, root, original, verified, current, head, target, nil)
}
func validateGoalIntegrationHandoff(ctx context.Context, root, original, verified, current, head string, target Target, lookup RemoteLookuper) error {
	if !shaRE.MatchString(head) || !shaRE.MatchString(original) || !shaRE.MatchString(current) || !shaRE.MatchString(verified) {
		return errors.New("exact B/M/H handoff identities required")
	}
	if _, err := goalGit(ctx, root, "merge-base", "--is-ancestor", original, verified); err != nil {
		return errors.New("verified baseline does not descend from original")
	}
	if current != verified {
		return errors.New("main advanced beyond verified contracts; convergence verification required")
	}
	for _, base := range []string{original, current} {
		if _, err := goalGit(ctx, root, "merge-base", "--is-ancestor", base, head); err != nil {
			return errors.New("handoff candidate does not contain accepted baseline")
		}
	}
	if _, err := goalGit(ctx, root, "merge-base", "--is-ancestor", verified, current); err != nil {
		return errors.New("main no longer descends from verified baseline")
	}
	identity, err := gitBaselineIdentity(root, head)
	if err != nil || identity.SourceCommit != target.PeeledCommitSHA || identity.SourceRefName != target.RefName || identity.SourceRefKind != target.RefKind {
		return errors.New("candidate baseline differs from fixed handoff target")
	}
	decision, err := ResolveGoalRebinding(root, original, current, identity)
	if err != nil || decision.Decision != GoalRebindingRun {
		return errors.New("fixed-target integration blocked: " + decision.Reason)
	}
	selected, err := ResolveUpstream(ResolveRequest{UpstreamRef: target.RefName, Lookuper: goalLookupContext(ctx, lookup)})
	if err != nil || selected.RefKind != target.RefKind || selected.PeeledCommitSHA != target.PeeledCommitSHA {
		return errors.New("upstream handoff target moved")
	}
	return ctx.Err()
}
