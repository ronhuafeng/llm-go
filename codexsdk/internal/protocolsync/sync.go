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
	OutcomeCurrent            = "current"
	OutcomeApplied            = "applied"
	OutcomePlanReady          = "plan_ready"
	OutcomeSemanticUnresolved = "semantic_unresolved"
)

// SyncRequest is the protocol-sync path owned by Go.
type SyncRequest struct {
	RepoRoot       string
	ModuleRoot     string
	UpstreamRepo   string
	UpstreamRef    string
	LatestStable   bool
	AllowDowngrade bool
	ForceCompare   bool
	Diagnostic     bool
	EventName      string
	Lookuper       RemoteLookuper
	Generate       func(GenerateRequest) (Candidate, error)
	Plan           func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error)
	Apply          func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error)
}

// ResumeRequest re-plans and applies the exact candidate after one targeted
// handwritten Agent pass.
type ResumeRequest struct {
	RepoRoot     string
	ModuleRoot   string
	CandidateDir string
	TargetRef    string
	TargetKind   string
	TargetSHA    string
	Plan         func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error)
	Apply        func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error)
}

// SyncResult is the minimal outcome the workflow needs.
type SyncResult struct {
	Outcome      string
	Reason       string
	Target       Target
	Candidate    string
	CandidateDir string
	Issue        *protocolupgrade.PlanIssue
}

// Sync resolves, generates, plans, and applies only a fully planned candidate.
// Semantic/generator incompatibility returns a read-only escalation outcome.
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
	afterPolicy := decideAfterPolicy(policy.Decision, req.ForceCompare || req.Diagnostic)
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
	result.CandidateDir = candidate.Dir

	if req.ForceCompare && !req.Diagnostic {
		dirty, err := ChangedPaths(req.RepoRoot)
		if err != nil {
			return result, err
		}
		if len(dirty) > 0 {
			return result, fmt.Errorf("comparison must leave the protocol worktree unchanged:\n- %s", strings.Join(dirty, "\n- "))
		}
		if candidate.DriftStatus == "clean" {
			result.Outcome = OutcomeCurrent
			result.Reason = "read-only comparison found no protocol drift"
			return result, nil
		}
		return result, fmt.Errorf("read-only comparison found protocol drift; comparison never applies")
	}

	applyReq := candidateApplyRequest(moduleRoot, candidate, target)
	plan := req.Plan
	if plan == nil {
		plan = protocolupgrade.Plan
	}
	planned, err := plan(applyReq)
	if err != nil {
		if req.Diagnostic {
			if cleanErr := AssertClean(req.RepoRoot); cleanErr != nil {
				return result, fmt.Errorf("diagnostic planning changed the accepted worktree: %w (plan error: %v)", cleanErr, err)
			}
		}
		return result, fmt.Errorf("plan candidate: %w", err)
	}
	if planned.Status == protocolupgrade.PlanSemanticUnresolved {
		dirty, err := ChangedPaths(req.RepoRoot)
		if err != nil {
			return result, err
		}
		if len(dirty) > 0 {
			return result, fmt.Errorf("semantic planning must leave the protocol worktree unchanged:\n- %s", strings.Join(dirty, "\n- "))
		}
		result.Outcome = OutcomeSemanticUnresolved
		result.Issue = planned.Issue
		if planned.Issue != nil {
			result.Reason = planned.Issue.Reason
		} else {
			result.Reason = "candidate has unresolved protocol semantics"
		}
		return result, nil
	}
	if planned.Status != protocolupgrade.PlanReady {
		return result, fmt.Errorf("unknown plan status %q", planned.Status)
	}
	if req.Diagnostic {
		if err := AssertClean(req.RepoRoot); err != nil {
			return result, fmt.Errorf("diagnostic planning changed the accepted worktree: %w", err)
		}
		result.Outcome = OutcomePlanReady
		result.Reason = "read-only candidate plan is ready"
		return result, nil
	}

	provenanceOnly := candidate.DriftStatus == "clean" && planned.Preview.GeneratedReleaseImpact == "metadata-only"
	apply := req.Apply
	if apply == nil {
		apply = protocolupgrade.Apply
	}
	if _, err := apply(applyReq); err != nil {
		return result, fmt.Errorf("apply planned candidate: %w", err)
	}
	paths, err := ChangedPaths(req.RepoRoot)
	if err != nil {
		return result, err
	}
	if err := validatePaths(paths, "mechanical"); err != nil {
		return result, fmt.Errorf("planned apply escaped the generated sync surface: %w", err)
	}
	result.Outcome = OutcomeApplied
	if provenanceOnly {
		result.Reason = "provenance-only candidate planned and applied"
	} else {
		result.Reason = "mechanical candidate planned and applied"
	}
	return result, nil
}

