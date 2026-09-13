package generatedproof

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProveMatchesCheckedInArtifacts(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")
	result, err := Prove(Request{ModuleRoot: moduleRoot})
	if err != nil {
		t.Fatal(err)
	}
	if !observedTrue(result.GeneratedArtifactsReproducible) {
		t.Fatalf("generated artifacts were not reproducible: %+v", result.Artifacts)
	}
	if result.UpstreamCommit == "" {
		t.Fatal("proof omitted observed upstream commit")
	}
	if result.UpstreamRefKind == "" {
		t.Fatal("proof omitted observed upstream ref kind")
	}
	if result.RepositoryTree == "" || result.WorktreeOverlay == nil {
		t.Fatal("proof omitted observed repository tree identity")
	}
	if !observedFalse(result.BaselinePathLeak) {
		t.Fatal("checked-in baseline reported a path leak")
	}
	payload := proofJSON(t, result)
	if payload["generated_artifacts_reproducible"] != true {
		t.Fatalf("successful proof JSON omitted generated_artifacts_reproducible=true: %s", marshalJSON(t, payload))
	}
	if payload["baseline_path_leak"] != false {
		t.Fatalf("clean path scan must serialize baseline_path_leak=false, got %s", marshalJSON(t, payload))
	}
	if payload["upstream_ref_kind"] != result.UpstreamRefKind {
		t.Fatalf("proof JSON omitted observed ref kind: %s", marshalJSON(t, payload))
	}
	want := []string{methodRegistry, protocolTypes, experimentalMem, sdkSurface}
	if len(result.Artifacts) != len(want) {
		t.Fatalf("artifacts = %#v, want %v", result.Artifacts, want)
	}
	for i, path := range want {
		if result.Artifacts[i].Path != path || !result.Artifacts[i].Reproducible {
			t.Fatalf("artifact %d = %#v, want %s", i, result.Artifacts[i], path)
		}
	}
}

