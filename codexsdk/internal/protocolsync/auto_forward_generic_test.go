package protocolsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestForwardCandidateFreezeBothAcceptedInputs(t *testing.T) {
	h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
	states := []string{goalState(t, h), goalState(t, b), goalState(t, m)}
	writeFile(t, filepath.Join(b, "accepted.txt"), "original")
	writeFile(t, filepath.Join(m, "accepted.txt"), "current")
	// Snapshotting includes the complete supplied content, while leaving its index
	// and history alone. Caller production checkouts are already clean and detached.
	states[1], states[2] = goalState(t, b), goalState(t, m)
	calls := 0
	var frozen string
	err := verifyForwardCandidate(context.Background(), h, b, m, t.TempDir(), func(ctx context.Context, req forwardCandidateRequest) error {
		calls++
		if req.RepoRoot == h {
			t.Fatal("executed caller checkout")
		}
		frozen = req.RepoRoot
		if _, err := os.Stat(filepath.Join(req.RepoRoot, "accepted.txt")); !os.IsNotExist(err) {
			t.Fatal("baseline content overlaid into candidate")
		}
		entries, err := os.ReadDir(req.CandidateDir)
		if err != nil || len(entries) != 0 {
			t.Fatal("invented upstream inputs", err)
		}
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	for i, root := range []string{h, b, m} {
		if goalState(t, root) != states[i] {
			t.Fatal("input mutated")
		}
	}
	if _, err := os.Stat(frozen); !os.IsNotExist(err) {
		t.Fatal("snapshot not cleaned")
	}
}

func TestForwardCandidateFailClosedAndRetainSafeDiagnostics(t *testing.T) {
	for _, mode := range []string{"native failure", "base mutation", "frozen mutation", "input mutation", "diagnostic overflow"} {
		t.Run(mode, func(t *testing.T) {
			h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
			dir := t.TempDir()
			calls := 0
			const secret = "forward-test-secret-827301"
			t.Setenv("FORWARD_TEST_SECRET", secret)
			err := verifyForwardCandidate(context.Background(), h, b, m, dir, func(ctx context.Context, req forwardCandidateRequest) error {
				calls++
				req.Output.Write([]byte(secret[:10]))
				req.Output.Write([]byte(secret[10:]))
				switch mode {
				case "native failure":
					return errors.New("native tests failed")
				case "base mutation":
					writeFile(t, filepath.Join(b, "tracked.go"), "changed")
				case "frozen mutation":
					writeFile(t, filepath.Join(req.RepoRoot, "tracked.go"), "changed")
				case "input mutation":
					writeFile(t, filepath.Join(h, "tracked.go"), "changed")
				case "diagnostic overflow":
					req.Output.Write([]byte(strings.Repeat("x", goalDiagnosticLimit+1)))
				}
				return nil
			})
			if err == nil {
				t.Fatal("false acceptance")
			}
			if mode == "native failure" && calls != 1 {
				t.Fatal("continued failed baseline")
			}
			files, _ := filepath.Glob(filepath.Join(dir, "verification-*.jsonl"))
			if len(files) != 1 {
				t.Fatal(files)
			}
			data, e := os.ReadFile(files[0])
			if e != nil || len(data) == 0 || strings.Contains(string(data), secret) {
				t.Fatal("missing or unsafe diagnostics", e)
			}
			if mode != "diagnostic overflow" && !strings.Contains(string(data), `"passed":false`) {
				t.Fatal("missing failure verdict")
			}
		})
	}
}

func TestForwardCandidateRejectUnsafeInputs(t *testing.T) {
	h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
	for _, paths := range [][4]string{{"relative", b, m, t.TempDir()}, {h, h, m, t.TempDir()}, {h, b, m, filepath.Join(h, "diagnostics")}, {h, b, m, "relative"}} {
		if err := VerifyForwardCandidate(context.Background(), paths[0], paths[1], paths[2], paths[3]); err == nil {
			t.Fatal("unsafe inputs accepted", paths)
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(h, link); err != nil {
		t.Fatal(err)
	}
	if err := VerifyForwardCandidate(context.Background(), link, b, m, t.TempDir()); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestForwardCandidateUnavailableSandboxFails(t *testing.T) {
	h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
	dir := t.TempDir()
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "bwrap"), "#!/bin/sh\nexit 93\n")
	if err := os.Chmod(filepath.Join(bin, "bwrap"), 0700); err != nil {
		t.Fatal(err)
	}
	// Keep Git/Go available while explicitly making the isolation launcher fail.
	// This is failure injection, never a replacement success path.
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := VerifyForwardCandidate(context.Background(), h, b, m, dir); err == nil || !strings.Contains(err.Error(), "boundary was not proven") {
		t.Fatalf("unavailable isolation accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyForwardCandidate(ctx, h, b, m, t.TempDir()); err == nil {
		t.Fatal("cancelled proof accepted")
	}
}
