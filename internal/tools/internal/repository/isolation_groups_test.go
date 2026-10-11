package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the actual workflow shell with a deterministic Go stand-in. Discovery
// must succeed before any group runs; every selected group runs exactly once.
func TestIsolationGroupsPreserveCoverageAndFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "pr-verification.yml")
	step, ok := workflowStepByID(workflow, "repair_isolation")
	if !ok {
		t.Fatal("missing isolation execution step")
	}
	script := workflowRunScript(t, step)
	for _, mode := range []string{"success", "discovery failure", "empty", "execution failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls")
			stub := `#!/bin/bash
set -eu
printf '%s\n' "$*" >> "$CALLS"
if [[ "$*" == *" -list "* ]]; then
  if [[ "$MODE" == "discovery failure" ]]; then
    printf '%s\n' TestLiveRepairSandboxA
    exit 1
  fi
  if [[ "$MODE" != "empty" ]]; then
    printf '%s\n' TestLiveRepairSandboxA TestLiveVerifierProofMatrixNewCase TestLiveVerifierProofMatrix未来 TestLiveRepairSandboxA
  fi
  printf '%s\n' 'ok example/package 0.001s'
  exit 0
fi
if [[ "$MODE" == "execution failure" && "$*" == *'^TestLiveVerifierProofMatrixNewCase$'* ]]; then exit 1; fi
`
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "CALLS="+log, "MODE="+mode)
			out, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "success") {
				t.Fatalf("outcome %v: %s", err, out)
			}
			b, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			calls := strings.Split(strings.TrimSpace(string(b)), "\n")
			if calls[0] != "test ./codexsdk/internal/protocolsync -list ^(TestLiveRepairSandbox|TestLiveVerifierProofMatrix)" {
				t.Fatalf("changed selection: %q", calls)
			}
			want := []string{"TestLiveRepairSandboxA", "TestLiveVerifierProofMatrixNewCase", "TestLiveVerifierProofMatrix未来"}
			switch mode {
			case "discovery failure", "empty":
				want = nil
			case "execution failure":
				want = want[:2]
			}
			if len(calls) != len(want)+1 {
				t.Fatalf("omitted/repeated or continued execution: %q", calls)
			}
			for i, name := range want {
				expected := "test ./codexsdk/internal/protocolsync -run ^" + name + "$ -count=1 -timeout=10m -v"
				if calls[i+1] != expected {
					t.Fatalf("group invocation %q want %q", calls[i+1], expected)
				}
			}
		})
	}
}
