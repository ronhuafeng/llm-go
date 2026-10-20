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

func TestForwardBaselinesFreezeBothAcceptedInputs(t *testing.T) {
	h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
	states := []string{goalState(t, h), goalState(t, b), goalState(t, m)}
	writeFile(t, filepath.Join(b, "accepted.txt"), "original")
	writeFile(t, filepath.Join(m, "accepted.txt"), "current")
	// Snapshotting includes the complete supplied content, while leaving its index
	// and history alone. Caller production checkouts are already clean and detached.
	states[1], states[2] = goalState(t, b), goalState(t, m)
	calls := 0
	var frozen string
	err := verifyForwardBaselines(context.Background(), h, b, m, t.TempDir(), func(ctx context.Context, req oracleRequest) error {
		calls++
		if req.RepoRoot == h || req.BaseRoot == b || req.BaseRoot == m {
			t.Fatal("executed caller checkout")
		}
		if frozen != "" && frozen != req.RepoRoot {
			t.Fatal("baseline lanes used different candidates")
		}
		frozen = req.RepoRoot
		got, err := os.ReadFile(filepath.Join(req.BaseRoot, "accepted.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != []string{"original", "current"}[calls-1] {
			t.Fatal("baseline binding changed", string(got))
		}
		if req.ModuleRoot != filepath.Join(req.BaseRoot, "codexsdk") {
			t.Fatal("wrong module")
		}
		entries, err := os.ReadDir(req.CandidateDir)
		if err != nil || len(entries) != 0 {
			t.Fatal("invented upstream inputs", err)
		}
		return nil
	})
	if err != nil || calls != 2 {
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

func TestForwardBaselinesFailClosedAndRetainSafeDiagnostics(t *testing.T) {
	for _, mode := range []string{"original failure", "current failure", "frozen mutation", "input mutation", "diagnostic overflow"} {
		t.Run(mode, func(t *testing.T) {
			h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
			dir := t.TempDir()
			calls := 0
			const secret = "forward-test-secret-827301"
			t.Setenv("FORWARD_TEST_SECRET", secret)
			err := verifyForwardBaselines(context.Background(), h, b, m, dir, func(ctx context.Context, req oracleRequest) error {
				calls++
				req.Output.Write([]byte(secret[:10]))
				req.Output.Write([]byte(secret[10:]))
				switch mode {
				case "original failure":
					return errors.New("rejected original")
				case "current failure":
					if calls == 2 {
						return errors.New("rejected current")
					}
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
			if mode == "original failure" && calls != 1 {
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

func TestForwardBaselinesRejectUnsafeInputs(t *testing.T) {
	h, b, m := goalFixture(t), goalFixture(t), goalFixture(t)
	for _, paths := range [][4]string{{"relative", b, m, t.TempDir()}, {h, h, m, t.TempDir()}, {h, b, m, filepath.Join(h, "diagnostics")}, {h, b, m, "relative"}} {
		if err := VerifyForwardBaselines(context.Background(), paths[0], paths[1], paths[2], paths[3]); err == nil {
			t.Fatal("unsafe inputs accepted", paths)
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(h, link); err != nil {
		t.Fatal(err)
	}
	if err := VerifyForwardBaselines(context.Background(), link, b, m, t.TempDir()); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestForwardBaselinesUnavailableSandboxFails(t *testing.T) {
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
	if err := VerifyForwardBaselines(context.Background(), h, b, m, dir); err == nil || !strings.Contains(err.Error(), "boundary was not proven") {
		t.Fatalf("unavailable isolation accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyForwardBaselines(ctx, h, b, m, t.TempDir()); err == nil {
		t.Fatal("cancelled proof accepted")
	}
}

// The required hosted sandbox suite calls this helper as a subtest. It executes
// the real baseline oracle; no model, target schema, or fake launcher is involved.
func testForwardBaselinesSandbox(t *testing.T) {
	b := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                                      "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n",
		"codexsdk/runtime.go":                         "package codexsdk\nfunc Stable()bool{return true}\nfunc Current()bool{return true}\n",
		"codexsdk/runtime_test.go":                    "package codexsdk\nimport \"testing\"\nfunc TestOriginal(t *testing.T){if !Stable(){t.Fatal(\"original baseline broken\")}}\n",
		"codexsdk/protocolv2/runtime.go":              "package protocolv2\n",
		"codexsdk/internal/wirejson/runtime.go":       "package wirejson\n",
		"codexsdk/internal/cmd/protocolv2gen/main.go": "package main\nfunc main(){}\n",
		mechanicalPrefix + "/fixture.json":            "{}",
	} {
		writeFile(t, filepath.Join(b, name), body)
	}
	for _, family := range []string{"llmkit/llmschema", "llmcaller/codex"} {
		writeFile(t, filepath.Join(b, family, "runtime.go"), "package fixture\nfunc Accepted()bool{return true}\n")
		writeFile(t, filepath.Join(b, family, "runtime_test.go"), "package fixture\nimport \"testing\"\nfunc TestAccepted(t *testing.T){if !Accepted(){t.Fatal(\""+family+" baseline broken\")}}\n")
	}
	runGitInit(t, b)
	clone := func(source string) string {
		t.Helper()
		root := filepath.Join(t.TempDir(), "repo")
		if _, err := goalSnapshot(context.Background(), source, root); err != nil {
			t.Fatal(err)
		}
		return root
	}
	m := clone(b)
	os.Remove(filepath.Join(m, "codexsdk/runtime_test.go"))
	writeFile(t, filepath.Join(m, "codexsdk/current_test.go"), "package codexsdk\nimport \"testing\"\nfunc TestCurrent(t *testing.T){if !Current(){t.Fatal(\"current baseline broken\")}}\n")
	m = clone(m)
	h := clone(m)
	os.Remove(filepath.Join(h, "codexsdk/current_test.go"))
	writeFile(t, filepath.Join(h, "codexsdk/bypass_test.go"), "package codexsdk\nimport (\"os\";\"testing\")\nfunc TestMain(m *testing.M){os.Exit(0)}\n")
	for _, family := range []string{"llmkit/llmschema", "llmcaller/codex"} {
		os.Remove(filepath.Join(h, family, "runtime_test.go"))
		writeFile(t, filepath.Join(h, family, "bypass_test.go"), "package fixture\nimport(\"os\";\"testing\")\nfunc TestMain(m *testing.M){os.Exit(0)}\n")
	}
	verify := func() error { return VerifyForwardBaselines(context.Background(), h, b, m, t.TempDir()) }
	if err := verify(); err != nil {
		t.Fatalf("legitimate targetless reconciliation: %v", err)
	}
	for _, tc := range []struct{ code, cause string }{
		{"func Stable()bool{return false};func Current()bool{return true}", "original baseline broken"},
		{"func Stable()bool{return true};func Current()bool{return false}", "current baseline broken"},
	} {
		writeFile(t, filepath.Join(h, "codexsdk/runtime.go"), "package codexsdk\n"+tc.code+"\n")
		if err := verify(); err == nil || !strings.Contains(err.Error(), tc.cause) {
			t.Fatalf("deleted test / candidate TestMain bypass: %v", err)
		}
	}

	writeFile(t, filepath.Join(h, "codexsdk/runtime.go"), "package codexsdk\nfunc Stable()bool{return true};func Current()bool{return true}\n")
	for _, family := range []string{"llmkit/llmschema", "llmcaller/codex"} {
		writeFile(t, filepath.Join(h, family, "runtime.go"), "package fixture\nfunc Accepted()bool{return false}\n")
		if err := verify(); err == nil || !strings.Contains(err.Error(), family+" baseline broken") {
			t.Fatalf("broader runtime regression escaped: %v", err)
		}
		writeFile(t, filepath.Join(h, family, "runtime.go"), "package fixture\nfunc Accepted()bool{return true}\n")
	}
}

func TestForwardBaselineBroaderRuntimeOverlayAndSelection(t *testing.T) {
	b, h := t.TempDir(), t.TempDir()
	for _, root := range []string{b, h} {
		writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
		for _, family := range []string{"llmkit/llmschema", "llmcaller/codex"} {
			writeFile(t, filepath.Join(root, family, "runtime.go"), "package fixture\n")
			writeFile(t, filepath.Join(root, family, "runtime_test.go"), "package fixture\nimport \"testing\"\nfunc TestAccepted(t *testing.T){}\n")
			writeFile(t, filepath.Join(root, family, "testdata/accepted.txt"), root)
		}
		runGitInit(t, root)
	}
	writeFile(t, filepath.Join(h, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n// candidate dependencies\n")
	writeFile(t, filepath.Join(h, "llmkit/llmschema/runtime.go"), "package fixture\n// proposed implementation\n")
	os.Remove(filepath.Join(h, "llmcaller/codex/runtime.go"))
	os.Remove(filepath.Join(h, "llmkit/llmschema/runtime_test.go"))
	packages, err := forwardRuntimePackages(b)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := selectedExecutableTests(filepath.Join(b, "codexsdk"), packages)
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"llmkit/llmschema", "llmcaller/codex"} {
		if !expected["github.com/ronhuafeng/llm-go/"+family+"/TestAccepted"] {
			t.Fatal("wrong execution identity", expected)
		}
	}
	if err := overlayForwardRuntime(b, h); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "llmkit/llmschema/runtime.go"} {
		a, _ := os.ReadFile(filepath.Join(h, name))
		b, _ := os.ReadFile(filepath.Join(b, name))
		if string(a) != string(b) {
			t.Fatal("candidate production omitted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(b, "llmcaller/codex/runtime.go")); !os.IsNotExist(err) {
		t.Fatal("candidate deletion omitted")
	}
	if _, err := os.Stat(filepath.Join(b, "llmkit/llmschema/runtime_test.go")); err != nil {
		t.Fatal("accepted test deleted")
	}
	data, _ := os.ReadFile(filepath.Join(b, "llmkit/llmschema/testdata/accepted.txt"))
	if string(data) != b {
		t.Fatal("accepted fixture replaced")
	}
	writeFile(t, filepath.Join(h, "llmkit/llmschema/embedded.json"), "{}")
	if err := runGit(h, "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := overlayForwardRuntime(b, h); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatal("non-Go drift silently ignored", err)
	}
}