func TestProveObservesGitHEADAndRejectsCallerMismatch(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")
	result, err := Prove(Request{ModuleRoot: moduleRoot})
	if err != nil {
		t.Fatal(err)
	}
	if result.RepositoryCommit == "" {
		t.Fatal("proof omitted observed repository commit")
	}
	if result.UpstreamRef == "" {
		t.Fatal("proof omitted observed upstream ref")
	}
	if result.UpstreamRefKind == "" {
		t.Fatal("proof omitted observed upstream ref kind")
	}
	matched, err := Prove(Request{
		ModuleRoot:           moduleRoot,
		ExpectedUpstreamKind: result.UpstreamRefKind,
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched.UpstreamRefKind != result.UpstreamRefKind {
		t.Fatalf("matching kind dropped observed kind: %+v", matched)
	}
	_, err = Prove(Request{
		ModuleRoot:               moduleRoot,
		ExpectedRepositoryCommit: "0000000000000000000000000000000000000000",
	})
	if err == nil || !strings.Contains(err.Error(), "repository commit=") {
		t.Fatalf("wrong repository commit error = %v", err)
	}
	_, err = Prove(Request{
		ModuleRoot:          moduleRoot,
		ExpectedUpstreamRef: "rust-v0.0.0",
	})
	if err == nil || !strings.Contains(err.Error(), "source_ref_name=") {
		t.Fatalf("wrong upstream ref error = %v", err)
	}
	_, err = Prove(Request{
		ModuleRoot:           moduleRoot,
		ExpectedUpstreamKind: "manual_commit",
	})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind=") {
		t.Fatalf("wrong upstream kind error = %v", err)
	}
}

func TestProveDoesNotWriteOnMismatch(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	surfacePath := filepath.Join(root, "sdk_surface.gen.go")
	if err := os.WriteFile(surfacePath, []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prove(Request{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected mismatch")
	}
	got, err := os.ReadFile(surfacePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package codexsdk\n" {
		t.Fatal("prove rewrote artifacts on mismatch")
	}
}

func TestWriteArtifactsOverwritesDriftWithoutFailing(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	surfacePath := filepath.Join(root, "sdk_surface.gen.go")
	if err := os.WriteFile(surfacePath, []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteArtifacts(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(surfacePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "package codexsdk\n" || !strings.Contains(string(got), "package codexsdk") {
		t.Fatalf("write artifacts did not replace drifted surface")
	}
	result, err := Prove(Request{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !observedTrue(result.GeneratedArtifactsReproducible) {
		t.Fatalf("written artifacts are not reproducible: %+v", result.Artifacts)
	}
}

func TestProveMismatchErrorNamesArtifacts(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	if err := os.WriteFile(filepath.Join(root, "sdk_surface.gen.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prove(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), sdkSurface) || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("mismatch error = %v", err)
	}
}

func TestProveFailsClosedOnWrongUpstreamCommit(t *testing.T) {
	_, err := Prove(Request{
		ModuleRoot:             filepath.Join("..", ".."),
		ExpectedUpstreamCommit: "0000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected wrong upstream commit to fail")
	}
	if !strings.Contains(err.Error(), "source_commit=") || !strings.Contains(err.Error(), "0000000000000000000000000000000000000000") {
		t.Fatalf("wrong-commit error = %v", err)
	}
}

func TestProveNamesMismatchedArtifact(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	surfacePath := filepath.Join(root, "sdk_surface.gen.go")
	if err := os.WriteFile(surfacePath, []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Prove(Request{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected mismatched sdk surface to fail")
	}
	if !observedFalse(result.GeneratedArtifactsReproducible) {
		t.Fatal("mismatch must not be reported as reproducible")
	}
	var found bool
	for _, artifact := range result.Artifacts {
		if artifact.Path == sdkSurface && !artifact.Reproducible && strings.Contains(artifact.Diagnostic, sdkSurface) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing precise sdk surface mismatch: %+v", result.Artifacts)
	}
}

func TestProveRejectsBaselinePathLeak(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	leakPath := filepath.Join(root, "internal", "protocolschema", "appserver", "v2", "baseline_metadata.json")
	raw, err := os.ReadFile(leakPath)
	if err != nil {
		t.Fatal(err)
	}
	leaked := strings.Replace(string(raw), `"codex"`, `"/Users/codex"`, 1)
	if err := os.WriteFile(leakPath, []byte(leaked), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Prove(Request{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected path leak to fail")
	}
	if !observedTrue(result.BaselinePathLeak) {
		t.Fatal("path leak was not recorded")
	}
	if proofJSON(t, result)["baseline_path_leak"] != true {
		t.Fatal("observed path leak must serialize as true")
	}
	if !strings.Contains(err.Error(), "local or cache paths") {
		t.Fatalf("path-leak error = %v", err)
	}
}

func TestProveJSONOmitsUnobservedOnRepositoryMismatch(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")
	result, err := Prove(Request{
		ModuleRoot:               moduleRoot,
		ExpectedRepositoryCommit: "0000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected repository mismatch")
	}
	if result.RepositoryCommit == "" || result.RepositoryTree == "" {
		t.Fatal("repository mismatch must preserve observed repository identity")
	}
	payload := proofJSON(t, result)
	assertJSONAbsent(t, payload, "generated_artifacts_reproducible", "baseline_path_leak", "artifacts")
	if payload["repository_commit"] != result.RepositoryCommit {
		t.Fatalf("repository commit dropped from JSON: %s", marshalJSON(t, payload))
	}
}

func TestProveJSONOmitsUnobservedOnUpstreamMismatch(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")
	result, err := Prove(Request{
		ModuleRoot:             moduleRoot,
		ExpectedUpstreamCommit: "0000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected upstream commit mismatch")
	}
	payload := proofJSON(t, result)
	if payload["upstream_commit"] == nil || payload["upstream_ref"] == nil || payload["upstream_ref_kind"] == nil {
		t.Fatalf("upstream mismatch dropped observed provenance: %s", marshalJSON(t, payload))
	}
	if payload["baseline_commit_matches"] != false {
		t.Fatalf("observed commit mismatch must serialize baseline_commit_matches=false: %s", marshalJSON(t, payload))
	}
	assertJSONAbsent(t, payload, "generated_artifacts_reproducible", "artifacts")
	if result.BaselinePathLeak != nil {
		t.Fatal("upstream commit mismatch happens before path scan")
	}

	result, err = Prove(Request{
		ModuleRoot:          moduleRoot,
		ExpectedUpstreamRef: "rust-v0.0.0",
	})
	if err == nil {
		t.Fatal("expected upstream ref mismatch")
	}
	payload = proofJSON(t, result)
	if payload["upstream_ref_kind"] == nil || payload["upstream_commit"] == nil {
		t.Fatalf("ref mismatch dropped provenance tuple: %s", marshalJSON(t, payload))
	}
	assertJSONAbsent(t, payload, "generated_artifacts_reproducible", "artifacts")
}

func TestProveJSONOmitsPathLeakWhenScanFails(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	dangling := filepath.Join(root, "internal", "protocolschema", "appserver", "v2", "dangling.json")
	if err := os.Symlink("/generatedproof/missing-baseline-path", dangling); err != nil {
		t.Fatal(err)
	}
	result, err := Prove(Request{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected path-scan error")
	}
	if result.BaselinePathLeak != nil {
		t.Fatal("path-scan error must not serialize baseline_path_leak as false")
	}
	payload := proofJSON(t, result)
	assertJSONAbsent(t, payload, "baseline_path_leak", "generated_artifacts_reproducible", "artifacts")
	if payload["upstream_commit"] == nil || payload["upstream_ref_kind"] == nil {
		t.Fatalf("path-scan error dropped already-observed provenance: %s", marshalJSON(t, payload))
	}
}

func TestProveJSONRecordsGeneratedMismatch(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	if err := os.WriteFile(filepath.Join(root, "sdk_surface.gen.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Prove(Request{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected generated mismatch")
	}
	payload := proofJSON(t, result)
	if payload["generated_artifacts_reproducible"] != false {
		t.Fatalf("generated mismatch must serialize explicit false: %s", marshalJSON(t, payload))
	}
	if payload["baseline_path_leak"] != false {
		t.Fatalf("completed clean path scan must remain present on generated mismatch: %s", marshalJSON(t, payload))
	}
	artifacts, ok := payload["artifacts"].([]any)
	if !ok || len(artifacts) == 0 {
		t.Fatalf("generated mismatch dropped artifact observations: %s", marshalJSON(t, payload))
	}
}

func TestProveFailsClosedOnMissingOrEmptySourceRefKind(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	rewriteBaseline(t, root, func(metadata map[string]any) {
		delete(metadata, "source_ref_kind")
	})
	result, err := Prove(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind") {
		t.Fatalf("missing source_ref_kind error = %v", err)
	}
	assertJSONAbsent(t, proofJSON(t, result), "upstream_ref_kind", "generated_artifacts_reproducible")

	rewriteBaseline(t, root, func(metadata map[string]any) {
		metadata["source_ref_kind"] = ""
	})
	_, err = Prove(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind") {
		t.Fatalf("empty source_ref_kind error = %v", err)
	}

	rewriteBaseline(t, root, func(metadata map[string]any) {
		metadata["source_ref_kind"] = "   "
	})
	_, err = Prove(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind") {
		t.Fatalf("placeholder source_ref_kind error = %v", err)
	}
}

func TestProveDoesNotInferRefKindFromRefName(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	rewriteBaseline(t, root, func(metadata map[string]any) {
		metadata["source_ref_name"] = "rust-v0.154.0"
		metadata["source_ref_kind"] = "manual_ref"
	})
	result, err := Prove(Request{
		ModuleRoot:           root,
		ExpectedUpstreamKind: "stable_rust_tag",
	})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind=manual_ref") {
		t.Fatalf("kind mismatch must use observed baseline kind, not ref spelling: %v", err)
	}
	if result.UpstreamRefKind != "manual_ref" {
		t.Fatalf("observed kind = %q", result.UpstreamRefKind)
	}
}

func TestProveObservesExactWorktreeTree(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	if err := os.WriteFile(filepath.Join(root, "tracked-overlay.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := initGitRepo(t, root)
	headTree := gitOutput(t, root, "rev-parse", "HEAD^{tree}")
	indexBefore := gitOutput(t, root, "write-tree")
	statusBefore := gitOutput(t, root, "status", "--porcelain", "--untracked-files=all")

	clean, err := Prove(Request{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if clean.RepositoryCommit != head {
		t.Fatalf("repository commit = %s, want %s", clean.RepositoryCommit, head)
	}
	if clean.RepositoryTree != headTree {
		t.Fatalf("clean tree = %s, want HEAD^{tree} %s", clean.RepositoryTree, headTree)
	}
	if !observedFalse(clean.WorktreeOverlay) {
		t.Fatal("clean proof must record worktree_overlay=false")
	}
	if gitOutput(t, root, "write-tree") != indexBefore || gitOutput(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("tree observation mutated the real index or HEAD")
	}
	if gitOutput(t, root, "status", "--porcelain", "--untracked-files=all") != statusBefore {
		t.Fatal("tree observation mutated the real worktree status")
	}

	if err := os.WriteFile(filepath.Join(root, "tracked-overlay.txt"), []byte("patched-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchedA, err := Prove(Request{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if patchedA.RepositoryCommit != head {
		t.Fatal("overlay proof must keep the base HEAD commit")
	}
	if patchedA.RepositoryTree == headTree {
		t.Fatal("tracked overlay must change the proved tree identity")
	}
	if !observedTrue(patchedA.WorktreeOverlay) {
		t.Fatal("patched proof must record worktree_overlay=true")
	}

	if err := os.WriteFile(filepath.Join(root, "untracked-overlay.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withUntracked, err := Prove(Request{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if withUntracked.RepositoryTree == patchedA.RepositoryTree {
		t.Fatal("untracked overlay file must contribute to the proved tree")
	}
	if !observedTrue(withUntracked.WorktreeOverlay) {
		t.Fatal("untracked overlay must still be recorded as an overlay")
	}

	if err := os.WriteFile(filepath.Join(root, "tracked-overlay.txt"), []byte("patched-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchedB, err := Prove(Request{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if patchedB.RepositoryTree == withUntracked.RepositoryTree {
		t.Fatal("different patches on the same HEAD must not share a tree identity")
	}

	if err := os.WriteFile(filepath.Join(root, "tracked-overlay.txt"), []byte("patched-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repeat, err := Prove(Request{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if repeat.RepositoryTree != withUntracked.RepositoryTree {
		t.Fatal("same base plus same overlay must reproduce the same tree identity")
	}
	payload := proofJSON(t, repeat)
	if payload["repository_commit"] == nil || payload["repository_tree"] == nil || payload["worktree_overlay"] != true {
		t.Fatalf("applied proof JSON must not identify the proved state by HEAD alone: %s", marshalJSON(t, payload))
	}
}

func observedTrue(value *bool) bool {
	return value != nil && *value
}

func observedFalse(value *bool) bool {
	return value != nil && !*value
}

func proofJSON(t *testing.T, result Result) map[string]any {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertJSONAbsent(t *testing.T, payload map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := payload[key]; ok {
			t.Fatalf("unobserved field %q present: %s", key, marshalJSON(t, payload))
		}
	}
}

func marshalJSON(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func rewriteBaseline(t *testing.T, root string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(baselineRel), "baseline_metadata.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	mutate(metadata)
	updated, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initGitRepo(t *testing.T, root string) string {
	t.Helper()
	gitOutput(t, root, "init", "-b", "main")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "fixture")
	return gitOutput(t, root, "rev-parse", "HEAD")
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-C", dir,
		"-c", "user.name=generatedproof-test",
		"-c", "user.email=generatedproof-test@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func copyTree(t *testing.T, srcRoot, destRoot string, rels []string) {
	t.Helper()
	for _, rel := range rels {
		src := filepath.Join(srcRoot, filepath.FromSlash(rel))
		dest := filepath.Join(destRoot, filepath.FromSlash(rel))
		info, err := os.Stat(src)
		if err != nil {
			t.Fatal(err)
		}
		if info.IsDir() {
			if err := filepath.Walk(src, func(path string, walkInfo os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				relPath, err := filepath.Rel(src, path)
				if err != nil {
					return err
				}
				target := filepath.Join(dest, relPath)
				if walkInfo.IsDir() {
					return os.MkdirAll(target, 0o755)
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				return os.WriteFile(target, raw, walkInfo.Mode())
			}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
