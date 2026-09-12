package generatedproof

import (
	"os"
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
	if !result.GeneratedArtifactsReproducible {
		t.Fatalf("generated artifacts were not reproducible: %+v", result.Artifacts)
	}
	if result.UpstreamCommit == "" {
		t.Fatal("proof omitted observed upstream commit")
	}
	if result.BaselinePathLeak {
		t.Fatal("checked-in baseline reported a path leak")
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
	if result.GeneratedArtifactsReproducible {
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
	if !result.BaselinePathLeak {
		t.Fatal("path leak was not recorded")
	}
	if !strings.Contains(err.Error(), "local or cache paths") {
		t.Fatalf("path-leak error = %v", err)
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
