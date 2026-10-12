package repository

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The former repair/verifier groups have no callers. Their replacement is the
// required source gate's concrete native-goal boundary probe, whose executed
// run+pass JSON evidence is mandatory (a skipped test must fail the gate).
func TestNativeGoalIsolationRemainsRequiredAndRejectsMissingEvidence(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "pr-verification.yml")
	source, ok := workflowJobByID(workflow, "source")
	if !ok {
		t.Fatal("required source gate missing")
	}
	for _, want := range []string{"python3 .github/scripts/verify-protocol-gate.py source", "bubblewrap", "HOSTED_ENVIRONMENT", "CODEX_CLI_VERSION: ${{ steps.runtime.outputs.version }}"} {
		if !strings.Contains(source, want) {
			t.Fatalf("native goal hosted setup missing %q", want)
		}
	}
	owner := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_verify.go")
	for _, want := range []string{`"test", "./codexsdk/internal/codexexec", "-run", "^TestNativeGoalSandboxBoundary$", "-count=1", "-json"`, `"LLMGO_NATIVE_GOAL_SANDBOX=1"`, "!native.run || !native.pass || native.skip || native.invalid", `case "skip", "fail":`} {
		if !strings.Contains(owner, want) {
			t.Fatalf("source gate lost executed isolation proof %q", want)
		}
	}
	fixture := repositorySource(t, root, "codexsdk/internal/codexexec/goal_sandbox_test.go")
	if !strings.Contains(fixture, "func TestNativeGoalSandboxBoundary(") || !strings.Contains(fixture, "LLMGO_NATIVE_GOAL_SANDBOX") {
		t.Fatal("selected native boundary consumer is missing")
	}
	// Exercise the production JSON-evidence reader using deterministic unit inputs;
	// no installed model or external service is invoked by this regression.
	cmd := exec.Command("go", "test", "./codexsdk/internal/protocolsync", "-run", "^TestGoalNativeEvidenceRejectsMissingOrSkipped$", "-count=1", "-json")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("isolation evidence regression failed: %v: %s", err, out)
	}
	var executed bool
	for _, line := range strings.Split(string(out), "\n") {
		var event struct{ Action, Test string }
		if json.Unmarshal([]byte(line), &event) == nil && event.Test == "TestGoalNativeEvidenceRejectsMissingOrSkipped" && event.Action == "pass" {
			executed = true
		}
	}
	if !executed {
		t.Fatalf("evidence regression did not execute: %s", out)
	}
}
