package protocolupgrade

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAcceptsCheckedInGeneratedSource(t *testing.T) {
	result, err := Check(CheckRequest{ModuleRoot: moduleRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Clean {
		t.Fatalf("checked-in generated source was not clean: %+v", result.Artifacts)
	}
}

func TestCheckReportsStaleGeneratedSource(t *testing.T) {
	root := copyModuleForCheck(t)
	surface := filepath.Join(root, "sdk_surface.gen.go")
	raw, err := os.ReadFile(surface)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(surface, append(raw, []byte("\n// stale\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Check(CheckRequest{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected stale generated source to fail")
	}
	if result.Clean {
		t.Fatal("stale generated source reported clean")
	}
	if !strings.Contains(err.Error(), "sdk_surface.gen.go") {
		t.Fatalf("error missing generated path: %v", err)
	}
}

func TestCheckReportsCandidateBaselineMismatch(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	candidate := t.TempDir()
	if err := copyTree(baseline, candidate); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidate, "AddedDrift.json"), []byte(`{"type":"string"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Check(CheckRequest{ModuleRoot: root, Candidate: candidate})
	if err == nil {
		t.Fatal("expected candidate mismatch to fail")
	}
	if result.Clean {
		t.Fatal("mismatched candidate reported clean")
	}
	if !strings.Contains(err.Error(), "AddedDrift.json") {
		t.Fatalf("error missing drifted path: %v", err)
	}
}

func TestCheckFailsUnsupportedGeneratorShape(t *testing.T) {
	root := copyModuleForCheck(t)
	baseline := filepath.Join(root, filepath.FromSlash(defaultBaselineRel))
	if err := os.WriteFile(filepath.Join(baseline, "RequestId.json"), []byte(`{"title":"RequestId","type":"boolean"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Check(CheckRequest{ModuleRoot: root})
	if err == nil {
		t.Fatal("expected unsupported schema shape to fail")
	}
	if !strings.Contains(err.Error(), "RequestId.json") && !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error missing unsupported shape diagnostic: %v", err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyModuleForCheck(t *testing.T) string {
	t.Helper()
	src := moduleRoot(t)
	dst := t.TempDir()
	baselineRel := filepath.FromSlash(defaultBaselineRel)
	if err := copyTree(filepath.Join(src, baselineRel), filepath.Join(dst, baselineRel)); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"protocolv2/method_registry.gen.go",
		"protocolv2/protocol_types.gen.go",
		"protocolv2/experimental_members.gen.go",
		"sdk_surface.gen.go",
	} {
		from := filepath.Join(src, filepath.FromSlash(rel))
		to := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := copyFile(from, to); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