// Resume verifies the Agent touched only handwritten codexsdk paths, re-plans
// the same target/candidate, then applies only if the second plan is complete.
func Resume(req ResumeRequest) (SyncResult, error) {
	if req.RepoRoot == "" || req.CandidateDir == "" {
		return SyncResult{}, fmt.Errorf("repo-root and candidate-dir are required")
	}
	if req.TargetRef == "" || req.TargetKind == "" || req.TargetSHA == "" {
		return SyncResult{}, fmt.Errorf("target ref, kind, and sha are required")
	}
	paths, err := ChangedPaths(req.RepoRoot)
	if err != nil {
		return SyncResult{}, err
	}
	if err := validatePaths(paths, "agent"); err != nil {
		return SyncResult{}, fmt.Errorf("Agent pass escaped handwritten codexsdk scope: %w", err)
	}

	moduleRoot := req.ModuleRoot
	if moduleRoot == "" {
		moduleRoot = filepath.Join(req.RepoRoot, "codexsdk")
	}
	target := Target{
		RefName:         req.TargetRef,
		RefKind:         req.TargetKind,
		PeeledCommitSHA: req.TargetSHA,
		TargetExplicit:  true,
	}
	candidate := candidateFromDir(req.CandidateDir, moduleRoot, target)
	result := SyncResult{
		Target:       target,
		Candidate:    candidate.SchemaDir,
		CandidateDir: candidate.Dir,
	}
	applyReq := candidateApplyRequest(moduleRoot, candidate, target)
	plan := req.Plan
	if plan == nil {
		plan = protocolupgrade.Plan
	}
	planned, err := plan(applyReq)
	if err != nil {
		return result, fmt.Errorf("re-plan candidate: %w", err)
	}
	if planned.Status == protocolupgrade.PlanSemanticUnresolved {
		result.Outcome = OutcomeSemanticUnresolved
		result.Issue = planned.Issue
		if planned.Issue != nil {
			result.Reason = planned.Issue.Reason
			return result, fmt.Errorf("semantic drift remains unresolved after Agent: %s", planned.Issue.Reason)
		}
		return result, fmt.Errorf("semantic drift remains unresolved after Agent")
	}
	if planned.Status != protocolupgrade.PlanReady {
		return result, fmt.Errorf("unknown re-plan status %q", planned.Status)
	}

	apply := req.Apply
	if apply == nil {
		apply = protocolupgrade.Apply
	}
	if _, err := apply(applyReq); err != nil {
		return result, fmt.Errorf("apply re-planned candidate: %w", err)
	}
	paths, err = ChangedPaths(req.RepoRoot)
	if err != nil {
		return result, err
	}
	if err := validatePaths(paths, "final"); err != nil {
		return result, fmt.Errorf("final sync surface invalid: %w", err)
	}
	result.Outcome = OutcomeApplied
	result.Reason = "Agent proposal re-planned successfully; candidate applied"
	return result, nil
}

func candidateFromDir(dir, moduleRoot string, target Target) Candidate {
	return Candidate{
		Dir:               dir,
		SchemaDir:         filepath.Join(dir, "schema"),
		StableSchemaDir:   filepath.Join(dir, "stable-schema"),
		ReportsDir:        filepath.Join(dir, "reports"),
		CommonRS:          filepath.Join(dir, "common.rs"),
		CommonRSSourceSHA: target.PeeledCommitSHA,
		CodexRepo:         filepath.Join(moduleRoot, ".cache", "openai-codex"),
		SourceCommit:      target.PeeledCommitSHA,
	}
}

func candidateApplyRequest(moduleRoot string, candidate Candidate, target Target) protocolupgrade.ApplyRequest {
	return protocolupgrade.ApplyRequest{
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
	}
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
	issueStage, issuePath, issueReason := "", "", ""
	if result.Issue != nil {
		issueStage = result.Issue.Stage
		issuePath = result.Issue.Path
		issueReason = result.Issue.Reason
	}
	lines := []string{
		"outcome=" + githubOutputValue(result.Outcome),
		"applied=" + applied,
		"target_ref=" + githubOutputValue(result.Target.RefName),
		"target_kind=" + githubOutputValue(result.Target.RefKind),
		"target_sha=" + githubOutputValue(result.Target.PeeledCommitSHA),
		"candidate=" + githubOutputValue(result.Candidate),
		"candidate_dir=" + githubOutputValue(result.CandidateDir),
		"reason=" + githubOutputValue(result.Reason),
		"issue_stage=" + githubOutputValue(issueStage),
		"issue_path=" + githubOutputValue(issuePath),
		"issue_reason=" + githubOutputValue(issueReason),
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	return err
}

func githubOutputValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
