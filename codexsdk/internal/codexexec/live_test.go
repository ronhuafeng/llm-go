package codexexec

import (
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
	args := []string{"sandbox", "-c", "sandbox_mode=" + strconv.Quote(mode)}
	args = append(args, e.sandboxOverrides()...)
	args = append(args, "--", "sh", "-c", script)
	cmd := e.command(ctx, args...)
	// This fixed fixture has no model request and needs no provider credential.
	env := cmd.Env[:0]
	for _, entry := range cmd.Env {
		if !strings.HasPrefix(entry, "AZURE_OPENAI_API_KEY=") {
			env = append(env, entry)
		}
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil || string(out) != want {
		return fmt.Errorf("native sandbox did not establish expected read/write boundary")
	}
	return nil
}
