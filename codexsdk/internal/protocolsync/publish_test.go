package protocolsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncBranchName(t *testing.T) {
	got := syncBranchName("codex/sync-upstream/run-1-1", "refs/tags/rust-v0.154.0", oldSHA)
	if got != "codex/sync-upstream/run-1-1-rust-v0.154.0-111111111111" {
		t.Fatalf("got %s", got)
	}
}

func TestParseSyncMetadata(t *testing.T) {
	body := `<!-- codexsdk-upstream-sync
upstream_ref: rust-v0.154.0
upstream_ref_kind: stable_rust_tag
upstream_commit: ` + oldSHA + `
sync_commit: ` + newSHA + `
base_branch: main
-->
`
	meta := parseSyncMetadata(body)
	if meta["upstream_ref"] != "rust-v0.154.0" || meta["upstream_commit"] != oldSHA || meta["sync_commit"] != newSHA || meta["base_branch"] != "main" {
		t.Fatalf("%+v", meta)
	}
}

func TestNormalizeBranchRef(t *testing.T) {
	if got := normalizeBranchRef("refs/heads/main", "origin"); got != "main" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeBranchRef("origin/main", "origin"); got != "main" {
		t.Fatalf("got %s", got)
	}
}

func TestPublishFailsClosedWhenLandingMoved(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	writeFile(t, filepath.Join(repo, "codexsdk", "sdk_surface.gen.go"), "package codexsdk\n")
	runGitInitCommit(t, repo, "sync change")
	remote := filepath.Join(t.TempDir(), "remote.git")
	clone := exec.Command("git", "clone", "--bare", repo, remote)
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone remote: %v\n%s", err, out)
	}
	if out, err := execGit(t, repo, "remote", "add", "origin", remote); err != nil {
		t.Fatalf("add remote: %v\n%s", err, out)
	}
	_, err := Publish(PublishRequest{
		RepoRoot:   repo,
		BaseBranch: "main",
		TargetRef:  "rust-v0.140.0",
		TargetKind: KindStableTag,
		TargetSHA:  oldSHA,
		Remote:     "origin",
	})
	if err == nil || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("err = %v", err)
	}
}

func TestPublishFailsClosedOnDirtyWorktree(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	if err := os.WriteFile(filepath.Join(repo, "codexsdk", "dirty.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Publish(PublishRequest{
		RepoRoot:   repo,
		BaseBranch: "main",
		TargetRef:  "rust-v0.140.0",
		TargetKind: KindStableTag,
		TargetSHA:  oldSHA,
	})
	if err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("err = %v", err)
	}
}

func gitMust(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := gitOutput(repo, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
