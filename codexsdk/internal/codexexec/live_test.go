package codexexec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This is workflow-executor evidence, independent of the SDK App Server stories.
// It runs once through the same native provider and exact installed release.
func TestLiveCodexRepairPreflightAndFeedback(t *testing.T) {
	if os.Getenv("LLMGO_LIVE_CODEX") != "1" {
		t.Skip("explicit live suite only")
	}
	workspace := t.TempDir()
	if out, err := exec.Command("git", "-C", workspace, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	executor, err := New(ctx, Config{Workspace: workspace, StateDir: t.TempDir(), Version: os.Getenv("CODEX_CLI_VERSION"), BaseURL: os.Getenv("CODEX_RESPONSES_API_ENDPOINT"), APIKey: os.Getenv("AZURE_OPENAI_API_KEY"), Model: os.Getenv("LLMGO_LIVE_CODEX_MODEL"), Reasoning: os.Getenv("LLMGO_LIVE_CODEX_REASONING")})
	if err != nil {
		t.Log("live_executor.stage=configure outcome=failed diagnostic=configuration_failed")
		t.Fatal(err)
	}
	defer executor.Close()
	t.Log("live_executor.stage=preflight outcome=started diagnostic=none")
	if err = executor.Preflight(ctx); err != nil {
		t.Log("live_executor.stage=preflight outcome=failed diagnostic=" + strings.TrimPrefix(err.Error(), "preflight: "))
		t.Fatal(err)
	}
	t.Log("live_executor.stage=preflight outcome=completed diagnostic=completed")
	first, err := executor.Run(ctx, "Write exactly first (no newline) to feedback.txt using a shell command. Do not change any other file.", "")
	if err != nil || first.SessionID == "" {
		t.Log("live_executor.stage=start outcome=failed diagnostic=" + first.Diagnostic)
		t.Fatalf("initial execution: %v %s", err, first.Diagnostic)
	}
	if data, e := os.ReadFile(filepath.Join(workspace, "feedback.txt")); e != nil || string(data) != "first" {
		t.Log("live_executor.stage=start outcome=failed diagnostic=file_assertion")
		t.Fatal("independent initial file assertion failed")
	}
	second, err := executor.Run(ctx, "Independent validation says feedback.txt must contain exactly verified (no newline). Fix that file using a shell command and finish.", first.SessionID)
	if err != nil || second.SessionID != first.SessionID {
		t.Log("live_executor.stage=resume outcome=failed diagnostic=" + second.Diagnostic)
		t.Fatalf("feedback resume: %v %s", err, second.Diagnostic)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "feedback.txt"))
	if err != nil || string(data) != "verified" {
		t.Log("live_executor.stage=resume outcome=failed diagnostic=file_assertion")
		t.Fatal("independent feedback file assertion failed")
	}
	if err := proveNativeSandboxMode(ctx, executor, false); err != nil {
		t.Log("live_executor.stage=readonly_probe outcome=failed diagnostic=positive_control_failed")
		t.Fatal(err)
	}
	// The repair controller owns feedback outside the writable proposal. Use
	// ordinary owner-writable permissions so denial proves the native sandbox,
	// rather than a read-only Unix mode bit or a missing path.
	feedbackPath := filepath.Join(t.TempDir(), "feedback.jsonl")
	feedback := []byte("{\"stage\":\"verification\",\"status\":\"failed\"}\n")
	if err := os.WriteFile(feedbackPath, feedback, 0600); err != nil {
		t.Fatal("cannot prepare controller feedback fixture")
	}
	if err := proveNativeFeedbackBoundary(ctx, executor, feedbackPath, feedback); err != nil {
		t.Log("live_executor.stage=readonly_probe outcome=failed diagnostic=write_denial_unverified")
		t.Fatal(err)
	}
	t.Log("live_executor.stage=readonly_probe outcome=completed diagnostic=write_denied_read_verified")
	// A fresh verifier identity must inspect the same candidate without write access.
	t.Log("live_executor.stage=readonly_review outcome=started diagnostic=none")
	reviewer, err := New(ctx, Config{Workspace: workspace, StateDir: t.TempDir(), ReadOnly: true, Version: os.Getenv("CODEX_CLI_VERSION"), BaseURL: os.Getenv("CODEX_RESPONSES_API_ENDPOINT"), APIKey: os.Getenv("AZURE_OPENAI_API_KEY"), Model: os.Getenv("LLMGO_LIVE_CODEX_MODEL"), Reasoning: os.Getenv("LLMGO_LIVE_CODEX_REASONING")})
	if err != nil {
		t.Log("live_executor.stage=readonly_review outcome=failed diagnostic=configuration_failed")
		t.Fatal("reviewer configuration failed")
	}
	defer reviewer.Close()
	if err := proveNativeSandboxMode(ctx, reviewer, true); err != nil {
		t.Log("live_executor.stage=readonly_probe outcome=failed diagnostic=write_denial_unverified")
		t.Fatal(err)
	}
	t.Log("live_executor.stage=readonly_probe outcome=completed diagnostic=write_denied_read_verified")
	reviewed, err := reviewer.Run(ctx, "Read feedback.txt using a shell command without changing any file. Reply with its exact contents only, without quotes or formatting.", "")
	if err != nil {
		t.Log("live_executor.stage=readonly_review outcome=failed diagnostic=" + reviewed.Diagnostic)
		t.Fatal("reviewer execution failed")
	}
	if reviewed.SessionID == "" || reviewed.SessionID == first.SessionID {
		t.Log("live_executor.stage=readonly_review outcome=failed diagnostic=session_mismatch")
		t.Fatal("reviewer did not use fresh session")
	}
	if b, e := os.ReadFile(filepath.Join(workspace, "feedback.txt")); e != nil || string(b) != "verified" {
		t.Log("live_executor.stage=readonly_review outcome=failed diagnostic=file_assertion")
		t.Fatal("read-only reviewer changed candidate")
	}
	if b, e := os.ReadFile(reviewed.FinalMessagePath); e != nil || strings.TrimSpace(string(b)) != "verified" {
		t.Log("live_executor.stage=readonly_review outcome=failed diagnostic=read_assertion")
		t.Fatal("read-only reviewer did not inspect candidate")
	}
	t.Log("live_executor.stage=readonly_review outcome=completed diagnostic=write_denied_read_verified")

}

