package protocolsync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func isVerifierSourceFeedback(err error) bool {
	var feedback *verifierFeedbackError
	return errors.As(err, &feedback)
}
func sourceExitOne(t *testing.T) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", "exit 1").Run()
	if err == nil {
		t.Fatal("exit fixture succeeded")
	}
	return err
}

// Trusted literal modules exercise the actual Go tidy implementation offline.
// The local dependency makes this test independent of network/cache availability.
func TestVerifierSourceTidyIndirectBecomesDirect(t *testing.T) {
	_ = verifierSourceTidyFeedbackFixture(t)
}

func verifierSourceTidyFeedbackFixture(t *testing.T) error {
	t.Helper()
	root := t.TempDir()
	original := "module fixture\n\ngo 1.26.8\n\nrequire example.test/wrapper v0.0.0\n\nrequire example.test/dependency v0.0.0 // indirect\n\nreplace example.test/wrapper => ./wrapper\n\nreplace example.test/dependency => ./dependency\n"
	writeFile(t, filepath.Join(root, "go.mod"), original)
	base := `package fixture;import "example.test/wrapper";var _ = wrapper.Value`
	writeFile(t, filepath.Join(root, "fixture.go"), base)
	writeFile(t, filepath.Join(root, "dependency/go.mod"), "module example.test/dependency\n\ngo 1.26.8\n")
	writeFile(t, filepath.Join(root, "dependency/dependency.go"), "package dependency;const Value=true\n")
	writeFile(t, filepath.Join(root, "wrapper/go.mod"), "module example.test/wrapper\n\ngo 1.26.8\n\nrequire example.test/dependency v0.0.0\n")
	writeFile(t, filepath.Join(root, "wrapper/wrapper.go"), `package wrapper;import "example.test/dependency";const Value=dependency.Value`)
	tidy := func() (string, error) {
		cmd := exec.Command("go", "mod", "tidy", "-diff")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
		var stdout, stderr repairOutput
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		got, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil || string(got) != original {
			t.Fatal("tidy -diff changed source")
		}
		return stdout.String(), verifierSourceToolOutcome("tidy", stdout.String(), stderr.String(), runErr)
	}
	if out, err := tidy(); err != nil {
		t.Fatalf("accepted literal baseline not tidy-clean: %v %s", err, out)
	}
	// This is the entire proposed source change: import an already-indirect module.
	writeFile(t, filepath.Join(root, "fixture.go"), `package fixture;import("example.test/wrapper";"example.test/dependency");var _,_=wrapper.Value,dependency.Value`)
	out, feedbackErr := tidy()
	if !strings.Contains(out, " // indirect") || !isVerifierSourceFeedback(feedbackErr) {
		t.Fatalf("actual indirect-to-direct change not feedback: %v %s", feedbackErr, out)
	}
	// Repair within source scope alone, without editing go.mod or go.sum.
	writeFile(t, filepath.Join(root, "fixture.go"), base)
	if out, err := tidy(); err != nil {
		t.Fatalf("source-only repair did not restore tidy: %v %s", err, out)
	}
	return feedbackErr
}
func TestVerifierSourceTidyUnknownIsNotFeedback(t *testing.T) {
	diff := "diff current/go.mod tidy/go.mod\n--- current/go.mod\n+++ tidy/go.mod\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	for _, tt := range []struct{ name, stdout, stderr string }{
		{"cache", "", "go: module lookup disabled by GOPROXY=off"},
		{"unknown", "unexpected tidy failure", ""},
		{"mixed", diff, "unexpected failure"},
		{"other file", strings.ReplaceAll(diff, "go.mod", "policy.go"), ""},
		{"append noise", diff + "network failed\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := verifierSourceToolOutcome("tidy", tt.stdout, tt.stderr, sourceExitOne(t))
			if err == nil || isVerifierSourceFeedback(err) {
				t.Fatalf("unknown tidy granted repair: %v", err)
			}
		})
	}
	if err := verifierSourceToolOutcome("tidy", "unexplained", "", nil); err == nil {
		t.Fatal("unexplained success output accepted")
	}
}

func TestVerifierSourcePermanentExecutionEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, body     string
		feedback, pass bool
	}{
		{"pass", `func TestPermanent(t *testing.T){}`, false, true},
		{"assertion", `func TestPermanent(t *testing.T){t.Fatal("permanent invariant violated")}`, true, false},
		{"init exits", `func init(){os.Exit(0)};func TestPermanent(t *testing.T){}`, false, false},
		{"skip", `func TestPermanent(t *testing.T){t.Skip("erased")}`, false, false},
		{"child skip", `func TestPermanent(t *testing.T){t.Run("missing",func(t *testing.T){t.Skip("erased")})}`, false, false},
		{"cache", `func TestPermanent(t *testing.T){t.Fatal("module lookup disabled by GOPROXY=off")}`, false, false},
		{"compile", `func TestPermanent(t *testing.T){missingIdentifier()}`, true, false},
		{"child assertion", `func TestPermanent(t *testing.T){t.Run("contract",func(t *testing.T){t.Fatal("invariant violated")})}`, true, false},
		{"silent failure", `func TestPermanent(t *testing.T){t.Fail()}`, true, false},
		{"panic", `func TestPermanent(t *testing.T){panic("invariant violated")}`, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "go.mod"), "module github.com/ronhuafeng/llm-go\ngo 1.26.8\n")
			dir := filepath.Join(root, "codexsdk/internal/schemaoracle")
			writeFile(t, filepath.Join(dir, "oracle.go"), "package schemaoracle\n")
			writeFile(t, filepath.Join(dir, "oracle_test.go"), "package schemaoracle\nimport(\"testing\";\"os\");var _=os.Args\n"+tt.body)
			expected, err := selectedExecutableTests(filepath.Join(root, "codexsdk"), []string{"internal/schemaoracle"})
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "-race", "-count=1", "-json", "./codexsdk/internal/schemaoracle")
			cmd.Dir = root
			events := newVerifierSourceEvents(expected)
			var stderr repairOutput
			cmd.Stdout = events
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			_, _ = events.baseline.diagnostic.Write([]byte(stderr.String()))
			err = verifierSourceTestOutcome(events, runErr, stderr.String())
			var assertion *verifierAssertionError
			if got := errors.As(err, &assertion); got != (tt.name == "assertion" || tt.name == "child assertion") {
				t.Fatalf("assertion evidence=%t for %s: %v", got, tt.name, err)
			}
			if (err == nil) != tt.pass || isVerifierSourceFeedback(err) != tt.feedback {
				t.Fatalf("pass=%t feedback=%t want %t/%t: %v", err == nil, isVerifierSourceFeedback(err), tt.pass, tt.feedback, err)
			}
		})
	}
}
func TestVerifierSourceSelectsCurrentTestFuzzExample(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal/schemaoracle")
	writeFile(t, filepath.Join(dir, "oracle.go"), "package schemaoracle\n")
	writeFile(t, filepath.Join(dir, "oracle_test.go"), `package schemaoracle
import("testing";"fmt")
func TestCurrent(t *testing.T){}
func FuzzCurrent(f *testing.F){f.Add("x");f.Fuzz(func(t *testing.T,s string){})}
func Example(){fmt.Println("ok")
// Output: ok
}
`)
	got, err := selectedExecutableTests(root, []string{"internal/schemaoracle"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TestCurrent", "FuzzCurrent", "Example"} {
		if !got["github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle/"+name] {
			t.Fatal("missing permanent executable", name)
		}
	}
	if len(got) != 3 {
		t.Fatalf("unexpected executable set: %v", got)
	}
}
func TestVerifierSourceCommandReadonlyAndRootScope(t *testing.T) {
	root := t.TempDir()
	s := oracleSandbox{root: root, candidate: "/tmp/candidate", work: "/tmp/work", goroot: "/opt/go", modules: "/tmp/modules"}
	for _, gate := range []string{"tidy", "vet", "permanent-tests"} {
		cmd, err := verifierSourceCommand(context.Background(), s, gate)
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(cmd.Args, " ")
		for _, want := range []string{"--unshare-all", "--ro-bind " + root + " " + root, "/opt/go/bin/go -C " + root} {
			if !strings.Contains(args, want) {
				t.Fatalf("missing %s: %s", want, args)
			}
		}
		if gate == "permanent-tests" && !strings.Contains(args, "test -race -count=1 -json ./codexsdk/internal/schemaoracle") {
			t.Fatal("wrong permanent scope", args)
		}
		if !strings.Contains(strings.Join(cmd.Env, " "), "GOPROXY=off") {
			t.Fatal("network gate absent")
		}
	}
}

func TestVerifierSourceRetainsOutputTruncation(t *testing.T) {
	var output verifierSourceOutput
	output.Write([]byte(strings.Repeat("x", 65536)))
	if output.truncated {
		t.Fatal("exact bounded output marked incomplete")
	}
	output.Write([]byte("late infrastructure failure"))
	if !output.truncated || output.Len() != 65536 {
		t.Fatal("truncated output lost its unknown-outcome marker")
	}
}

func TestVerifierSourceMalformedOrderCannotProveOldAssertion(t *testing.T) {
	const pkg = "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
	for _, mode := range []string{"terminal before run", "oversized framing"} {
		t.Run(mode, func(t *testing.T) {
			events := newVerifierSourceEvents(map[string]bool{pkg + "/TestPermanent": true})
			emit := func(action, output string) {
				b, err := json.Marshal(map[string]string{"Action": action, "Package": pkg, "Test": "TestPermanent", "Output": output})
				if err != nil {
					t.Fatal(err)
				}
				events.Write(append(b, '\n'))
			}
			if mode == "terminal before run" {
				emit("pass", "")
			} else {
				events.Write([]byte(strings.Repeat("x", (4<<20)+1)))
			}
			emit("run", "")
			emit("output", "    oracle_test.go:4: contract failed\n")
			emit("fail", "")
			err := verifierSourceTestOutcome(events, sourceExitOne(t), "")
			var assertion *verifierAssertionError
			if err == nil || errors.As(err, &assertion) {
				t.Fatalf("malformed evidence counted: %v", err)
			}
		})
	}
}
