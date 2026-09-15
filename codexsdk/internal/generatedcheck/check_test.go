package generatedcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckMatchesCheckedInArtifacts(t *testing.T) {
	if err := Check(Request{ModuleRoot: filepath.Join("..", "..")}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDoesNotWriteOnMismatch(t *testing.T) {
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
	if err := Check(Request{ModuleRoot: root}); err == nil {
		t.Fatal("expected mismatch")
	}
	got, err := os.ReadFile(surfacePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package codexsdk\n" {
		t.Fatal("check rewrote artifacts on mismatch")
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
	if err := Check(Request{ModuleRoot: root}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckMismatchErrorNamesArtifacts(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	if err := os.WriteFile(filepath.Join(root, "sdk_surface.gen.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), sdkSurface) || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("mismatch error = %v", err)
	}
}

func TestCheckFailsClosedOnWrongUpstreamCommit(t *testing.T) {
	err := Check(Request{
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

func TestCheckRejectsBaselinePathLeak(t *testing.T) {
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
	err = Check(Request{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected path leak to fail")
	}
	if !strings.Contains(err.Error(), "local or cache paths") {
		t.Fatalf("path-leak error = %v", err)
	}
}

func TestCheckFailsClosedOnMissingOrEmptySourceRefKind(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	rewriteBaseline(t, root, func(metadata map[string]any) {
		delete(metadata, "source_ref_kind")
	})
	err := Check(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind") {
		t.Fatalf("missing source_ref_kind error = %v", err)
	}

	rewriteBaseline(t, root, func(metadata map[string]any) {
		metadata["source_ref_kind"] = ""
	})
	err = Check(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind") {
		t.Fatalf("empty source_ref_kind error = %v", err)
	}

	rewriteBaseline(t, root, func(metadata map[string]any) {
		metadata["source_ref_kind"] = "   "
	})
	err = Check(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind") {
		t.Fatalf("placeholder source_ref_kind error = %v", err)
	}
}

func TestCheckDoesNotInferRefKindFromRefName(t *testing.T) {
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
	err := Check(Request{
		ModuleRoot:           root,
		ExpectedUpstreamKind: "stable_rust_tag",
	})
	if err == nil || !strings.Contains(err.Error(), "source_ref_kind=manual_ref") {
		t.Fatalf("kind mismatch must use observed baseline kind, not ref spelling: %v", err)
	}
}

func TestCheckFailsClosedOnInvalidSourceCommit(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	rewriteBaseline(t, root, func(metadata map[string]any) {
		metadata["source_commit"] = "not-a-sha"
	})
	err := Check(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_commit") {
		t.Fatalf("invalid source_commit error = %v", err)
	}
}

func TestCheckFailsClosedOnEmptySourceRefName(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("..", ".."), root, []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	})
	rewriteBaseline(t, root, func(metadata map[string]any) {
		metadata["source_ref_name"] = ""
	})
	err := Check(Request{ModuleRoot: root})
	if err == nil || !strings.Contains(err.Error(), "source_ref_name") {
		t.Fatalf("empty source_ref_name error = %v", err)
	}
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
