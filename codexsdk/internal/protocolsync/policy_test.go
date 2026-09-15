package protocolsync

import (
	"strings"
	"testing"
)

const (
	oldSHA = "1111111111111111111111111111111111111111"
	newSHA = "2222222222222222222222222222222222222222"
)

func TestEvaluatePolicySameCommitSkips(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:   BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0", SourceRefKind: KindStableTag},
		TargetRef:  "rust-v0.141.0",
		TargetKind: KindStableTag,
		TargetSHA:  oldSHA,
	})
	if decision.Decision != DecisionSkip {
		t.Fatalf("decision = %s, want skip", decision.Decision)
	}
}

func TestEvaluatePolicyStableTagForwardAllows(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:   BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0", SourceRefKind: KindStableTag},
		TargetRef:  "rust-v0.141.0",
		TargetKind: KindStableTag,
		TargetSHA:  newSHA,
	})
	if decision.Decision != DecisionAllow || !strings.Contains(decision.Reason, "moves forward") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluatePolicyStableTagDowngradeBlocksByDefault(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:       BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.141.0", SourceRefKind: KindStableTag},
		TargetRef:      "rust-v0.140.0",
		TargetKind:     KindStableTag,
		TargetSHA:      newSHA,
		TargetExplicit: true,
	})
	if decision.Decision != DecisionBlock {
		t.Fatalf("decision = %s, want block", decision.Decision)
	}
}

func TestEvaluatePolicyStableTagDowngradeAllowsWhenEnabled(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:       BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.141.0", SourceRefKind: KindStableTag},
		TargetRef:      "rust-v0.140.0",
		TargetKind:     KindStableTag,
		TargetSHA:      newSHA,
		TargetExplicit: true,
		AllowDowngrade: true,
	})
	if decision.Decision != DecisionAllow || !strings.Contains(decision.Reason, "downgrade") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluatePolicySameTagDifferentSHABlocks(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:   BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0", SourceRefKind: KindStableTag},
		TargetRef:  "rust-v0.140.0",
		TargetKind: KindStableTag,
		TargetSHA:  newSHA,
	})
	if decision.Decision != DecisionBlock || !strings.Contains(decision.Reason, "peeled commit changed") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluatePolicyManualBaselineToDefaultStableBlocks(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:   BaselineIdentity{SourceCommit: oldSHA, SourceRefName: oldSHA, SourceRefKind: KindManualCommit},
		TargetRef:  "rust-v0.141.0",
		TargetKind: KindStableTag,
		TargetSHA:  newSHA,
	})
	if decision.Decision != DecisionBlock || !strings.Contains(decision.Reason, "switch tracks") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluatePolicyManualBaselineToExplicitStableAllows(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:       BaselineIdentity{SourceCommit: oldSHA, SourceRefName: oldSHA, SourceRefKind: KindManualCommit},
		TargetRef:      "rust-v0.141.0",
		TargetKind:     KindStableTag,
		TargetSHA:      newSHA,
		TargetExplicit: true,
	})
	if decision.Decision != DecisionAllow || !strings.Contains(decision.Reason, "track switch") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluatePolicyExplicitManualCommitAllows(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:       BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0", SourceRefKind: KindStableTag},
		TargetRef:      newSHA,
		TargetKind:     KindManualCommit,
		TargetSHA:      newSHA,
		TargetExplicit: true,
	})
	if decision.Decision != DecisionAllow || !strings.Contains(decision.Reason, "manual upstream target") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluatePolicyInvalidStableTargetBlocks(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:   BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0", SourceRefKind: KindStableTag},
		TargetRef:  "main",
		TargetKind: KindStableTag,
		TargetSHA:  newSHA,
	})
	if decision.Decision != DecisionBlock || !strings.Contains(decision.Reason, "not a rust-vX.Y.Z tag") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluatePolicyIncompleteBaselineBlocks(t *testing.T) {
	bases := []BaselineIdentity{
		{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0", SourceRefKind: "obsolete"},
		{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0"},
		{SourceCommit: oldSHA, SourceRefKind: KindStableTag},
		{SourceRefName: "rust-v0.140.0", SourceRefKind: KindStableTag},
	}
	for _, base := range bases {
		decision := EvaluatePolicy(PolicyRequest{
			Baseline:   base,
			TargetRef:  "rust-v0.141.0",
			TargetKind: KindStableTag,
			TargetSHA:  newSHA,
		})
		if decision.Decision != DecisionBlock || !strings.Contains(decision.Reason, "baseline source identity") {
			t.Fatalf("baseline %+v: decision = %+v", base, decision)
		}
	}
}

func TestEvaluatePolicyDoesNotInferTargetKind(t *testing.T) {
	decision := EvaluatePolicy(PolicyRequest{
		Baseline:  BaselineIdentity{SourceCommit: oldSHA, SourceRefName: "rust-v0.140.0", SourceRefKind: KindStableTag},
		TargetRef: "rust-v0.141.0",
		TargetSHA: newSHA,
	})
	if decision.Decision != DecisionBlock || !strings.Contains(decision.Reason, "target source identity") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestInferRefKind(t *testing.T) {
	if got := inferRefKind("rust-v0.100.0"); got != KindStableTag {
		t.Fatalf("got %s", got)
	}
	if got := inferRefKind(oldSHA); got != KindManualCommit {
		t.Fatalf("got %s", got)
	}
	if got := inferRefKind("main"); got != KindManualRef {
		t.Fatalf("got %s", got)
	}
}
