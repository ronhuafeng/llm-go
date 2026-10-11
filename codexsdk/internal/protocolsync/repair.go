package protocolsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
)

// RepairRequest belongs to the trusted control binary, never the proposal.
// The mutable workspace contains handwritten proposals only. Each oracle round
// gets a fresh clone, so failed Apply output never becomes the next proposal.
type RepairRequest struct {
	ResumeRequest
	CheckpointDir string
	MaxRounds     int
	Executor      interface {
		Preflight(context.Context) error
		Run(context.Context, string, string) (codexexec.Result, error)
	}
	// Oracle is an owner-local deterministic test seam, not a CLI-supplied command.
	Oracle func(context.Context, ResumeRequest) (SyncResult, []byte, error)
}

type repairCheckpoint struct {
	Version         int    `json:"version"`
	Base            string `json:"base"`
	Target          Target `json:"target"`
	CandidateSHA256 string `json:"candidate_sha256"`
	ProposalSHA256  string `json:"proposal_sha256"`
	Round           int    `json:"round"`
	Status          string `json:"status"`
	Stage           string `json:"stage"`
	Diagnostic      string `json:"diagnostic"`
}

func Repair(ctx context.Context, req RepairRequest) (result SyncResult, err error) {
	if req.MaxRounds == 0 {
		req.MaxRounds = 6
	}
	if req.MaxRounds < 1 || req.MaxRounds > 12 || req.Executor == nil || req.CheckpointDir == "" {
		return result, errors.New("repair requires executor, checkpoint directory and 1..12 rounds")
	}
	checkpointAbs, e := filepath.Abs(req.CheckpointDir)
	if e != nil {
		return result, e
	}
	rootAbs, e := filepath.Abs(req.RepoRoot)
	if e != nil {
		return result, e
	}
	if rel, e := filepath.Rel(rootAbs, checkpointAbs); e != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return result, errors.New("checkpoint must be outside the proposal workspace")
	}
	base, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return result, err
	}
	base = strings.TrimSpace(base)
	cp := repairCheckpoint{Version: 1, Base: base, Target: Target{RefName: req.TargetRef, RefKind: req.TargetKind, PeeledCommitSHA: req.TargetSHA}, CandidateSHA256: req.CandidateSHA256, Status: "ready"}
	if err = repairIntegrity(req.ResumeRequest, base); err != nil {
		return result, err
	}
	// Copy immutable input before invoking any proposed code or model.
	candidate, cleanup, err := copyVerifiedCandidate(req.CandidateDir, req.CandidateSHA256, req.TargetRef, req.TargetKind, req.TargetSHA)
	if err != nil {
		return result, err
	}
	defer cleanup()
	req.CandidateDir = candidate
	var proposal []byte
	defer func() {
		if ctx.Err() != nil {
			cp.Status = "cancelled"
			cp.Diagnostic = "execution interrupted; recover this candidate on the same base and target"
		}
		if err != nil && (cp.Status == "ready" || cp.Status == "repairable") {
			cp.Status = "blocked"
			cp.Diagnostic = boundedRepairDiagnostic(err.Error())
		}
		if err != nil {
			result.Outcome = OutcomeFailed
			if result.Stage == "" {
				result.Stage = cp.Stage
			}
			if result.FailureCategory == "" {
				result.FailureCategory = failureCategory(err)
			}
			result.Reason = boundedRepairDiagnostic(err.Error())
		}
		if saveErr := saveRepairCheckpoint(req.CheckpointDir, req.ResumeRequest, cp, proposal); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
	}()
	proposal, err = repairPatch(req.RepoRoot)
	if err != nil {
		return result, err
	}
	if err = req.Executor.Preflight(ctx); err != nil {
		cp.Status = "blocked"
		cp.Stage = "executor_preflight"
		cp.Diagnostic = boundedRepairDiagnostic(err.Error())
		return result, err
	}
	oracle := req.Oracle
	if oracle == nil {
		oracle = repairOracle
	}
	session := ""
	feedback := "Read .agents/skills/codexsdk-sync-upstream/SKILL.md and codexsdk/.cache/codexsdk-sync/agent-input.md. Repair only permitted handwritten source and focused tests for the exact retained candidate. Do not edit generated output, control policy, acceptance, Git state, or credentials. Leave changes unstaged. Independent verification will supply feedback."
	lastDigest := ""
	for round := 1; round <= req.MaxRounds; round++ {
		cp.Round = round
		cp.Stage = "agent"
		if err = repairIntegrity(req.ResumeRequest, base); err != nil {
			return result, err
		}
		execution, runErr := req.Executor.Run(ctx, feedback, session)
		if session != "" && execution.SessionID != "" && execution.SessionID != session {
			return result, errors.New("repair executor changed session identity")
		}
		if execution.SessionID != "" {
			session = execution.SessionID
		}
		// Keep partial in-scope work even after interruption; never store transcripts.
		if scopeErr := repairIntegrity(req.ResumeRequest, base); scopeErr != nil {
			return result, scopeErr
		}
		var normalizeErr error
		if runErr == nil {
			paths, e := ChangedPaths(req.RepoRoot)
			if e != nil {
				return result, e
			}
			var source []string
			for _, p := range paths {
				if strings.HasSuffix(p, ".go") {
					if _, e := os.Stat(filepath.Join(req.RepoRoot, p)); e == nil {
						source = append(source, filepath.Join(req.RepoRoot, p))
					}
				}
			}
			if len(source) > 0 {
				command := exec.CommandContext(ctx, "gofmt", append([]string{"-w"}, source...)...)
				command.Env = repairEnvironment()
				if out, e := command.CombinedOutput(); e != nil {
					normalizeErr = fmt.Errorf("normalize handwritten proposal: %w: %s", e, boundedRepairDiagnostic(string(out)))
				}
			}
		}
		proposal, err = repairPatch(req.RepoRoot)
		if err != nil {
			return result, err
		}
		if runErr != nil {
			cp.Status = execution.Status
			cp.Diagnostic = boundedRepairDiagnostic(execution.Diagnostic)
			return result, runErr
		}
		if execution.Status != "completed" || session == "" {
			return result, errors.New("repair executor did not complete an identified session")
		}
		digest := repairDigest(proposal)
		if digest == lastDigest {
			cp.Status = "no_progress"
			return result, errors.New("repair made no proposal progress after verification feedback")
		}
		lastDigest = digest
		cp.Stage = "oracle"
		var generated []byte
		var verifyErr error
		if normalizeErr != nil {
			result = SyncResult{Stage: "verification", FailureCategory: FailureValidation}
			verifyErr = normalizeErr
		} else {
			result, generated, verifyErr = oracle(ctx, req.ResumeRequest)
		}
		if integrityErr := repairIntegrity(req.ResumeRequest, base); integrityErr != nil {
			return result, integrityErr
		}
		after, patchErr := repairPatch(req.RepoRoot)
		if patchErr != nil {
			return result, patchErr
		}
		if repairDigest(after) != digest {
			return result, errors.New("proposal changed while independent verification ran")
		}
		if verifyErr == nil && result.Outcome == OutcomeApplied {
			if err = applyProposalPatchBytes(req.RepoRoot, generated); err != nil {
				return result, err
			}
			if err = CheckScope(req.RepoRoot, "final"); err != nil {
				return result, err
			}
			cp.Status = "verified"
			cp.Diagnostic = "independent candidate planning and deterministic verification succeeded"
			return result, nil
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		recoverable := (result.Stage == "replan" && result.FailureCategory == FailureUnsupported && result.Issue != nil) || result.Stage == "verification"
		if !recoverable {
			cp.Status = "blocked"
			cp.Stage = result.Stage
			cp.Diagnostic = boundedRepairDiagnostic(fmt.Sprint(verifyErr))
			return result, fmt.Errorf("independent oracle blocked repair at %s: %w", result.Stage, verifyErr)
		}
		cp.Diagnostic = boundedRepairDiagnostic(fmt.Sprint(verifyErr))
		feedback = "Independent verification rejected the current proposal. Continue the same exact candidate and permitted scope. Stage: " + result.Stage + ". Diagnostic:\n" + cp.Diagnostic
		if result.Issue != nil {
			feedback += "\nTyped unresolved: " + result.Issue.Stage + " " + result.Issue.Path + ": " + result.Issue.Reason
		}
		cp.Status = "repairable"
		if err = saveRepairCheckpoint(req.CheckpointDir, req.ResumeRequest, cp, proposal); err != nil {
			return result, err
		}
	}
	cp.Status = "exhausted"
	return result, errors.New("repair round budget exhausted; retained candidate requires explicit recovery")
}