// The official CLI sandbox helper resolves the same home, cwd and explicit
// permission overrides as Executor.Run. The writable control must succeed first;
// arbitrary process failure cannot count as a denied write.
func proveNativeSandboxMode(ctx context.Context, e *Executor, readonly bool) error {
	mode := "workspace-write"
	script := "printf 'probe-start\\n'; printf verified > feedback.txt || exit 23; printf 'write-allowed\\n'"
	want := "probe-start\nwrite-allowed\n"
	if readonly {
		mode = "read-only"
		script = "printf 'probe-start\\n'; if printf changed > feedback.txt; then exit 21; fi; test \"$(cat feedback.txt)\" = verified || exit 22; printf 'write-denied-read-verified\\n'"
		want = "probe-start\nwrite-denied-read-verified\n"
	}
	if e.config.ReadOnly != readonly {
		return fmt.Errorf("probe mode differs from executor")
	}
	cmd := nativeSandboxProbe(ctx, e, mode, "sh", "-c", script)
	out, err := cmd.Output()
	if err != nil || string(out) != want {
		return fmt.Errorf("native sandbox did not establish expected read/write boundary")
	}
	return nil
}

// This fixed command uses the same Executor config and native sandbox as Run.
// Successful workspace writes and an exact read precede both denial checks.
func proveNativeFeedbackBoundary(ctx context.Context, e *Executor, path string, expected []byte) error {
	if e.config.ReadOnly {
		return fmt.Errorf("feedback probe requires repair workspace-write mode")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return fmt.Errorf("controller feedback fixture is not a regular file")
	}
	script := `command -v cmp >/dev/null && command -v mv >/dev/null || exit 20
cmp - "$1" || exit 21
printf workspace-write-verified > feedback-probe-positive.txt || exit 22
cat "$1" > feedback-probe-original.jsonl || exit 23
printf replacement > feedback-probe-replacement.jsonl || exit 24
if printf changed > "$1"; then exit 25; fi
cmp feedback-probe-original.jsonl "$1" || exit 26
if mv -f -- feedback-probe-replacement.jsonl "$1"; then exit 27; fi
test -f feedback-probe-replacement.jsonl || exit 28
cmp feedback-probe-original.jsonl "$1" || exit 29
printf 'feedback-read-verified\nworkspace-write-verified\nfeedback-write-and-replacement-denied\n'`
	cmd := nativeSandboxProbe(ctx, e, "workspace-write", "sh", "-c", script, "feedback-boundary", path)
	cmd.Stdin = bytes.NewReader(expected)
	out, runErr := cmd.Output()
	after, statErr := os.Lstat(path)
	original, readErr := os.ReadFile(path)
	if statErr != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || readErr != nil || !bytes.Equal(original, expected) {
		return fmt.Errorf("controller feedback changed during native sandbox probe")
	}
	positive, positiveErr := os.ReadFile(filepath.Join(e.config.Workspace, "feedback-probe-positive.txt"))
	if runErr != nil || string(out) != "feedback-read-verified\nworkspace-write-verified\nfeedback-write-and-replacement-denied\n" || positiveErr != nil || string(positive) != "workspace-write-verified" {
		return fmt.Errorf("native sandbox did not establish controller feedback read/write boundary")
	}
	return nil
}

func nativeSandboxProbe(ctx context.Context, e *Executor, mode string, command ...string) *exec.Cmd {
	args := []string{"sandbox", "-c", "sandbox_mode=" + strconv.Quote(mode)}
	args = append(args, e.sandboxOverrides()...)
	args = append(args, "--")
	args = append(args, command...)
	cmd := e.command(ctx, args...)
	// This fixed fixture makes no model request and needs no provider credential.
	env := cmd.Env[:0]
	for _, entry := range cmd.Env {
		if !strings.HasPrefix(entry, "AZURE_OPENAI_API_KEY=") {
			env = append(env, entry)
		}
	}
	cmd.Env = env
	return cmd
}
