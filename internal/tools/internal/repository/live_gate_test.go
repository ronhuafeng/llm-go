package repository

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
				checks = append(checks, map[string]any{"id": 1, "name": name, "status": "completed", "conclusion": "success", "app": map[string]string{"slug": "github-actions"}})
			}
			if test.oldSuccess {
				checks = append(checks, map[string]any{"id": 2, "name": "Live Codex integration / Live scenarios", "status": "completed", "conclusion": "success", "app": map[string]string{"slug": "github-actions"}})
			}
			if !test.missing {
				checks = append(checks, map[string]any{"id": 3, "name": "Live Codex integration / Live scenarios", "status": test.status, "conclusion": test.conclusion, "app": map[string]string{"slug": "github-actions"}})
			}
			data, err := json.Marshal(map[string]any{"check_runs": checks})
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(dir, "checks.json")
			if err := os.WriteFile(fixture, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nif [ \"$1\" = api ]; then cat \"$CHECK_FIXTURE\"; fi\n"), 0700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "-c", script)
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
	for _, want := range []string{"ref: ${{ inputs.ref || github.sha }}", "go run ./internal/tools/cmd/livecodex version", "go run ./internal/tools/cmd/livecodex run", "not applicable", "MINI_CODEX_API_KEY: ${{ secrets.AZURE_OPENAI_API_KEY }}"} {
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