func repairIntegrity(req ResumeRequest, base string) error {
	current, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(current) != base {
		return errors.New("repair base changed")
	}
	if err = CheckScope(req.RepoRoot, "agent"); err != nil {
		return err
	}
	return VerifyCandidate(req.CandidateDir, req.CandidateSHA256, req.TargetRef, req.TargetKind, req.TargetSHA)
}
func repairDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func redactRepairDiagnostic(s string) string {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if len(value) >= 8 && (strings.Contains(name, "KEY") || strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") || name == "CODEX_RESPONSES_API_ENDPOINT") {
			s = strings.ReplaceAll(s, value, "[redacted]")
		}
	}

	return s
}

func boundedRepairDiagnostic(s string) string {
	s = redactRepairDiagnostic(s)
	if len(s) > 8192 {
		s = s[len(s)-8192:]
	}
	return s
}
func repairPatch(root string) ([]byte, error) {
	paths, err := ChangedPaths(root)
	if err != nil {
		return nil, err
	}
	if len(paths) > 0 {
		args := append([]string{"add", "-N", "--"}, paths...)
		if err = runGit(root, args...); err != nil {
			return nil, err
		}
	}
	cmd := exec.Command("git", "-C", root, "diff", "--binary", "--no-ext-diff", "HEAD", "--")
	return cmd.Output()
}
func applyProposalPatchBytes(root string, patch []byte) error {
	if len(patch) == 0 {
		return nil
	}
	cmd := exec.Command("git", "-C", root, "apply", "--whitespace=nowarn", "-")
	cmd.Stdin = strings.NewReader(string(patch))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply verified patch: %w: %s", err, out)
	}
	return nil
}

