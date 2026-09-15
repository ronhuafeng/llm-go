package protocolsync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
)

const (
	OutcomeCurrent = "current"
	OutcomeApplied = "applied"
)

// SyncRequest is the mechanical protocol-sync path owned by Go.
type SyncRequest struct {
	RepoRoot       string
	ModuleRoot     string
	UpstreamRepo   string
	UpstreamRef    string
	LatestStable   bool
	AllowDowngrade bool
	ForceCompare   bool
	EventName      string
	Lookuper       RemoteLookuper
	Generate       func(GenerateRequest) (Candidate, error)
	Apply          func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error)
}

// SyncResult is the minimal outcome the workflow needs.
type SyncResult struct {
	Outcome   string
	Reason    string
	Target    Target
	Candidate string
}

// Sync resolves, generates, compares, and applies deterministic protocol changes.
func Sync(req SyncRequest) (SyncResult, error) {
	if err := AssertClean(req.RepoRoot); err != nil {
		return SyncResult{}, err
	}
	moduleRoot := req.ModuleRoot
	if moduleRoot == "" {
		moduleRoot = filepath.Join(req.RepoRoot, "codexsdk")
	}
	upstreamRepo := req.UpstreamRepo
	if upstreamRepo == "" {
		upstreamRepo = defaultRemote
	}
	target, err := ResolveUpstream(ResolveRequest{
		Remote:       upstreamRepo,
		UpstreamRef:  req.UpstreamRef,
		LatestStable: req.LatestStable || strings.TrimSpace(req.UpstreamRef) == "",
		Lookuper:     req.Lookuper,
	})
	if err != nil {
		return SyncResult{}, err
	}
	if req.LatestStable && strings.TrimSpace(req.UpstreamRef) != "" {
		return SyncResult{}, fmt.Errorf("latest-stable cannot be combined with an explicit upstream ref")
	}
	if strings.TrimSpace(req.UpstreamRef) != "" {
		target.TargetExplicit = true
	}

	baseline, err := loadBaselineIdentity(filepath.Join(moduleRoot, filepath.FromSlash(defaultBaselineRel), "baseline_metadata.json"))
	if err != nil {
		return SyncResult{}, err
	}
	mode := "manual"
	if req.EventName == "schedule" {
		mode = "scheduled"
	}
	policy := EvaluatePolicy(PolicyRequest{
		Baseline:       baseline,
		TargetRef:      target.RefName,
		TargetKind:     target.RefKind,
		TargetSHA:      target.PeeledCommitSHA,
		TargetExplicit: target.TargetExplicit,
		Mode:           mode,
		AllowDowngrade: req.AllowDowngrade,
	})
	afterPolicy := decideAfterPolicy(policy.Decision, req.ForceCompare)
	result := SyncResult{Target: target, Reason: policy.Reason}
	if afterPolicy == "blocked" {
		return result, fmt.Errorf("%s", policy.Reason)
	}
	if afterPolicy == "current" {
		result.Outcome = OutcomeCurrent
		return result, nil
	}

	generate := req.Generate
	if generate == nil {
		generate = GenerateCandidate
	}
	candidate, err := generate(GenerateRequest{
		ModuleRoot:   moduleRoot,
		UpstreamRepo: upstreamRepo,
		Target:       target,
	})
	if err != nil {
		return result, err
	}
	if candidate.SourceCommit != target.PeeledCommitSHA {
		return result, fmt.Errorf("candidate source_commit does not match the resolved target")
	}
	result.Candidate = candidate.SchemaDir
	afterDrift := decideAfterDrift(req.ForceCompare, candidate.DriftStatus)
	if afterDrift == "comparison" || afterDrift == "comparison_dirty" {
		dirty, err := ChangedPaths(req.RepoRoot)
		if err != nil {
			return result, err
		}
		if len(dirty) > 0 {
			return result, fmt.Errorf("comparison must leave the protocol worktree unchanged:\n- %s", strings.Join(dirty, "\n- "))
		}
	}
	if afterDrift == "comparison" {
		result.Outcome = OutcomeCurrent
		result.Reason = "read-only comparison found no protocol drift"
		return result, nil
	}
	if afterDrift == "comparison_dirty" {
		return result, fmt.Errorf("read-only comparison found protocol drift; comparison never applies")
	}

	apply := req.Apply
	if apply == nil {
		apply = protocolupgrade.Apply
	}
	if _, err := apply(protocolupgrade.ApplyRequest{
		Baseline:          filepath.Join(moduleRoot, filepath.FromSlash(defaultBaselineRel)),
		Candidate:         candidate.SchemaDir,
		StableCandidate:   candidate.StableSchemaDir,
		CodexRepo:         candidate.CodexRepo,
		Reports:           candidate.ReportsDir,
		CommonRS:          candidate.CommonRS,
		CommonRSSourceSHA: candidate.CommonRSSourceSHA,
		TargetRef:         target.RefName,
		TargetKind:        target.RefKind,
		TargetSHA:         target.PeeledCommitSHA,
		ModuleRoot:        moduleRoot,
	}); err != nil {
		return result, fmt.Errorf("mechanical apply failed with a deterministic incompatibility: %w", err)
	}
	paths, err := ChangedPaths(req.RepoRoot)
	if err != nil {
		return result, err
	}
	if err := validatePaths(paths, "mechanical"); err != nil {
		return result, fmt.Errorf("mechanical apply escaped the generated sync surface: %w", err)
	}
	result.Outcome = OutcomeApplied
	result.Reason = "mechanical generation applied; workflow runs Agent then deterministic checks"
	return result, nil
}

func decideAfterPolicy(decision string, forceCompare bool) string {
	switch decision {
	case DecisionBlock:
		return "blocked"
	case DecisionSkip:
		if forceCompare {
			return "generate"
		}
		return "current"
	case DecisionAllow:
		return "generate"
	default:
		return "blocked"
	}
}

func decideAfterDrift(forceCompare bool, driftStatus string) string {
	if forceCompare {
		if driftStatus == "clean" {
			return "comparison"
		}
		return "comparison_dirty"
	}
	if driftStatus == "clean" {
		return "comparison"
	}
	return "apply"
}

func loadBaselineIdentity(path string) (BaselineIdentity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return BaselineIdentity{}, err
	}
	var metadata struct {
		SourceCommit  string `json:"source_commit"`
		SourceRefName string `json:"source_ref_name"`
		SourceRefKind string `json:"source_ref_kind"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return BaselineIdentity{}, fmt.Errorf("decode baseline metadata: %w", err)
	}
	return BaselineIdentity{
		SourceCommit:  metadata.SourceCommit,
		SourceRefName: metadata.SourceRefName,
		SourceRefKind: metadata.SourceRefKind,
	}, nil
}

// WriteGitHubOutput emits the workflow contract for a sync result.
func WriteGitHubOutput(path string, result SyncResult) error {
	if path == "" {
		return nil
	}
	applied := "false"
	if result.Outcome == OutcomeApplied {
		applied = "true"
	}
	lines := []string{
		"outcome=" + result.Outcome,
		"applied=" + applied,
		"target_ref=" + result.Target.RefName,
		"target_kind=" + result.Target.RefKind,
		"target_sha=" + result.Target.PeeledCommitSHA,
		"candidate=" + result.Candidate,
		"reason=" + result.Reason,
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	return err
}
