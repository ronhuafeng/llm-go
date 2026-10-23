package repository

import (
	"encoding/json"
	"os"
	"os/exec"
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
	for _, want := range []string{"goal-preflight", "-base-sha", "-app-bot-id", "-upstream-ref", "GH_READ_TOKEN: ${{ secrets.GITHUB_TOKEN }}", "goal-preflight.json", "select(. == \"run\" or . == \"skip\" or . == \"needs_recovery\")"} {
		if !strings.Contains(preflight, want) {
			t.Fatalf("preflight missing %q", want)
		}
	}
	for _, bad := range []string{"PRIVATE_KEY", "AZURE_OPENAI_API_KEY", "goal-prepare", "diagnose -", "contents: write"} {
		if strings.Contains(preflight, bad) {
			t.Fatalf("cheap read-only preflight acquired %q", bad)
		}
	}
	for _, id := range []string{"sandbox_host", "runtime", "prepare", "app_token", "draft", "binding", "goal"} {
		step, ok := workflowStepByID(job, id)
		guard := "if: ${{ steps.preflight.outputs.outcome == 'run' || steps.recovery.outputs.outcome == 'admitted' }}"
		if id == "draft" {
			guard = "if: ${{ steps.preflight.outputs.outcome == 'run' }}"
		}
		if !ok || !strings.Contains(step, guard) {
			t.Fatalf("%s can execute without a new-goal or explicit recovery admission", id)
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

func TestProtocolRecoveryAdmissionPrecedesAuthorityAndReusesDraft(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, _ := workflowJobByID(workflow, "sync")
	recovery, ok := workflowStepByID(job, "recovery")
	if !ok {
		t.Fatal("missing explicit recovery admission")
	}
	for _, want := range []string{"steps.start.outputs.recovery == 'true'", "goal-recover", "-prior-run-id", "-pr-number", "-expected-head", "-authorize-new-bounded-execution", "-new-execution-deadline", "-regenerate-empty-seed", "GH_READ_TOKEN: ${{ secrets.GITHUB_TOKEN }}", "goal-recovery.json", ".candidate_dir", ".binding_file", ".execution_deadline"} {
		if !strings.Contains(recovery, want) {
			t.Fatalf("recovery admission missing %q", want)
		}
	}
	for _, bad := range []string{"PRIVATE_KEY", "AZURE_OPENAI_API_KEY", "goal-prepare", " -base-sha ", "-target-sha", "-artifact-id", "-artifact-name", "-run-attempt", "inputs.upstream_ref"} {
		if strings.Contains(recovery, bad) {
			t.Fatalf("recovery acquired authority or operator-supplied provenance: %q", bad)
		}
	}
	for _, id := range []string{"sandbox_host", "runtime", "prepare", "app_token", "binding", "goal"} {
		if strings.Index(job, "id: "+id) < strings.Index(job, "id: recovery") {
			t.Fatalf("%s runs before recovery admission", id)
		}
	}
	draft, _ := workflowStepByID(job, "draft")
	if !strings.Contains(draft, "if: ${{ steps.preflight.outputs.outcome == 'run' }}") || strings.Contains(draft, "steps.recovery") {
		t.Fatal("recovery must never create a replacement Draft")
	}
	prepare, _ := workflowStepByID(job, "prepare")
	for _, want := range []string{"RECOVERED_CANDIDATE: ${{ steps.recovery.outputs.candidate_dir }}", `workspace="${RECOVERED_CANDIDATE}"`, `if [[ -z "${workspace}" ]]`, "jq '{Target: .target}'", `mkdir -m 700 "${RUNNER_TEMP}/goal-publication"`} {
		if !strings.Contains(prepare, want) {
			t.Fatalf("recovery source or publication isolation missing %q", want)
		}
	}
	goal, _ := workflowStepByID(job, "goal")
	for _, want := range []string{`BINDING_FILE: ${{ steps.binding.outputs.binding_file }}`, `-publication-state-dir "${RUNNER_TEMP}/goal-publication"`, `-base-sha "$(jq -r .base "${binding}")"`, `-deadline "$(jq -r .deadline "${binding}")"`, `-target-sha "$(jq -r .target_sha "${binding}")"`} {
		if !strings.Contains(goal, want) {
			t.Fatalf("execution lost authenticated binding: %q", want)
		}
	}
	evidence, _ := workflowStepByID(job, "evidence")
	for _, want := range []string{"goal-binding.json", "goal-recovery.json", "goal-publication/publication-intent.json", "goal-publication/candidate.bundle", "goal-adopted-publication/publication-intent.json", "goal-adopted-publication/candidate.bundle"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("recoverable evidence missing %q", want)
		}
	}
	if strings.Index(job, "id: binding") > strings.Index(job, "id: goal") {
		t.Fatal("launch record must be persisted before model access")
	}
	if !strings.Contains(job, "steps.preflight.outputs.outcome == 'needs_recovery'") || !strings.Contains(job, "Existing PR: ") || !strings.Contains(job, "Expected head: ") {
		t.Fatal("existing PR must be surfaced as needs_recovery, never silently skipped or resumed")
	}
}

func TestProtocolRecoveryDispatchGuardDoesNotRenewImplicitly(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, _ := workflowJobByID(workflow, "sync")
	step, _ := workflowStepByID(job, "start")
	script := workflowRunScript(t, step)
	for _, tc := range []struct {
		name                                    string
		env                                     map[string]string
		wantRecovery, wantDeadline, wantFailure bool
	}{
		{name: "scheduled new goal", env: map[string]string{"GITHUB_EVENT_NAME": "schedule"}, wantDeadline: true},
		{name: "inherited recovery deadline", env: map[string]string{"RECOVERY_RUN_ID": "100", "RECOVERY_PR_NUMBER": "20", "RECOVERY_EXPECTED_HEAD": strings.Repeat("a", 40)}, wantRecovery: true},
		{name: "explicit new recovery deadline", env: map[string]string{"RECOVERY_RUN_ID": "100", "RECOVERY_PR_NUMBER": "20", "RECOVERY_EXPECTED_HEAD": strings.Repeat("a", 40), "NEW_EXECUTION": "true"}, wantRecovery: true, wantDeadline: true},
		{name: "rerun", env: map[string]string{"GITHUB_RUN_ATTEMPT": "2"}, wantFailure: true},
		{name: "non-main", env: map[string]string{"GITHUB_REF_NAME": "proposal"}, wantFailure: true},
		{name: "missing exact head", env: map[string]string{"RECOVERY_RUN_ID": "100", "RECOVERY_PR_NUMBER": "20"}, wantFailure: true},
		{name: "operator target override", env: map[string]string{"RECOVERY_RUN_ID": "100", "RECOVERY_PR_NUMBER": "20", "RECOVERY_EXPECTED_HEAD": strings.Repeat("a", 40), "REQUESTED_REF": "rust-v9.0.0"}, wantFailure: true},
		{name: "scheduled recovery", env: map[string]string{"GITHUB_EVENT_NAME": "schedule", "RECOVERY_RUN_ID": "100", "RECOVERY_PR_NUMBER": "20", "RECOVERY_EXPECTED_HEAD": strings.Repeat("a", 40)}, wantFailure: true},
		{name: "unbound regeneration", env: map[string]string{"REGENERATE_EMPTY_SEED": "true"}, wantFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "output")
			env := map[string]string{"DEFAULT_BRANCH": "main", "GITHUB_REF_NAME": "main", "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_RUN_ATTEMPT": "1", "RECOVERY_RUN_ID": "", "RECOVERY_PR_NUMBER": "", "RECOVERY_EXPECTED_HEAD": "", "NEW_EXECUTION": "false", "REGENERATE_EMPTY_SEED": "false", "REQUESTED_REF": "", "ALLOW_DOWNGRADE": "false", "FORCE_COMPARE": "false", "GITHUB_OUTPUT": output}
			for key, value := range tc.env {
				env[key] = value
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = os.Environ()
			for key, value := range env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantFailure {
				t.Fatalf("error=%v output=%s", err, out)
			}
			data, _ := os.ReadFile(output)
			if tc.wantFailure {
				if len(data) != 0 {
					t.Fatalf("invalid dispatch exported authority: %s", data)
				}
				return
			}
			if strings.Contains(string(data), "recovery=true") != tc.wantRecovery || strings.Contains(string(data), "deadline=") != tc.wantDeadline {
				t.Fatalf("unexpected selected execution: %s", data)
			}
		})
	}
}

func TestProtocolExecutionBindingPreservesOriginalBaseAndFreshRun(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, _ := workflowJobByID(workflow, "sync")
	step, _ := workflowStepByID(job, "binding")
	script := workflowRunScript(t, step)
	for _, recovered := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "recovery"}[recovered], func(t *testing.T) {
			dir := t.TempDir()
			base, oldHead, head, controller, upstream := strings.Repeat("b", 40), strings.Repeat("a", 40), strings.Repeat("c", 40), strings.Repeat("d", 40), strings.Repeat("e", 40)
			original := map[string]any{"base": base, "head": oldHead, "branch": "codex/sync-upstream-fixture", "number": 20, "confirmed": true, "url": "https://github.com/example/repo/pull/20", "deadline": "2020-01-01T00:00:00Z", "controller_sha": base, "run_id": 99}
			writeJSON := func(name string, value any) {
				t.Helper()
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeJSON("goal-prepared.json", original)
			writeJSON("recovered-binding.json", original)
			writeJSON("goal-source.json", map[string]any{"Target": map[string]any{"RefName": "rust-v0.200.0", "RefKind": "stable_rust_tag", "PeeledCommitSHA": upstream}})
			writeJSON("goal-recovery.json", map[string]any{"expected_head": head, "prior_deadline": "2020-01-01T00:00:00Z"})
			bindingFile := ""
			if recovered {
				bindingFile = filepath.Join(dir, "recovered-binding.json")
			} else {
				head = oldHead
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "GITHUB_REPOSITORY=example/repo", "GITHUB_RUN_ID=100", "GITHUB_RUN_ATTEMPT=1", "GITHUB_SHA="+controller, "APP_BOT_ID=123", "EXECUTION_DEADLINE=2030-01-01T01:00:00Z", "RECOVERED_BINDING="+bindingFile, "GITHUB_OUTPUT="+filepath.Join(dir, "output"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("binding script: %v: %s", err, out)
			}
			data, err := os.ReadFile(filepath.Join(dir, "goal-binding.json"))
			if err != nil {
				t.Fatal(err)
			}
			var actual map[string]any
			if err = json.Unmarshal(data, &actual); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]any{"schema": float64(1), "repository": "example/repo", "run_id": float64(100), "run_attempt": float64(1), "controller_sha": controller, "base": base, "head": head, "app_bot_id": float64(123), "number": float64(20), "branch": "codex/sync-upstream-fixture", "deadline": "2030-01-01T01:00:00Z", "target_ref": "rust-v0.200.0", "target_kind": "stable_rust_tag", "target_sha": upstream, "confirmed": true, "url": "https://github.com/example/repo/pull/20"} {
				if actual[key] != want {
					t.Fatalf("binding %s=%v, want %v", key, actual[key], want)
				}
			}
			result, err := os.ReadFile(filepath.Join(dir, "goal-result.json"))
			if err != nil || !strings.Contains(string(result), "Native goal not started") {
				t.Fatal("missing explicit pre-model failure evidence")
			}
		})
	}
}

