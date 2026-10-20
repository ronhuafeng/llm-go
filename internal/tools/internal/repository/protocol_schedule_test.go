package repository

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolScheduleUsesCheapReadOnlyPreflight(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	for _, want := range []string{"schedule:", "cron: '0 10 * * 1-5'", "workflow_dispatch:", "group: codex-upstream-protocol-sync", "cancel-in-progress: false"} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("scheduled/manual coordination missing %q", want)
		}
	}
	job, _ := workflowJobByID(workflow, "sync")
	preflight, ok := workflowStepByID(job, "preflight")
	if !ok {
		t.Fatal("missing preflight")
	}
	for _, want := range []string{"goal-preflight", "-base-sha", "-app-bot-id", "-upstream-ref", "GH_READ_TOKEN: ${{ secrets.GITHUB_TOKEN }}", "goal-preflight.json", "select(. == \"run\" or . == \"skip\")"} {
		if !strings.Contains(preflight, want) {
			t.Fatalf("preflight missing %q", want)
		}
	}
	for _, bad := range []string{"PRIVATE_KEY", "AZURE_OPENAI_API_KEY", "goal-prepare", "diagnose -", "contents: write"} {
		if strings.Contains(preflight, bad) {
			t.Fatalf("cheap read-only preflight acquired %q", bad)
		}
	}
	for _, id := range []string{"sandbox_host", "runtime", "prepare", "app_token", "draft", "goal"} {
		step, ok := workflowStepByID(job, id)
		if !ok || !strings.Contains(step, "if: ${{ steps.preflight.outputs.outcome == 'run' }}") {
			t.Fatalf("%s can execute after skip or failed preflight", id)
		}
		if strings.Index(job, "id: "+id) < strings.Index(job, "id: preflight") {
			t.Fatalf("%s precedes preflight", id)
		}
	}
	prepare, _ := workflowStepByID(job, "prepare")
	for _, want := range []string{"goal-preflight.json", "-event-name \"${GITHUB_EVENT_NAME}\"", ".Target.RefName", "$actual.RefName == $expected.RefName", "$actual.RefKind == $expected.RefKind", "$actual.PeeledCommitSHA == $expected.PeeledCommitSHA"} {
		if !strings.Contains(prepare, want) {
			t.Fatalf("selected target not retained: %q", want)
		}
	}
	if strings.Contains(prepare, "inputs.upstream_ref") {
		t.Fatal("preparation reselects manual input instead of fixed preflight target")
	}
	if !strings.Contains(job, "steps.preflight.outputs.outcome == 'skip'") || !strings.Contains(job, "No schema build, model goal, or publication was invoked.") {
		t.Fatal("skip must be reported without claiming goal acceptance")
	}
	if !strings.Contains(job, `test "${GITHUB_RUN_ATTEMPT}" = 1`) || !strings.Contains(job, `test "${GITHUB_REF_NAME}" = "${DEFAULT_BRANCH}"`) {
		t.Fatal("scheduled path weakened accepted-control or no-replay boundary")
	}
}
