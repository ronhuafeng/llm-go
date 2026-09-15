package protocolsync

import (
	"fmt"
	"regexp"
	"strconv"
)

const (
	KindStableTag    = "stable_rust_tag"
	KindManualRef    = "manual_ref"
	KindManualCommit = "manual_commit"

	DecisionAllow = "allow"
	DecisionSkip  = "skip"
	DecisionBlock = "block"
)

var (
	rustTagRE = regexp.MustCompile(`^rust-v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	shaRE     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

var validKinds = map[string]bool{
	KindStableTag:    true,
	KindManualRef:    true,
	KindManualCommit: true,
}

// BaselineIdentity is the checked-in upstream identity.
type BaselineIdentity struct {
	SourceCommit  string
	SourceRefName string
	SourceRefKind string
}

// PolicyRequest is one target-policy evaluation.
type PolicyRequest struct {
	Baseline       BaselineIdentity
	TargetRef      string
	TargetKind     string
	TargetSHA      string
	TargetExplicit bool
	Mode           string
	AllowDowngrade bool
}

// PolicyDecision is the target-policy result.
type PolicyDecision struct {
	Decision        string
	Reason          string
	BaselineRefName string
	BaselineRefKind string
	BaselineCommit  string
	TargetRefName   string
	TargetRefKind   string
	TargetCommit    string
	TargetExplicit  bool
	Mode            string
}

// EvaluatePolicy decides whether a selected upstream target may be compared or applied.
func EvaluatePolicy(req PolicyRequest) PolicyDecision {
	mode := req.Mode
	if mode == "" {
		mode = "manual"
	}
	decision := func(kind, reason string) PolicyDecision {
		return PolicyDecision{
			Decision:        kind,
			Reason:          reason,
			BaselineRefName: req.Baseline.SourceRefName,
			BaselineRefKind: req.Baseline.SourceRefKind,
			BaselineCommit:  req.Baseline.SourceCommit,
			TargetRefName:   req.TargetRef,
			TargetRefKind:   req.TargetKind,
			TargetCommit:    req.TargetSHA,
			TargetExplicit:  req.TargetExplicit,
			Mode:            mode,
		}
	}

	if !shaRE.MatchString(req.Baseline.SourceCommit) || req.Baseline.SourceRefName == "" || !validKinds[req.Baseline.SourceRefKind] {
		return decision(DecisionBlock, "baseline source identity is incomplete or unsupported")
	}
	if !validKinds[req.TargetKind] || !shaRE.MatchString(req.TargetSHA) {
		return decision(DecisionBlock, "target source identity is incomplete or unsupported")
	}
	if req.Baseline.SourceCommit == req.TargetSHA {
		return decision(DecisionSkip, "baseline already points at the selected upstream commit")
	}

	if req.TargetKind == KindStableTag {
		targetVersion, ok := parseRustTag(req.TargetRef)
		if !ok {
			return decision(DecisionBlock, fmt.Sprintf("stable target is not a rust-vX.Y.Z tag: %s", req.TargetRef))
		}
		if req.Baseline.SourceRefKind == KindStableTag {
			baselineVersion, ok := parseRustTag(req.Baseline.SourceRefName)
			if !ok {
				return decision(DecisionBlock, fmt.Sprintf("stable baseline is not a rust-vX.Y.Z tag: %s", req.Baseline.SourceRefName))
			}
			if compareVersion(targetVersion, baselineVersion) > 0 {
				return decision(DecisionAllow, "stable tag moves forward by version")
			}
			if compareVersion(targetVersion, baselineVersion) == 0 {
				return decision(DecisionBlock, "stable tag name matches baseline but peeled commit changed")
			}
			if req.AllowDowngrade && req.TargetExplicit {
				return decision(DecisionAllow, "explicit stable tag downgrade requested")
			}
			return decision(DecisionBlock, "stable tag target is older than the current baseline tag")
		}
		if req.Baseline.SourceRefKind == KindManualRef || req.Baseline.SourceRefKind == KindManualCommit {
			if req.TargetExplicit {
				return decision(DecisionAllow, "explicit track switch from manual baseline to stable tag")
			}
			return decision(DecisionBlock, "current baseline is manual; automatic stable-tag drift detection would switch tracks")
		}
	}

	if req.TargetKind == KindManualRef || req.TargetKind == KindManualCommit {
		if req.TargetExplicit {
			return decision(DecisionAllow, "explicit manual upstream target requested")
		}
		return decision(DecisionBlock, "manual upstream targets must be explicit")
	}
	return decision(DecisionBlock, "target policy did not match any allowed transition")
}

func parseRustTag(ref string) ([3]int, bool) {
	match := rustTagRE.FindStringSubmatch(ref)
	if match == nil {
		return [3]int{}, false
	}
	var version [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return [3]int{}, false
		}
		version[i] = n
	}
	return version, true
}

func compareVersion(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

func inferRefKind(ref string) string {
	if rustTagRE.MatchString(ref) {
		return KindStableTag
	}
	if shaRE.MatchString(ref) {
		return KindManualCommit
	}
	return KindManualRef
}