func TestProtocolRecoveryRetainsAdoptionWithoutSeedingNewWrite(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, _ := workflowJobByID(workflow, "sync")
	step, _ := workflowStepByID(job, "recovery")
	script := workflowRunScript(t, step)
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty seed", true: "retained candidate"}[retained], func(t *testing.T) {
			dir := t.TempDir()
			bin, original := filepath.Join(dir, "bin"), filepath.Join(dir, "original")
			for _, path := range []string{bin, original} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, data string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), mode); err != nil {
					t.Fatal(err)
				}
			}
			// The mocked control only returns already admitted facts; no backend or
			// repository effect participates in this workflow transport test.
			write(filepath.Join(bin, "go"), "#!/bin/sh\nset -eu\ncp \"$FIXTURE_CONTROL\" \"$3\"\n", 0700)
			control := filepath.Join(dir, "fixture-control")
			write(control, "#!/bin/sh\nset -eu\ncat \"$FIXTURE_RESULT\"\n", 0700)
			receipt := `{"deadline":"2020-01-01T00:00:00Z","intended_head":"immutable"}`
			write(filepath.Join(original, "publication-intent.json"), receipt, 0600)
			write(filepath.Join(original, "candidate.bundle"), "immutable source bytes", 0600)
			facts := map[string]any{"outcome": "admitted", "candidate_dir": filepath.Join(dir, "candidate"), "binding_file": filepath.Join(dir, "binding.json"), "execution_deadline": "2030-01-01T00:00:00Z"}
			if retained {
				facts["adopted_evidence_dir"] = original
			}
			data, err := json.Marshal(facts)
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(dir, "result.json")
			write(fixture, string(data), 0600)
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "RUNNER_TEMP="+dir, "GITHUB_WORKSPACE="+root, "GITHUB_REPOSITORY=example/repo", "GITHUB_SHA="+strings.Repeat("a", 40), "APP_BOT_ID=123", "PRIOR_RUN_ID=100", "PR_NUMBER=20", "EXPECTED_HEAD="+strings.Repeat("b", 40), "NEW_EXECUTION=false", "REGENERATE_EMPTY_SEED=false", "NEW_DEADLINE=", "GITHUB_OUTPUT="+filepath.Join(dir, "output"), "FIXTURE_CONTROL="+control, "FIXTURE_RESULT="+fixture)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("recovery transport: %v: %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(dir, "goal-publication")); !os.IsNotExist(err) {
				t.Fatal("adopted receipt became active write intent")
			}
			adopted := filepath.Join(dir, "goal-adopted-publication")
			if !retained {
				if _, err := os.Stat(adopted); !os.IsNotExist(err) {
					t.Fatal("empty seed manufactured retained candidate evidence")
				}
				return
			}
			info, err := os.Stat(adopted)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatal("adopted evidence directory must be private")
			}
			for name, want := range map[string]string{"publication-intent.json": receipt, "candidate.bundle": "immutable source bytes"} {
				got, err := os.ReadFile(filepath.Join(adopted, name))
				if err != nil || string(got) != want {
					t.Fatalf("adopted %s changed: %s (%v)", name, got, err)
				}
			}
		})
	}
}
