package protocolsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssertCleanAndStagePaths(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	if err := AssertClean(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "codexsdk", "client.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AssertClean(repo); err == nil {
		t.Fatal("dirty worktree must fail assert-clean")
	}
	if _, err := StagePaths(repo, "mechanical"); err == nil || !strings.Contains(err.Error(), "mechanical") {
		t.Fatalf("handwritten file must not stage as mechanical: %v", err)
	}
	if _, err := StagePaths(repo, "final"); err != nil {
		t.Fatal(err)
	}
	cached, err := gitOutput(repo, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cached, "codexsdk/client.go") {
		t.Fatalf("staged paths = %q", cached)
	}
}

func TestStagePathsRejectsEmptySet(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	if _, err := StagePaths(repo, "final"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidatePathsRejectsCacheAndAgents(t *testing.T) {
	err := validatePaths([]string{"codexsdk/.cache/openai-codex/README"}, "final")
	if err == nil || !strings.Contains(err.Error(), "final") {
		t.Fatalf("err = %v", err)
	}
	err = validatePaths([]string{"README.md"}, "final")
	if err == nil {
		t.Fatal("non-codexsdk path must fail final scope")
	}
}
