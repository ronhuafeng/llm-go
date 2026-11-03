package repository

import (
	"fmt"
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
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FIXTURE_CALLS="+calls, "FIXTURE_INSTALLS="+installs, "GITHUB_OUTPUT="+output, "FIXTURE_OBSERVED="+observed, "ACCEPTED_BASE=")
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

func TestRepairRuntimeUsesAcceptedBaseAndIndependentModel(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	data, err := os.ReadFile(filepath.Join(root, "codexsdk/internal/cmd/protocolupgrade/goal_command.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{`Model: "gpt-6.1-sol"`, `Reasoning: "high"`, `Version: os.Getenv("CODEX_CLI_VERSION")`} {
		if !strings.Contains(source, want) {
			t.Fatalf("native goal missing runtime setting %s", want)
		}
	}
	if strings.Contains(source, "baseline_metadata.json") {
		t.Fatal("goal must consume the already selected version, not reread candidate metadata")
	}
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	if !strings.Contains(workflow, "CODEX_CLI_VERSION: ${{ steps.runtime.outputs.version }}") || !strings.Contains(workflow, "accepted-base: ${{ github.sha }}") {
		t.Fatal("run-resolved CLI version absent")
	}
}

// The selected executor must come from the immutable accepted commit even when
// the candidate/worktree has already advanced its protocol metadata.
func TestAcceptedBaseRuntimeSetupIgnoresCandidateAndLatest(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	data, err := os.ReadFile(filepath.Join(root, ".github/actions/setup-codex/action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	step, ok := workflowStepByID(string(data), "runtime")
	if !ok {
		t.Fatal("missing runtime step")
	}
	script := workflowRunScript(t, step)
	for _, fixture := range []struct{ version, sourceKind, sourceRef string }{
		{"0.165.0", "stable_rust_tag", "rust-v0.165.0"},
		{"0.172.3", "manual_ref", "feature/protocol"},
		{"0.172.3", "manual_commit", strings.Repeat("a", 40)},
		{"0.172.3-beta.1", "manual_ref", "feature/protocol"},
	} {
		t.Run(fixture.sourceKind+"/"+fixture.version, func(t *testing.T) {
			version := fixture.version
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				command := exec.Command("git", args...)
				command.Dir = dir
				command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid")
				out, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "-q")
			baseline := filepath.Join(dir, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json")
			if err := os.MkdirAll(filepath.Dir(baseline), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(baseline, []byte(fmt.Sprintf(`{"codex_version":"codex-cli %s","source_ref_kind":%q,"source_ref_name":%q}`, version, fixture.sourceKind, fixture.sourceRef)), 0600); err != nil {
				t.Fatal(err)
			}
			git("add", ".")
			git("commit", "-qm", "accepted baseline")
			base := git("rev-parse", "HEAD")
			if err := os.WriteFile(baseline, []byte(`{"codex_version":"codex-cli 99.0.0","source_ref_kind":"stable_rust_tag"}`), 0600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				"go": "exit 41", // Latest must not be resolved at all.
				"npm": `test "$*" = "install --global --registry=https://registry.npmjs.org @openai/codex@$FIXTURE_VERSION" || exit 42
printf x >> "$FIXTURE_INSTALLS"`,
				"codex": `printf 'codex-cli %s\n' "$FIXTURE_VERSION"`,
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			output, installs := filepath.Join(dir, "output"), filepath.Join(dir, "installs")
			command := exec.Command("bash", "-c", script)
			command.Dir = dir
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "ACCEPTED_BASE="+base, "FIXTURE_VERSION="+version, "FIXTURE_INSTALLS="+installs, "GITHUB_OUTPUT="+output)
			out, err := command.CombinedOutput()
			if strings.Contains(version, "beta") {
				if err == nil {
					t.Fatal("prerelease accepted")
				}
				if _, err := os.Stat(installs); !os.IsNotExist(err) {
					t.Fatal("invalid baseline reached install")
				}
				return
			}
			if err != nil {
				t.Fatalf("setup: %v: %s", err, out)
			}
			result, _ := os.ReadFile(output)
			installed, _ := os.ReadFile(installs)
			if string(result) != "version="+version+"\n" || string(installed) != "x" {
				t.Fatalf("wrong selection/install: %q %q", result, installed)
			}
		})
	}
}
