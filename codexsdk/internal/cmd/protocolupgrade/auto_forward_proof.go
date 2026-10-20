package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

type forwardVerifier struct {
	f      forwardFlags
	op     *protocolsync.NativeForward
	source string
	stderr io.Writer
}

func newForwardVerifier(ctx context.Context, f forwardFlags, op *protocolsync.NativeForward, stderr io.Writer) (*forwardVerifier, error) {
	v := &forwardVerifier{f: f, op: op, stderr: stderr}
	if f.sha == "" {
		return v, nil
	}
	// This source was rebuilt by a read-only workflow step before the controller
	// received any provider/App key. Never run upstream build scripts here.
	raw, e := os.ReadFile(os.Getenv("AUTO_FORWARD_SOURCE"))
	if e != nil || len(raw) > 4<<20 {
		return nil, errors.New("trusted prewarmed upstream inputs unavailable")
	}
	var prepared protocolsync.SyncResult
	if json.Unmarshal(raw, &prepared) != nil || prepared.Target.RefName != f.ref || prepared.Target.RefKind != f.kind || prepared.Target.PeeledCommitSHA != f.sha || !filepath.IsAbs(prepared.CandidateDir) {
		return nil, errors.New("prewarmed upstream identity differs from fixed target")
	}
	v.source = prepared.CandidateDir
	return v, nil
}
func (v *forwardVerifier) local(ctx context.Context, work, candidate, main string) (protocolsync.GoalVerifyRequest, error) {
	bases := filepath.Join(work, "accepted")
	if e := os.MkdirAll(bases, 0700); e != nil {
		return protocolsync.GoalVerifyRequest{}, e
	}
	base, e := detachedForwardRoot(ctx, v.f.root, bases, main)
	if e != nil {
		return protocolsync.GoalVerifyRequest{}, e
	}
	original := base
	if main != v.f.original {
		original, e = detachedForwardRoot(ctx, v.f.root, bases, v.f.original)
		if e != nil {
			return protocolsync.GoalVerifyRequest{}, e
		}
	}
	diagnosticRoot := os.Getenv("AUTO_FORWARD_DIAGNOSTICS")
	if diagnosticRoot == "" {
		diagnosticRoot = os.TempDir()
	}
	if e = os.MkdirAll(diagnosticRoot, 0700); e != nil {
		return protocolsync.GoalVerifyRequest{}, e
	}
	diagnostics, e := os.MkdirTemp(diagnosticRoot, "forward-diagnostics-")
	if e != nil {
		return protocolsync.GoalVerifyRequest{}, e
	}
	return protocolsync.GoalVerifyRequest{RepoRoot: candidate, DiagnosticDir: diagnostics, TrustedCacheRoot: filepath.Join(v.f.root, "codexsdk/.cache"), BaseRoot: base, OriginalBaseRoot: original, CandidateDir: v.source}, nil
}
func (v *forwardVerifier) verify(ctx context.Context, main, head string) error {

	work, e := os.MkdirTemp("", "auto-forward-proof-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(work)
	candidate, e := detachedForwardRoot(ctx, v.f.root, work, head)
	if e != nil {
		return e
	}
	local, e := v.local(ctx, work, candidate, main)
	if e != nil {
		return e
	}
	if v.f.sha == "" {
		return protocolsync.VerifyForwardBaselines(ctx, candidate, local.OriginalBaseRoot, local.BaseRoot, local.DiagnosticDir)
	}
	result, e := protocolsync.VerifyGoal(ctx, local)
	defer result.Close()
	if e != nil {
		fmt.Fprintf(v.stderr, "Current-main/original-base proof failed; diagnostics: %s\n", result.DiagnosticPath)
		if semanticForwardFailure(ctx, result.DiagnosticPath, e) {
			return protocolsync.ErrForwardSemanticProof
		}
		return errors.New("immutable original/current baseline proof failed")
	}
	return nil
}
func (v *forwardVerifier) repair(ctx context.Context, candidate, main, head string) (protocolsync.GoalVerifyResult, error) {
	var empty protocolsync.GoalVerifyResult
	if v.f.sha == "" {
		return empty, errors.New("semantic repair needs fixed target")
	}
	work, e := os.MkdirTemp("", "auto-forward-fresh-repair-")
	if e != nil {
		return empty, e
	}
	defer os.RemoveAll(work)
	local, e := v.local(ctx, work, candidate, main)
	if e != nil {
		return empty, e
	}
	current := func(call context.Context) error {
		state, e := v.op.Read(call, head)
		if e != nil {
			return e
		}
		if state.Main != main || state.Head != head || !state.Ready {
			return errors.New("repair binding moved")
		}
		decision, e := v.op.Policy(call, main, head)
		if e != nil {
			return e
		}
		if decision != "run" {
			return errors.New("repair target superseded or blocked")
		}
		return nil
	}
	if e = current(ctx); e != nil {
		return empty, e
	}
	dirs, e := codexexec.NativeGoalRuntimePaths("codex")
	if e != nil {
		return empty, errors.New("native repair runtime unavailable")
	}
	if rust := os.Getenv("AUTO_FORWARD_RUST_TOOLS"); rust != "" {
		dirs = append(dirs, rust)
	}
	moduleCache, e := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").Output()
	if e != nil {
		return empty, e
	}
	executor, e := codexexec.New(ctx, codexexec.Config{Workspace: candidate, StateDir: filepath.Join(work, "state"), Version: os.Getenv("CODEX_CLI_VERSION"), BaseURL: os.Getenv("CODEX_RESPONSES_API_ENDPOINT"), APIKey: os.Getenv("AZURE_OPENAI_API_KEY"), Model: "gpt-6.1-sol", Reasoning: "high", GoalSandbox: &codexexec.GoalSandboxConfig{RuntimeDirs: dirs, ModuleCache: strings.TrimSpace(string(moduleCache)), ReadOnlyDirs: []string{local.DiagnosticDir}}})
	if e != nil {
		return empty, errors.New("existing isolated repair runtime/provider unavailable")
	}
	objective := fmt.Sprintf(`NEW integration repair invocation, never resume the completed upstream goal.
Repair this complete candidate conflict or completed local contract assertion failure for PR #%d, branch %s, old H %s, exact main M %s, original accepted B %s, fixed upstream U %s (%s %s). The original deadline is %s and cannot be renewed.
All old-H changes must survive unless the fixed public contract requires a justified adaptation. Do not use automatic ours/theirs, drop assertions, weaken verifier/control sources, retarget U or request repository authority. Read both sides and resolve meaning. If unresolvable, report blocked.
Use verify_upstream (no arguments) to run the full local proof, including immutable original-B and current-M contracts. Read the complete diagnostic JSONL. Finish this fresh goal only after an unchanged snapshot passes local proof. You cannot publish or integrate. The trusted host later publishes by exact-old-H lease, waits for all four fresh exact-head CI results, and only then performs non-force main FF.`, v.f.pr, v.op.Branch(), head, main, v.f.original, v.f.sha, v.f.kind, v.f.ref, v.f.deadline)
	repaired, e := protocolsync.RunIntegrationRepair(ctx, protocolsync.IntegrationRepairRequest{Objective: objective, Local: local, Current: current, Progress: &protocolsync.GoalProgress{Output: v.stderr, Actions: os.Getenv("GITHUB_ACTIONS") == "true"}}, executor)
	closeErr := executor.Close()
	if e != nil || closeErr != nil {
		repaired.Snapshot.Close()
		return empty, errors.New("fresh isolated repair failed without publication")
	}
	if e = current(ctx); e != nil {
		repaired.Snapshot.Close()
		return empty, e
	}
	return repaired.Snapshot, nil
}

// Only a complete failed local proof with an actual Go assertion failure is
// eligible for semantic repair. Setup, sandbox, timeout, incomplete diagnostic,
// compilation and CI failures remain explicit blockers. Output is feedback,
// never authority to accept a proposal; the repaired snapshot must fully pass.
func semanticForwardFailure(ctx context.Context, path string, proofErr error) bool {
	if ctx.Err() != nil {
		return false
	}
	var incomplete *protocolsync.GoalVerificationIncompleteError
	var exit *exec.ExitError
	if errors.As(proofErr, &incomplete) || (!errors.As(proofErr, &exit) && !strings.Contains(proofErr.Error(), "baseline runtime assertions:")) {
		return false
	}
	f, e := os.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 16<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	failed, complete := false, false
	for scanner.Scan() {
		var row struct {
			Kind     string `json:"kind"`
			Output   string `json:"output"`
			Complete bool   `json:"complete"`
			Passed   bool   `json:"passed"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			return false
		}
		if row.Kind == "output" && strings.Contains(row.Output, "--- FAIL: Test") {
			failed = true
		}
		if row.Kind == "footer" {
			complete = row.Complete && !row.Passed
		}
	}
	return scanner.Err() == nil && complete && failed
}
