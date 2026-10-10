package codexexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	executor, err := New(ctx, Config{Workspace: workspace, StateDir: t.TempDir(), Version: os.Getenv("LLMGO_LIVE_CODEX_VERSION"), BaseURL: os.Getenv("CODEX_RESPONSES_API_ENDPOINT"), APIKey: os.Getenv("AZURE_OPENAI_API_KEY"), Model: os.Getenv("LLMGO_LIVE_CODEX_MODEL"), Reasoning: os.Getenv("LLMGO_LIVE_CODEX_REASONING")})
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	if err = executor.Preflight(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := executor.Run(ctx, "Write exactly first (no newline) to feedback.txt using a shell command. Do not change any other file.", "")
	if err != nil || first.SessionID == "" {
		t.Fatalf("initial execution: %v %s", err, first.Diagnostic)
	}
	if data, e := os.ReadFile(filepath.Join(workspace, "feedback.txt")); e != nil || string(data) != "first" {
		t.Fatal("independent initial file assertion failed")
	}
	second, err := executor.Run(ctx, "Independent validation says feedback.txt must contain exactly verified (no newline). Fix that file using a shell command and finish.", first.SessionID)
	if err != nil || second.SessionID != first.SessionID {
		t.Fatalf("feedback resume: %v %s", err, second.Diagnostic)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "feedback.txt"))
	if err != nil || string(data) != "verified" {
		t.Fatal("independent feedback file assertion failed")
	}
}
