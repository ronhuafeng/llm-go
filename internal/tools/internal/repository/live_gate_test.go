package repository

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLiveResultIsRequiredBeforeEachIntegrationEffect(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "auto-forward.yml")
	step, ok := workflowStepByID(workflow, "acceptance")
	if !ok {
		t.Fatal("acceptance step must have an executable test seam")
	}
	script := workflowRunScript(t, step)
	if !strings.Contains(workflow, "run: *require_acceptance") {
		t.Fatal("write phase must recheck the same acceptance contract")
	}
	for _, test := range []struct {
		name, status, conclusion  string
		missing, oldSuccess, pass bool
	}{
		{name: "live success", status: "completed", conclusion: "success", pass: true},
		{name: "not applicable success", status: "completed", conclusion: "success", pass: true},
		{name: "missing", missing: true},
		{name: "running", status: "in_progress"},
		{name: "failure", status: "completed", conclusion: "failure"},
		{name: "cancelled", status: "completed", conclusion: "cancelled"},
		{name: "skipped", status: "completed", conclusion: "skipped"},
		{name: "new failure supersedes old success", status: "completed", conclusion: "failure", oldSuccess: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			checks := []map[string]any{}
			for _, name := range []string{"Root source verification", "Codex generated reproducibility / Generated reproducibility", "Codex protocol provenance"} {
				checks = append(checks, map[string]any{"id": len(checks) + 10, "name": name, "status": "completed", "conclusion": "success", "head_sha": strings.Repeat("a", 40), "app": map[string]string{"slug": "github-actions"}})
			}
			if test.oldSuccess {
				checks = append(checks, map[string]any{"id": 2, "name": "Live Codex integration / Live scenarios", "status": "completed", "conclusion": "success", "head_sha": strings.Repeat("a", 40), "app": map[string]string{"slug": "github-actions"}})
			}
			if !test.missing {
				checks = append(checks, map[string]any{"id": 3, "name": "Live Codex integration / Live scenarios", "status": test.status, "conclusion": test.conclusion, "head_sha": strings.Repeat("a", 40), "app": map[string]string{"slug": "github-actions"}})
			}
			data, err := json.Marshal(map[string]any{"check_runs": checks, "total_count": len(checks)})
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(dir, "checks.json")
			if err := os.WriteFile(fixture, data, 0600); err != nil {
				t.Fatal(err)
			}
			gh := `#!/bin/sh
case "$*" in
 *actions/workflows*) printf '%s' '{"total_count":1,"workflow_runs":[{"id":99,"head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","run_attempt":1,"status":"completed","conclusion":"success"}]}' ;;
 *check-runs*) cat "$CHECK_FIXTURE" ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "-c", script)
			command.Dir = root
			command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CHECK_FIXTURE="+fixture, "REPOSITORY=fixture/repo", "HEAD_SHA="+strings.Repeat("a", 40), "PR_NUMBER=1")
			out, err := command.CombinedOutput()
			if (err == nil) != test.pass {
				t.Fatalf("accepted=%v, error=%v, output=%s", test.pass, err, out)
			}
		})
	}
}

func TestLiveVerificationUsesExactCandidateAndNativeSecretMapping(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	parent := readWorkflow(t, root, "pr-verification.yml")
	live, ok := workflowJobByID(parent, "live-codex")
	if !ok {
		t.Fatal("live result must be part of PR verification before its workflow-completion dispatch")
	}
	for _, want := range []string{"uses: ./.github/workflows/live-codex-smoke.yml", "ref: ${{ github.event.pull_request.head.sha || github.sha }}", "secrets.AZURE_OPENAI_API_KEY", "secrets.CODEX_RESPONSES_API_ENDPOINT"} {
		if !strings.Contains(live, want) {
			t.Fatalf("live call missing %q", want)
		}
	}
	called := readWorkflow(t, root, "live-codex-smoke.yml")
	for _, want := range []string{"ref: ${{ inputs.ref || github.sha }}", "uses: ./.github/actions/setup-codex", "CODEX_CLI_VERSION: ${{ steps.runtime.outputs.version }}", "python3 .github/scripts/verify-protocol-gate.py live", "not applicable"} {
		if !strings.Contains(called, want) {
			t.Fatalf("live workflow missing %q", want)
		}
	}
	for _, forbidden := range []string{"@latest", "codex-responses-api-proxy", "workflow_run:", "actor.login", "contents: write", "APP_PRIVATE_KEY", "continue-on-error"} {
		if strings.Contains(called, forbidden) {
			t.Fatalf("live verification contains %q", forbidden)
		}
	}
}

func TestLiveProviderWorkflowsUseExistingInputNamesDirectly(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	bindings := regexp.MustCompile(`(?m)^\s+([A-Z_][A-Z0-9_]*):\s*\$\{\{\s*secrets\.([A-Z_][A-Z0-9_]*)\s*\}\}\s*$`)
	for _, name := range []string{"live-codex-smoke.yml", "llm-readiness.yml"} {
		t.Run(name, func(t *testing.T) {
			bound := map[string]bool{"AZURE_OPENAI_API_KEY": false, "CODEX_RESPONSES_API_ENDPOINT": false}
			matches := bindings.FindAllStringSubmatch(readWorkflow(t, root, name), -1)
			for _, binding := range matches {
				if _, canonical := bound[binding[2]]; binding[1] != binding[2] || !canonical {
					t.Fatal("provider workflow renamed or added a credential input")
				}
				bound[binding[2]] = true
			}
			for name, present := range bound {
				if !present {
					t.Errorf("provider workflow did not bind canonical input %s", name)
				}
			}
		})
	}
}

func TestNativeGoalDoesNotAutomaticallyRepeatLive(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	for _, forbidden := range []string{"recover-checks:", "actions: write", "--recover-cancelled", "rerun-failed-jobs"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("new goal has external retry authority %q", forbidden)
		}
	}
	observer, err := os.ReadFile(filepath.Join(root, "codexsdk/internal/protocolsync/goal_publication.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(observer), "checkacceptance.Observe") {
		t.Fatal("same goal must observe trusted exact-head CI")
	}
}

func TestSandboxHostSetupIsBoundedToEphemeralHostedVM(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pr-verification.yml", "live-codex-smoke.yml", "codexsdk-upstream-protocol-sync.yml"} {
		workflow := readWorkflow(t, root, name)
		if strings.Count(workflow, "id: sandbox_host") != 1 {
			t.Fatal(name, "unexpected additional administrative sandbox setup")
		}
		if name == "codexsdk-upstream-protocol-sync.yml" {
			if strings.Index(workflow, "id: sandbox_host") > strings.Index(workflow, "id: app_token") {
				t.Fatal("host prerequisites must precede publication credentials")
			}
		}
		step, ok := workflowStepByID(workflow, "sandbox_host")
		if !ok {
			t.Fatal(name, "missing explicit sandbox host prerequisite")
		}
		script := workflowRunScript(t, step)
		guard := strings.Index(script, `test "${HOSTED_ENVIRONMENT}" = github-hosted`)
		setting := strings.Index(script, "sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0")
		if guard < 0 || setting < guard || !strings.Contains(step, "HOSTED_ENVIRONMENT: ${{ runner.environment }}") {
			t.Fatal(name, "unbounded host setting")
		}
		if strings.Contains(script, "|| true") || strings.Contains(script, "--share-net") {
			t.Fatal(name, "sandbox prerequisite must fail closed")
		}
	}
}
