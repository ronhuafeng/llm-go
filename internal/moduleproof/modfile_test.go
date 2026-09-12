package moduleproof

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestWriteVerifyModfileAppliesReplacesWithoutTouchingSource(t *testing.T) {
	dir := t.TempDir()
	srcMod := filepath.Join(dir, "go.mod")
	srcSum := filepath.Join(dir, "go.sum")
	original := "module example.com/app\n\ngo 1.23.0\n\nrequire example.com/lib v1.2.3\n"
	if err := os.WriteFile(srcMod, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcSum, []byte("example.com/lib v1.2.3 h1:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	destMod := filepath.Join(destDir, "verify.mod")
	destSum := filepath.Join(destDir, "verify.sum")
	if err := WriteVerifyModfile(srcMod, srcSum, []Replace{{
		OldPath: "example.com/lib",
		NewPath: "../lib",
	}}, destMod, destSum); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(srcMod)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("source go.mod changed:\n%s", got)
	}

	written, err := os.ReadFile(destMod)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := modfile.Parse(destMod, written, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Replace) != 1 || parsed.Replace[0].Old.Path != "example.com/lib" || parsed.Replace[0].New.Path != "../lib" {
		t.Fatalf("temporary replace = %#v", parsed.Replace)
	}
	if strings.Contains(string(got), "replace ") {
		t.Fatal("committed manifest gained a replace")
	}
}

func TestWriteVerifyModfileFailsClosedWithoutDestinations(t *testing.T) {
	err := WriteVerifyModfile("go.mod", "go.sum", nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "temporary module files are required") {
		t.Fatalf("missing destination error = %v", err)
	}
}
