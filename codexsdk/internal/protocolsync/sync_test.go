package protocolsync

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
)

func TestDecideAfterPolicy(t *testing.T) {
	if got := decideAfterPolicy(DecisionSkip, false); got != "current" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterPolicy(DecisionSkip, true); got != "generate" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterPolicy(DecisionAllow, false); got != "generate" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterPolicy(DecisionBlock, false); got != "blocked" {
		t.Fatalf("got %s", got)
	}
}

func TestDecideAfterDrift(t *testing.T) {
	if got := decideAfterDrift(true, "clean"); got != "comparison" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterDrift(true, "review-required"); got != "comparison_dirty" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterDrift(false, "clean"); got != "comparison" {
		t.Fatalf("got %s", got)
	}
	if got := decideAfterDrift(false, "review-required"); got != "apply" {
		t.Fatalf("got %s", got)
	}
}

func TestSyncCurrentSkipsGenerate(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	generated := false
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.140.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.140.0":    oldSHA + "\trefs/tags/rust-v0.140.0",
			"refs/tags/rust-v0.140.0^{}": oldSHA + "\trefs/tags/rust-v0.140.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			generated = true
			return Candidate{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeCurrent {
		t.Fatalf("outcome = %s", result.Outcome)
	}
	if generated {
		t.Fatal("current baseline must not generate")
	}
}

func TestSyncForceCompareCurrentStillGenerates(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.140.0",
		ForceCompare: true,
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.140.0":    oldSHA + "\trefs/tags/rust-v0.140.0",
			"refs/tags/rust-v0.140.0^{}": oldSHA + "\trefs/tags/rust-v0.140.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{SchemaDir: "/tmp/schema", SourceCommit: oldSHA, DriftStatus: "clean"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeCurrent || result.Candidate != "/tmp/schema" {
		t.Fatalf("%+v", result)
	}
}

func TestSyncBlockedDowngradeFailsClosed(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.141.0", KindStableTag)
	_, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.140.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.140.0":    newSHA + "\trefs/tags/rust-v0.140.0",
			"refs/tags/rust-v0.140.0^{}": newSHA + "\trefs/tags/rust-v0.140.0^{}",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "older than the current baseline") {
		t.Fatalf("err = %v", err)
	}
}

func TestSyncAppliesRealDrift(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	applied := false
	result, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.141.0",
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{SchemaDir: "/tmp/schema", SourceCommit: newSHA, DriftStatus: "review-required"}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applied = true
			path := filepath.Join(repo, "codexsdk", "sdk_surface.gen.go")
			if err := os.WriteFile(path, []byte("package codexsdk\n"), 0o644); err != nil {
				return protocolupgrade.ApplyResult{}, err
			}
			return protocolupgrade.ApplyResult{Status: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied || result.Outcome != OutcomeApplied {
		t.Fatalf("applied=%v result=%+v", applied, result)
	}
}

func TestSyncForceCompareDirtyFailsWithoutApply(t *testing.T) {
	repo := initSyncRepo(t, oldSHA, "rust-v0.140.0", KindStableTag)
	applied := false
	_, err := Sync(SyncRequest{
		RepoRoot:     repo,
		ModuleRoot:   filepath.Join(repo, "codexsdk"),
		UpstreamRepo: "fake",
		UpstreamRef:  "rust-v0.141.0",
		ForceCompare: true,
		Lookuper: fakeLookuper{byPattern: map[string]string{
			"refs/tags/rust-v0.141.0":    newSHA + "\trefs/tags/rust-v0.141.0",
			"refs/tags/rust-v0.141.0^{}": newSHA + "\trefs/tags/rust-v0.141.0^{}",
		}},
		Generate: func(GenerateRequest) (Candidate, error) {
			return Candidate{SchemaDir: "/tmp/schema", SourceCommit: newSHA, DriftStatus: "review-required"}, nil
		},
		Apply: func(protocolupgrade.ApplyRequest) (protocolupgrade.ApplyResult, error) {
			applied = true
			return protocolupgrade.ApplyResult{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "comparison never applies") {
		t.Fatalf("err = %v", err)
	}
	if applied {
		t.Fatal("force-compare dirty must not apply")
	}
}

func TestIsMechanicalPath(t *testing.T) {
	if !isMechanicalPath("codexsdk/internal/protocolschema/appserver/v2/manifest.json") {
		t.Fatal("baseline path should be mechanical")
	}
	if !isMechanicalPath("codexsdk/sdk_surface.gen.go") {
		t.Fatal("sdk surface should be mechanical")
	}
	if !isMechanicalPath("codexsdk/protocolv2/protocol_types.gen.go") {
		t.Fatal("generated protocol types should be mechanical")
	}
	if isMechanicalPath("codexsdk/client.go") {
		t.Fatal("handwritten source is not mechanical")
	}
}

func TestWriteGitHubOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out")
	err := WriteGitHubOutput(path, SyncResult{
		Outcome:   OutcomeApplied,
		Reason:    "applied",
		Candidate: "/exact/schema",
		Target:    Target{RefName: "rust-v0.154.0", RefKind: KindStableTag, PeeledCommitSHA: oldSHA},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(mustRead(t, path))
	for _, want := range []string{"outcome=applied", "applied=true", "candidate=/exact/schema", "target_ref=rust-v0.154.0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}

func initSyncRepo(t *testing.T, commit, ref, kind string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "codexsdk", "internal", "protocolschema", "appserver", "v2", "baseline_metadata.json"), mustJSON(t, map[string]string{
		"source_commit":   commit,
		"source_ref_name": ref,
		"source_ref_kind": kind,
	}))
	writeFile(t, filepath.Join(root, "README.md"), "# fixture\n")
	runGitInit(t, root)
	return root
}

func runGitInit(t *testing.T, root string) {
	t.Helper()
	cmds := [][]string{
		{"init", "-b", "main"},
		{"add", "-A"},
		{"commit", "-m", "fixture"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=protocolsync-test", "-c", "user.email=protocolsync-test@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(append(raw, '\n'))
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
