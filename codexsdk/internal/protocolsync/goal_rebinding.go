package protocolsync

import "fmt"

const (
	GoalRebindingRun             = "run"
	GoalRebindingAlreadyAccepted = "already_accepted"
	GoalRebindingSuperseded      = "superseded"
	GoalRebindingBlocked         = "blocked"
)

// GoalRebindingRequest keeps the original promise B separate from current main M
// and the immutable upstream target U. These are trusted controller inputs.
type GoalRebindingRequest struct {
	OriginalBaseSHA, CurrentBaseSHA           string
	OriginalBaseline, CurrentBaseline, Target BaselineIdentity
}

type GoalRebindingDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// EvaluateGoalRebinding classifies a fixed stable target without changing it.
// Manual-track integration has no monotonic ordering and fails closed.
func EvaluateGoalRebinding(req GoalRebindingRequest) GoalRebindingDecision {
	result := func(decision, reason string) GoalRebindingDecision { return GoalRebindingDecision{decision, reason} }
	blocked := func(reason string) GoalRebindingDecision { return result(GoalRebindingBlocked, reason) }
	if !shaRE.MatchString(req.OriginalBaseSHA) || !shaRE.MatchString(req.CurrentBaseSHA) {
		return blocked("exact original and current repository commits are required")
	}
	identities := []BaselineIdentity{req.OriginalBaseline, req.CurrentBaseline, req.Target}
	for _, identity := range identities {
		if !shaRE.MatchString(identity.SourceCommit) || identity.SourceRefKind != KindStableTag {
			return blocked("rebinding requires complete stable-track identities")
		}
		if _, ok := parseRustTag(identity.SourceRefName); !ok {
			return blocked("rebinding requires stable rust-vX.Y.Z tags")
		}
	}
	if req.OriginalBaseSHA == req.CurrentBaseSHA && req.OriginalBaseline != req.CurrentBaseline {
		return blocked("one repository commit has conflicting baseline identities")
	}
	for i, a := range identities {
		for _, b := range identities[i+1:] {
			if a.SourceRefName == b.SourceRefName && a.SourceCommit != b.SourceCommit {
				return blocked("stable tag name matches but peeled commit changed")
			}
			if a.SourceCommit == b.SourceCommit && a.SourceRefName != b.SourceRefName {
				return blocked("upstream commit has conflicting stable tag identities")
			}
		}
	}
	// Reuse the existing target policy; rebinding never authorizes a downgrade.
	policy := func(base, target BaselineIdentity) PolicyDecision {
		return EvaluatePolicy(PolicyRequest{Baseline: base, TargetRef: target.SourceRefName, TargetKind: target.SourceRefKind, TargetSHA: target.SourceCommit})
	}
	if p := policy(req.OriginalBaseline, req.CurrentBaseline); p.Decision == DecisionBlock {
		return blocked("current main baseline: " + p.Reason)
	}
	if p := policy(req.OriginalBaseline, req.Target); p.Decision == DecisionBlock {
		return blocked("fixed target: " + p.Reason)
	}
	if req.CurrentBaseline == req.Target {
		return result(GoalRebindingAlreadyAccepted, "current main already contains the exact fixed upstream target")
	}
	if p := policy(req.CurrentBaseline, req.Target); p.Decision == DecisionAllow {
		return result(GoalRebindingRun, "fixed target advances current main; preserve original and current baseline contracts")
	}
	return result(GoalRebindingSuperseded, "current main contains a newer stable upstream target")
}

// ResolveGoalRebinding reads baseline metadata from exact immutable Git commits.
// It does not fetch, rewrite a candidate, publish, or reuse previous CI results.
func ResolveGoalRebinding(repoRoot, originalBaseSHA, currentBaseSHA string, target BaselineIdentity) (GoalRebindingDecision, error) {
	blocked := GoalRebindingDecision{Decision: GoalRebindingBlocked}
	if !shaRE.MatchString(originalBaseSHA) || !shaRE.MatchString(currentBaseSHA) {
		return blocked, fmt.Errorf("rebinding requires exact repository commits")
	}
	original, err := gitBaselineIdentity(repoRoot, originalBaseSHA)
	if err != nil {
		return blocked, fmt.Errorf("original accepted baseline: %w", err)
	}
	current, err := gitBaselineIdentity(repoRoot, currentBaseSHA)
	if err != nil {
		return blocked, fmt.Errorf("current accepted baseline: %w", err)
	}
	return EvaluateGoalRebinding(GoalRebindingRequest{OriginalBaseSHA: originalBaseSHA, CurrentBaseSHA: currentBaseSHA, OriginalBaseline: original, CurrentBaseline: current, Target: target}), nil
}
