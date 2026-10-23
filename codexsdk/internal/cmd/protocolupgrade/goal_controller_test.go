package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
)

func TestGoalControllerPreservesOriginalBWhileExecutingCurrentM(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		b, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-b", "main")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@localhost")
	metadata := filepath.Join(root, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json")
	if err := os.MkdirAll(filepath.Dir(metadata), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, []byte(`{"source_commit":"`+strings.Repeat("a", 40)+`","source_ref_name":"rust-v0.163.0","source_ref_kind":"stable_rust_tag"}`), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "original accepted base")
	original := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("new current-main obligation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "new controller")
	current := git("rev-parse", "HEAD")
	binding := protocolsync.GoalPublicationRequest{RepoRoot: root, BaseSHA: original, Target: protocolsync.Target{RefName: "rust-v0.164.0", RefKind: protocolsync.KindStableTag, PeeledCommitSHA: strings.Repeat("b", 40)}}
	originalRoot, cleanup, err := prepareGoalControllerBases(context.Background(), &binding)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if originalRoot == root || binding.BaseSHA != original || binding.VerificationBaseSHA != current {
		t.Fatal("B replaced or old controller required", binding, originalRoot)
	}
	b, err := exec.Command("git", "-C", originalRoot, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(b)) != original {
		t.Fatal("original contract not pinned", err)
	}
	if _, err := os.Stat(filepath.Join(originalRoot, "README.md")); !os.IsNotExist(err) {
		t.Fatal("current-main files leaked into original contract")
	}
	if git("rev-parse", "HEAD") != current {
		t.Fatal("trusted controller checkout moved")
	}
	// A dirty controller may not become accepted authority, even with the right SHA.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("not accepted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, release, err := prepareGoalControllerBases(context.Background(), &binding)
	release()
	if err == nil {
		t.Fatal("dirty controller accepted")
	}
}