// repairOracle executes proposed generator code only in a disposable tree.
// Expected target behavior comes from independently retained schema facts;
// baseline assertions remain paired with their baseline fixtures.
func repairOracle(ctx context.Context, req ResumeRequest) (SyncResult, []byte, error) {
	result := SyncResult{Stage: "configuration"}
	dir, err := os.MkdirTemp("", "protocol-repair-oracle-")
	if err != nil {
		return result, nil, err
	}
	defer os.RemoveAll(dir)
	root := filepath.Join(dir, "repo")
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", req.RepoRoot, root)
	if out, e := boundedCommandOutput(cmd); e != nil {
		return result, nil, fmt.Errorf("clone oracle: %w: %s", e, out)
	}
	patch, err := repairPatch(req.RepoRoot)
	if err != nil {
		return result, nil, err
	}
	if err = applyProposalPatchBytes(root, patch); err != nil {
		return result, nil, err
	}
	// Selected common.rs must still be reconstructed from exact upstream Git.
	source := filepath.Join(root, "codexsdk", ".cache", "openai-codex")
	if err = os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		return result, nil, err
	}
	existing := filepath.Join(req.ModuleRoot, ".cache", "openai-codex")
	if _, e := os.Stat(filepath.Join(existing, ".git")); e == nil {
		if err = runGit(root, "clone", "--quiet", "--no-hardlinks", existing, source); err != nil {
			return result, nil, err
		}
	} else {
		if err = runGit(root, "init", "--quiet", source); err != nil {
			return result, nil, err
		}
		command := exec.CommandContext(ctx, "git", "-C", source, "fetch", "--quiet", "--depth=1", defaultRemote, req.TargetSHA)
		if out, e := command.CombinedOutput(); e != nil {
			return result, nil, fmt.Errorf("reconstruct exact source: %w: %s", e, out)
		}
	}
	beforeSource, e := repairNonMechanicalPatch(root)
	if e != nil {
		return result, nil, e
	}
	sandbox, e := newOracleSandbox(ctx, root, req.CandidateDir)
	if e != nil {
		return SyncResult{Stage: "oracle_sandbox", FailureCategory: FailureExecution}, nil, e
	}
	base, e := gitOutput(root, "rev-parse", "HEAD")
	if e != nil {
		return result, nil, e
	}
	verifyIdentity := func() error {
		head, e := gitOutput(root, "rev-parse", "HEAD")
		if e != nil {
			return e
		}
		if head != base {
			return errors.New("oracle Git identity changed")
		}
		return VerifyCandidate(req.CandidateDir, req.CandidateSHA256, req.TargetRef, req.TargetKind, req.TargetSHA)
	}
	binary := filepath.Join(sandbox.work, "protocolupgrade")
	cmd = sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin", "go"), "build", "-o", binary, "./internal/cmd/protocolupgrade")
	buildOutput, buildErr := boundedCommandOutput(cmd)
	if e := verifyIdentity(); e != nil {
		return SyncResult{Stage: "oracle_integrity"}, nil, e
	}
	builtSource, e := repairNonMechanicalPatch(root)
	if e != nil {
		return result, nil, e
	}
	if repairDigest(beforeSource) != repairDigest(builtSource) {
		return SyncResult{Stage: "oracle_integrity"}, nil, errors.New("candidate build changed source")
	}
	if buildErr != nil {
		return SyncResult{Stage: "verification", FailureCategory: FailureValidation}, nil, fmt.Errorf("candidate build: %w\n%s", buildErr, boundedRepairDiagnostic(string(buildOutput)))
	}
	args := []string{"resume", "-json", "-repo-root", root, "-module-root", filepath.Join(root, "codexsdk"), "-candidate-dir", req.CandidateDir, "-candidate-sha256", req.CandidateSHA256, "-target-ref", req.TargetRef, "-target-kind", req.TargetKind, "-target-sha", req.TargetSHA}
	cmd = sandbox.command(ctx, binary, args...)

	cmd.Dir = filepath.Join(root, "codexsdk")
	var stdout, stderr repairOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if e := verifyIdentity(); e != nil {
		return SyncResult{Stage: "oracle_integrity"}, nil, e
	}
	if e := CheckScope(root, "final"); e != nil {
		return SyncResult{Stage: "oracle_integrity"}, nil, e
	}
	afterSource, e := repairNonMechanicalPatch(root)
	if e != nil {
		return result, nil, e
	}
	if repairDigest(beforeSource) != repairDigest(afterSource) {
		return SyncResult{Stage: "oracle_integrity"}, nil, errors.New("proposed generator changed handwritten/control source during construction")
	}
	if e := json.Unmarshal([]byte(stdout.String()), &result); e != nil {
		return SyncResult{Stage: "oracle_launch"}, nil, fmt.Errorf("oracle did not return structured result: %w: %s", e, boundedRepairDiagnostic(stderr.String()))
	}
	if runErr != nil {
		return result, nil, fmt.Errorf("%s", boundedRepairDiagnostic(stderr.String()))
	}
	if _, err = repairPatch(root); err != nil {
		return result, nil, err
	}
	// Capture construction before executable tests. The original proposal tests
	// are retained in the real workspace; only generated bytes return from here.
	generatedPaths, err := ChangedPaths(root)
	if err != nil {
		return result, nil, err
	}
	var mechanical []string
	for _, p := range generatedPaths {
		if isMechanicalPath(p) {
			mechanical = append(mechanical, p)
		}
	}
	var generated []byte
	if len(mechanical) > 0 {
		cmd = exec.Command("git", append([]string{"-C", root, "diff", "--binary", "HEAD", "--"}, mechanical...)...)
		generated, err = cmd.Output()
		if err != nil {
			return result, nil, err
		}
	}
	frozen, err := repairPatch(root)
	if err != nil {
		return result, nil, err
	}
	for _, args := range [][]string{{"test", "./..."}, {"run", "./internal/cmd/protocolupgrade", "check", "-module-root", ".", "-candidate", filepath.Join(req.CandidateDir, "schema")}, {"vet", "./..."}} {
		cmd = sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin", "go"), args...)
		cmd.Dir = filepath.Join(root, "codexsdk")
		out, e := boundedCommandOutput(cmd)
		if identityErr := verifyIdentity(); identityErr != nil {
			return SyncResult{Stage: "oracle_integrity"}, nil, identityErr
		}
		after, pe := repairPatch(root)
		if pe != nil {
			return result, nil, pe
		}
		if repairDigest(after) != repairDigest(frozen) {
			return SyncResult{Stage: "oracle_integrity"}, nil, errors.New("executable verification changed proposal bytes")
		}
		if e != nil {
			return SyncResult{Stage: "verification", FailureCategory: FailureValidation}, nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), e, boundedRepairDiagnostic(string(out)))
		}
	}
	// Target assertions evolve from independently selected schema facts. Stable
	// regression assertions run with their matching baseline, never pasted onto
	// incompatible target types.
	if e := runTargetOracle(ctx, req, root, sandbox); e != nil {
		var integrity *oracleIntegrityError
		if errors.As(e, &integrity) {
			return SyncResult{Stage: "oracle_integrity"}, nil, e
		}
		var contract *oracleContractError
		if errors.As(e, &contract) {
			return SyncResult{Stage: "oracle_contract", FailureCategory: FailureUnsupported}, nil, e
		}
		return SyncResult{Stage: "verification", FailureCategory: FailureValidation}, nil, fmt.Errorf("target schema oracle: %w", e)
	}
	if e := runBaselineOracle(ctx, req, sandbox); e != nil {
		var integrity *oracleIntegrityError
		if errors.As(e, &integrity) {
			return SyncResult{Stage: "oracle_integrity"}, nil, e
		}
		var contract *oracleContractError
		if errors.As(e, &contract) {
			return SyncResult{Stage: "oracle_contract", FailureCategory: FailureUnsupported}, nil, e
		}
		return SyncResult{Stage: "verification", FailureCategory: FailureValidation}, nil, fmt.Errorf("baseline oracle: %w", e)
	}
	if e := verifyIdentity(); e != nil {
		return SyncResult{Stage: "oracle_integrity"}, nil, e
	}
	afterOracle, e := repairNonMechanicalPatch(root)
	if e != nil {
		return result, nil, e
	}
	if repairDigest(beforeSource) != repairDigest(afterOracle) {
		return SyncResult{Stage: "oracle_integrity"}, nil, errors.New("independent verification changed handwritten source")
	}

	return result, generated, nil
}
func repairEnvironment() []string {
	var env []string
	for _, name := range []string{"PATH", "HOME", "GOPATH", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN", "GOROOT", "TMPDIR", "TMP", "TEMP", "CI"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// Construction may emit only mechanical files; it cannot rewrite the source
// that the subsequent independent checks execute.
func repairNonMechanicalPatch(root string) ([]byte, error) {
	if _, err := repairPatch(root); err != nil {
		return nil, err
	}
	paths, err := ChangedPaths(root)
	if err != nil {
		return nil, err
	}
	var source []string
	for _, p := range paths {
		if !isMechanicalPath(p) {
			source = append(source, p)
		}
	}
	if len(source) == 0 {
		return nil, nil
	}
	return exec.Command("git", append([]string{"-C", root, "diff", "--binary", "--no-ext-diff", "HEAD", "--"}, source...)...).Output()
}

// Discard excess command text in memory as well as in retained diagnostics.
type repairOutput struct{ strings.Builder }

func (w *repairOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 65536 - w.Len(); remaining > 0 {
		_, _ = w.Builder.Write(p[:min(remaining, n)])
	}
	return n, nil
}
func boundedCommandOutput(cmd *exec.Cmd) ([]byte, error) {
	var output repairOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return []byte(output.String()), err
}
