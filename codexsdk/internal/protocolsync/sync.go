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
	OutcomeBaselineMatches    = "baseline_matches"
	OutcomeSchemasMatch       = "schemas_match"
	OutcomeExactVerified      = "exact_verified"
	OutcomeFailed             = "failed"
	OutcomeApplied            = "applied"
	OutcomePlanReady          = "plan_ready"
	OutcomePRPending          = "pr_pending"
	OutcomeSemanticUnresolved = "semantic_unresolved"
)

// SyncRequest is the protocol-sync path owned by Go.
type SyncRequest struct {
	Publication    *PendingRequest
	RepoRoot       string
	ModuleRoot     string
	UpstreamRepo   string
	UpstreamRef    string
	LatestStable   bool
	AllowDowngrade bool
	ForceCompare   bool
	ValidationOnly bool
	Diagnostic     bool
	EventName      string
	Lookuper       RemoteLookuper
	Generate       func(GenerateRequest) (Candidate, error)
	Plan           func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error)
	VerifyExact    func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error)
	Apply          func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error)
}

// ResumeRequest re-plans and applies the exact candidate after one targeted
// handwritten Agent pass.
type ResumeRequest struct {
	RepoRoot        string
	ModuleRoot      string
	CandidateDir    string
	CandidateSHA256 string
	TargetRef       string
	TargetKind      string
	TargetSHA       string
	Plan            func(protocolupgrade.ApplyRequest) (protocolupgrade.PlanResult, error)
	Apply           func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error)
}

// SyncResult is the minimal outcome the workflow needs.
type SyncResult struct {
	Publication     *PendingPublication
	Stage           string
	FailureCategory string
	Outcome         string
	Reason          string
	Target          Target
	Candidate       string
	CandidateDir    string
	CandidateSHA256 string
	Issue           *protocolupgrade.PlanIssue
}

