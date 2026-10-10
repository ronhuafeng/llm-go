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
		if err != nil && cp.Status == "ready" {
			cp.Status = "blocked"
			cp.Diagnostic = boundedRepairDiagnostic(err.Error())
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
		result, generated, verifyErr := oracle(ctx, req.ResumeRequest)
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
func boundedRepairDiagnostic(s string) string {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if len(value) >= 8 && (strings.Contains(name, "KEY") || strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") || name == "CODEX_RESPONSES_API_ENDPOINT") {
			s = strings.ReplaceAll(s, value, "[redacted]")
		}
	}

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
// The original accepted tests are restored there: edits to tests may add local
// evidence but cannot remove the independent acceptance assertions.
func repairOracle(ctx context.Context, req ResumeRequest) (SyncResult, []byte, error) {
	result := SyncResult{Stage: "configuration"}
	dir, err := os.MkdirTemp("", "protocol-repair-oracle-")
	if err != nil {
		return result, nil, err
	}
	defer os.RemoveAll(dir)
	root := filepath.Join(dir, "repo")
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", req.RepoRoot, root)
	if out, e := cmd.CombinedOutput(); e != nil {
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
		if err = runGit(root, "clone", "--quiet", "--shared", existing, source); err != nil {
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
	args := []string{"run", "./internal/cmd/protocolupgrade", "resume", "-json", "-repo-root", root, "-module-root", filepath.Join(root, "codexsdk"), "-candidate-dir", req.CandidateDir, "-candidate-sha256", req.CandidateSHA256, "-target-ref", req.TargetRef, "-target-kind", req.TargetKind, "-target-sha", req.TargetSHA}
	cmd = exec.CommandContext(ctx, "go", args...)
	cmd.Dir = filepath.Join(root, "codexsdk")
	cmd.Env = repairEnvironment()
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
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
		cmd = exec.CommandContext(ctx, "go", args...)
		cmd.Dir = filepath.Join(root, "codexsdk")
		cmd.Env = repairEnvironment()
		out, e := cmd.CombinedOutput()
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
	// A second pass restores accepted tests, independently of the model's test
	// edits. The proposal remains unchanged outside this disposable tree.
	tracked, e := gitNUL(root, "ls-files", "-z", "--", "*_test.go")
	if e != nil {
		return result, nil, e
	}
	if len(tracked) > 0 {
		if e = runGit(root, append([]string{"restore", "--source=HEAD", "--worktree", "--"}, tracked...)...); e != nil {
			return result, nil, e
		}
	}
	trustedBefore, e := repairPatch(root)
	if e != nil {
		return result, nil, e
	}
	cmd = exec.CommandContext(ctx, "go", "test", "./...")
	cmd.Dir = filepath.Join(root, "codexsdk")
	cmd.Env = repairEnvironment()
	out, e := cmd.CombinedOutput()
	trustedAfter, pe := repairPatch(root)
	if pe != nil {
		return result, nil, pe
	}
	if repairDigest(trustedBefore) != repairDigest(trustedAfter) {
		return SyncResult{Stage: "oracle_integrity"}, nil, errors.New("trusted test execution changed proposal bytes")
	}
	if e != nil {
		return SyncResult{Stage: "verification", FailureCategory: FailureValidation}, nil, fmt.Errorf("accepted test assertions: %w\n%s", e, boundedRepairDiagnostic(string(out)))
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
