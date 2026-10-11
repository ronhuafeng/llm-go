package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialLatestRuntimeSetupResolvesOnceAndVerifiesInstall(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	data, err := os.ReadFile(filepath.Join(root, ".github/actions/setup-codex/action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	step, ok := workflowStepByID(string(data), "runtime")
	if !ok {
		t.Fatal("missing selected runtime step")
	}
	script := workflowRunScript(t, step)
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "mismatch"}[mismatch], func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				"go": `test "$*" = 'run ./internal/tools/cmd/livecodex version' || exit 41
printf x >> "$FIXTURE_CALLS"
printf '0.171.2\n'`,
				"npm": `test "$*" = 'install --global --registry=https://registry.npmjs.org @openai/codex@0.171.2' || exit 42
printf x >> "$FIXTURE_INSTALLS"`,
				"codex": `test "$*" = '--version' || exit 43
printf 'codex-cli %s\n' "$FIXTURE_OBSERVED"`,
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			observed := "0.171.2"
			if mismatch {
				observed = "0.171.3"
			}
			calls, installs, output := filepath.Join(dir, "calls"), filepath.Join(dir, "installs"), filepath.Join(dir, "output")
			cmd := exec.Command("bash", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FIXTURE_CALLS="+calls, "FIXTURE_INSTALLS="+installs, "GITHUB_OUTPUT="+output, "FIXTURE_OBSERVED="+observed)
			out, err := cmd.CombinedOutput()
			if (err != nil) != mismatch {
				t.Fatalf("mismatch=%v error=%v output=%s", mismatch, err, out)
			}
			for _, path := range []string{calls, installs} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "x" {
					t.Fatal("selection/install must happen exactly once")
				}
			}
			data, _ := os.ReadFile(output)
			if mismatch {
				if len(data) != 0 {
					t.Fatal("mismatched runtime was exported")
				}
			} else if string(data) != "version=0.171.2\n" || !strings.Contains(string(out), "codex_runtime_release=0.171.2") {
				t.Fatal("selected installed release not recorded")
			}
		})
	}
}

func TestRepairRuntimeIsIndependentOfSchemaAndLiveModel(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	step, ok := workflowStepByID(workflow, "resume")
	if !ok || !strings.Contains(step, "model: gpt-6.1-sol") || !strings.Contains(step, "reasoning-effort: high") {
		t.Fatal("formal repair model/effort not selected")
	}
	source, err := os.ReadFile(filepath.Join(root, "codexsdk/internal/cmd/protocolupgrade/main.go"))
	if err != nil {
		t.Fatal(err)
	}
	_, repair, ok := strings.Cut(string(source), "func runRepair(")
	if !ok {
		t.Fatal("missing repair entry point")
	}
	repair, _, _ = strings.Cut(repair, "func runRecover(")
	if !strings.Contains(repair, `Version: os.Getenv("CODEX_CLI_VERSION")`) || strings.Contains(repair, "baseline_metadata.json") {
		t.Fatal("repair runtime must consume run-selected version independently of baseline")
	}
}