// Sync resolves, generates, plans, and applies only a fully planned candidate.
// Semantic/generator incompatibility returns a read-only escalation outcome.
func Sync(req SyncRequest) (result SyncResult, err error) {
	stage := "configuration"
	defer func() { finishSync(&result, err, stage) }()
	if req.ValidationOnly && req.Diagnostic {
		return result, &Failure{Category: FailurePolicy, Err: fmt.Errorf("validation-only cannot be combined with diagnostic mode")}
	}
	if req.ValidationOnly && !req.ForceCompare {
		return result, &Failure{Category: FailurePolicy, Err: fmt.Errorf("validation-only requires force-compare")}
	}
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
	stage = "resolve"
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

	result.Target = target
	stage = "baseline"
	baseline, err := loadBaselineIdentity(filepath.Join(moduleRoot, filepath.FromSlash(defaultBaselineRel), "baseline_metadata.json"))
	if err != nil {
		return result, err
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
	afterPolicy := decideAfterPolicy(policy.Decision, req.ForceCompare || req.Diagnostic || req.ValidationOnly)
	result.Reason = policy.Reason
	stage = "policy"
	if afterPolicy == "blocked" {
		return result, &Failure{Category: FailurePolicy, Err: fmt.Errorf("%s", policy.Reason)}
	}
	if afterPolicy == "current" {
		result.Outcome = OutcomeBaselineMatches
		return result, nil
	}

	if req.Publication != nil && !req.ValidationOnly && !req.Diagnostic {
		stage = "pending_publication"
		inspection := *req.Publication
		inspection.RepoRoot = req.RepoRoot
		inspection.Target = target
		inspection.ReadChecks = true
		base, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
		if err != nil {
			return result, err
		}
		inspection.BaseSHA = strings.TrimSpace(base)
		pending, err := InspectPending(inspection)
		if err != nil {
			return result, err
		}
		result.Publication = &pending
		if pending.Reusable && !req.ForceCompare {
			result.Outcome = OutcomePRPending
			result.Reason = fmt.Sprintf("%s remains pending: %s; no generation, Agent pass, or fresh proof", pending.URL, pending.Checks)
			return result, nil
		}
	}

	generate := req.Generate
	if generate == nil {
		generate = GenerateCandidate
	}
	stage = "generate"
	candidate, err := generate(GenerateRequest{
		ModuleRoot:   moduleRoot,
		UpstreamRepo: upstreamRepo,
		Target:       target,
	})
	if err != nil {
		return result, err
	}
	if candidate.SourceCommit != target.PeeledCommitSHA {
		return result, &Failure{Category: FailureSource, Err: fmt.Errorf("candidate source_commit does not match the resolved target")}
	}
	result.Candidate = candidate.SchemaDir
	result.CandidateDir = candidate.Dir
	if req.ValidationOnly {
		stage = "exact_verification"
		verify := req.VerifyExact
		if verify == nil {
			verify = protocolupgrade.VerifyExact
		}
		proof, err := verify(candidateApplyRequest(moduleRoot, candidate, target))
		if err != nil {
			return result, fmt.Errorf("exact upstream verification: %w", err)
		}
		if proof.Status != protocolupgrade.PlanReady {
			if proof.Issue != nil {
				return result, &Failure{Category: FailureUnsupported, Err: fmt.Errorf("exact upstream verification unresolved at %s %s: %s", proof.Issue.Stage, proof.Issue.Path, proof.Issue.Reason)}
			}
			return result, fmt.Errorf("exact upstream verification returned %q", proof.Status)
		}
		if err := AssertClean(req.RepoRoot); err != nil {
			return result, fmt.Errorf("exact upstream verification changed the accepted worktree: %w", err)
		}
		result.Outcome = OutcomeExactVerified
		result.Reason = "fresh exact upstream reconstruction matches all accepted semantic artifacts"
		return result, nil
	}

	if req.ForceCompare && !req.Diagnostic {
		stage = "schema_comparison"
		dirty, err := ChangedPaths(req.RepoRoot)
		if err != nil {
			return result, err
		}
		if len(dirty) > 0 {
			return result, fmt.Errorf("comparison must leave the protocol worktree unchanged:\n- %s", strings.Join(dirty, "\n- "))
		}
		if candidate.DriftStatus == "clean" {
			result.Outcome = OutcomeSchemasMatch
			result.Reason = "read-only comparison found no protocol drift"
			return result, nil
		}
		return result, &Failure{Category: FailureValidation, Err: fmt.Errorf("read-only comparison found protocol drift; comparison never applies")}
	}

	applyReq := candidateApplyRequest(moduleRoot, candidate, target)
	plan := req.Plan
	if plan == nil {
		plan = protocolupgrade.PlanRepository
	}
	stage = "plan"
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
		fingerprint, err := candidateDigest(candidate.Dir)
		if err != nil {
			return result, fmt.Errorf("snapshot unresolved candidate: %w", err)
		}
		result.CandidateSHA256 = fingerprint
		result.Outcome = OutcomeSemanticUnresolved
		result.FailureCategory = FailureUnsupported
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
		apply = func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) { return planned.Apply() }
	}
	stage = "apply"
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
func Resume(req ResumeRequest) (result SyncResult, err error) {
	stage := "proposal_scope"
	result.Target = Target{RefName: req.TargetRef, RefKind: req.TargetKind, PeeledCommitSHA: req.TargetSHA, TargetExplicit: true}
	defer func() { finishSync(&result, err, stage) }()
	if req.RepoRoot == "" || req.CandidateDir == "" {
		return result, fmt.Errorf("repo-root and candidate-dir are required")
	}
	if req.TargetRef == "" || req.TargetKind == "" || req.TargetSHA == "" {
		return result, fmt.Errorf("target ref, kind, and sha are required")
	}
	paths, err := ChangedPaths(req.RepoRoot)
	if err != nil {
		return result, err
	}
	if err := validatePaths(paths, "agent"); err != nil {
		return result, fmt.Errorf("Agent pass escaped handwritten codexsdk scope: %w", err)
	}
	stage = "candidate_integrity"
	verifiedDir, cleanup, err := copyVerifiedCandidate(req.CandidateDir, req.CandidateSHA256, req.TargetRef, req.TargetKind, req.TargetSHA)
	if err != nil {
		return result, &Failure{Category: FailureSource, Err: fmt.Errorf("verify initial candidate: %w", err)}
	}
	defer cleanup()

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
	candidate := candidateFromDir(verifiedDir, moduleRoot, target)
	result = SyncResult{
		Target:          target,
		Candidate:       filepath.Join(req.CandidateDir, "schema"),
		CandidateDir:    req.CandidateDir,
		CandidateSHA256: req.CandidateSHA256,
	}
	applyReq := candidateApplyRequest(moduleRoot, candidate, target)
	plan := req.Plan
	if plan == nil {
		plan = protocolupgrade.PlanRepository
	}
	stage = "replan"
	planned, err := plan(applyReq)
	if err != nil {
		return result, fmt.Errorf("re-plan candidate: %w", err)
	}
	if planned.Status == protocolupgrade.PlanSemanticUnresolved {
		result.Outcome = OutcomeSemanticUnresolved
		result.Issue = planned.Issue
		if planned.Issue != nil {
			result.Reason = planned.Issue.Reason
			return result, &Failure{Category: FailureUnsupported, Err: fmt.Errorf("semantic drift remains unresolved after Agent: %s", planned.Issue.Reason)}
		}
		return result, &Failure{Category: FailureUnsupported, Err: fmt.Errorf("semantic drift remains unresolved after Agent")}
	}
	if planned.Status != protocolupgrade.PlanReady {
		return result, fmt.Errorf("unknown re-plan status %q", planned.Status)
	}

	apply := req.Apply
	if apply == nil {
		apply = func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) { return planned.Apply() }
	}
	stage = "apply"
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
	return decodeBaselineIdentity(raw)
}

func decodeBaselineIdentity(raw []byte) (BaselineIdentity, error) {
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

	publicationHead, publicationBranch, publicationNumber, prURL, publicationChecks := "", "", "", "", ""
	if result.Publication != nil {
		publicationHead = result.Publication.Head
		publicationBranch = result.Publication.Branch
		publicationNumber = fmt.Sprint(result.Publication.Number)
		prURL = result.Publication.URL
		publicationChecks = result.Publication.Checks
	}
	lines := []string{
		"outcome=" + githubOutputValue(result.Outcome),
		"publication_head=" + githubOutputValue(publicationHead),
		"publication_branch=" + githubOutputValue(publicationBranch),
		"publication_number=" + githubOutputValue(publicationNumber),
		"pr_url=" + githubOutputValue(prURL),
		"publication_checks=" + githubOutputValue(publicationChecks),
		"stage=" + githubOutputValue(result.Stage),
		"failure_category=" + githubOutputValue(result.FailureCategory),
		"applied=" + applied,
		"target_ref=" + githubOutputValue(result.Target.RefName),
		"target_kind=" + githubOutputValue(result.Target.RefKind),
		"target_sha=" + githubOutputValue(result.Target.PeeledCommitSHA),
		"candidate=" + githubOutputValue(result.Candidate),
		"candidate_dir=" + githubOutputValue(result.CandidateDir),
		"candidate_sha256=" + githubOutputValue(result.CandidateSHA256),
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
