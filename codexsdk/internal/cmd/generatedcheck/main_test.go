package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckExitsZeroOnCleanCheckout(t *testing.T) {
	code := run([]string{"-module-root", filepath.Join("..", "..", "..")})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestCheckExitsOneOnWrongUpstreamCommit(t *testing.T) {
	code := run([]string{
		"-module-root", filepath.Join("..", "..", ".."),
		"-expected-upstream-commit", "0000000000000000000000000000000000000000",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestWriteArtifactsRejectsExpectedUpstreamFlags(t *testing.T) {
	code := run([]string{
		"-write-artifacts",
		"-expected-upstream-commit", "0000000000000000000000000000000000000000",
	})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestCheckExitsOneOnGeneratedMismatch(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join("..", "..", "..")
	for _, rel := range []string{
		"internal/protocolschema/appserver/v2",
		"protocolv2",
		"sdk_surface.gen.go",
	} {
		copyPath(t, filepath.Join(src, filepath.FromSlash(rel)), filepath.Join(root, filepath.FromSlash(rel)))
	}
	if err := os.WriteFile(filepath.Join(root, "sdk_surface.gen.go"), []byte("package codexsdk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := run([]string{"-module-root", root})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func copyPath(t *testing.T, src, dest string) {
	t.Helper()
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
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
		return
	}
	if err := filepath.Walk(src, func(path string, walkInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
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
}
