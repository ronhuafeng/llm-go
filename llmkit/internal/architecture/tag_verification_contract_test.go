package architecture

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTagVerificationWorkflowUsesIsolatedProxyOnlyGate(t *testing.T) {
	root := repoRoot(t)
	workflow := readContractFile(t, filepath.Join(root, ".github", "workflows", "tag-verification.yml"))
	if got := workflowPushTags(workflow); !reflect.DeepEqual(got, []string{"v*"}) {
		t.Errorf("tag verification push tags = %v, want [v*]", got)
	}
	if got := workflowJobValue(workflow, "clean-consumer", "timeout-minutes"); got != "25" {
		t.Errorf("tag verification job timeout-minutes = %q, want 25", got)
	}
	steps := workflowJobSteps(workflow, "clean-consumer")

	checkout := requireWorkflowStep(t, steps, func(step workflowStep) bool {
		return strings.HasPrefix(step.Uses, "actions/checkout@")
	})
	for key, want := range map[string]string{"ref": "${{ github.sha }}", "fetch-depth": "0", "persist-credentials": "false"} {
		if checkout.With[key] != want {
			t.Errorf("checkout %s = %q, want %q", key, checkout.With[key], want)
		}
	}
	setup := requireWorkflowStep(t, steps, func(step workflowStep) bool {
		return strings.HasPrefix(step.Uses, "actions/setup-go@")
	})
	if setup.With["cache"] != "false" {
		t.Errorf("setup-go cache = %q, want false", setup.With["cache"])
	}
	resolver := requireWorkflowStep(t, steps, func(step workflowStep) bool {
		return step.Name == "Resolve immutable tag through the public proxy"
	})
	for _, required := range []string{
		`./scripts/ci/verify-tag-consumer.sh`,
		`${{ github.ref_name }}`,
		`git rev-list -n 1`,
		`$RUNNER_TEMP/tag-verification`,
	} {
		if !strings.Contains(resolver.Run, required) {
			t.Errorf("resolver step missing %q", required)
		}
	}
	upload := requireWorkflowStep(t, steps, func(step workflowStep) bool {
		return strings.HasPrefix(step.Uses, "actions/upload-artifact@")
	})
	if upload.With["path"] != "${{ runner.temp }}/tag-verification/tag-evidence.json" {
		t.Errorf("artifact path = %q", upload.With["path"])
	}
	if upload.With["if-no-files-found"] != "error" {
		t.Errorf("artifact missing-file policy = %q", upload.With["if-no-files-found"])
	}

	script := readContractFile(t, filepath.Join(root, "scripts", "ci", "verify-tag-consumer.sh"))
	for _, required := range []string{
		`GOPROXY=https://proxy.golang.org`,
		`GOVCS='*:off'`,
		`GOWORK=off`,
		`GOMODCACHE="$workdir/modcache"`,
		`GOCACHE="$workdir/buildcache"`,
		`GOPATH="$workdir/gopath"`,
		`-timeout 10m`,
		`-retry-interval 15s`,
		`-command-timeout 10m`,
	} {
		if !strings.Contains(script, required) {
			t.Errorf("tag consumer bootstrap missing %q", required)
		}
	}
	if strings.Contains(script, "proxy.golang.org,direct") {
		t.Error("tag consumer bootstrap permits direct fallback")
	}
}

func TestWorkflowPushTagsRejectsBranchDispatchAndRunTextDecoys(t *testing.T) {
	fixture := `on:
  workflow_dispatch:
  push:
    branches:
      - main
jobs:
  clean-consumer:
    steps:
      - run: echo 'push tags v*'
`
	if got := workflowPushTags(fixture); len(got) != 0 {
		t.Fatalf("non-tag triggers satisfied tag contract: %v", got)
	}
}

func workflowJobValue(workflow string, job string, key string) string {
	for _, line := range workflowJobLines(workflow, job) {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent != 4 {
			continue
		}
		field, value, ok := strings.Cut(trimmed, ":")
		if ok && field == key {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

func TestWorkflowJobStepsDoesNotTreatSummaryTextAsArtifactUpload(t *testing.T) {
	fixture := `jobs:
  clean-consumer:
    steps:
      - name: Misleading summary
        run: |
          echo 'actions/upload-artifact@v4'
          echo 'path: tag-evidence.json'
`
	for _, step := range workflowJobSteps(fixture, "clean-consumer") {
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			t.Fatalf("summary text produced upload step: %#v", step)
		}
	}
}

type workflowStep struct {
	Name string
	Uses string
	Run  string
	With map[string]string
}

func workflowJobSteps(workflow string, job string) []workflowStep {
	var steps []workflowStep
	inSteps := false
	inWith := false
	inRun := false
	var current *workflowStep
	flush := func() {
		if current != nil {
			steps = append(steps, *current)
			current = nil
		}
	}
	for _, line := range workflowJobLines(workflow, job) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 4 && trimmed == "steps:" {
			inSteps = true
			continue
		}
		if !inSteps {
			continue
		}
		if indent == 6 && strings.HasPrefix(trimmed, "- ") {
			flush()
			current = &workflowStep{With: make(map[string]string)}
			inWith = false
			inRun = false
			parseWorkflowStepField(current, strings.TrimPrefix(trimmed, "- "))
			continue
		}
		if current == nil {
			continue
		}
		if indent == 8 {
			inWith = trimmed == "with:"
			inRun = trimmed == "run: |"
			if !inWith && !inRun {
				parseWorkflowStepField(current, trimmed)
			}
			continue
		}
		if indent >= 10 && inRun {
			current.Run += trimmed + "\n"
			continue
		}
		if indent == 10 && inWith {
			key, value, ok := strings.Cut(trimmed, ":")
			if ok {
				current.With[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
			}
		}
	}
	flush()
	return steps
}

// These contract tests intentionally share only YAML section-boundary
// discovery. Trigger and step parsing stay explicit because they validate
// different shapes; growing this into a partial general-purpose YAML parser
// would make the release gate harder, not easier, to review.
func workflowJobLines(workflow string, job string) []string {
	var lines []string
	inJob := false
	for _, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if inJob {
				lines = append(lines, line)
			}
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 2 && strings.HasSuffix(trimmed, ":") {
			if inJob {
				break
			}
			inJob = strings.TrimSuffix(trimmed, ":") == job
			continue
		}
		if inJob {
			if indent <= 2 {
				break
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func workflowPushTags(workflow string) []string {
	var tags []string
	inOn := false
	inPush := false
	inTags := false
	for _, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inOn = trimmed == "on:"
			inPush = false
			inTags = false
			continue
		}
		if !inOn {
			continue
		}
		if indent == 2 {
			inPush = trimmed == "push:"
			inTags = false
			continue
		}
		if !inPush {
			continue
		}
		if indent == 4 {
			inTags = trimmed == "tags:"
			continue
		}
		if inTags && indent == 6 && strings.HasPrefix(trimmed, "- ") {
			tags = append(tags, strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), `"'`))
		}
	}
	return tags
}

func parseWorkflowStepField(step *workflowStep, field string) {
	key, value, ok := strings.Cut(field, ":")
	if !ok {
		return
	}
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	switch strings.TrimSpace(key) {
	case "name":
		step.Name = value
	case "uses":
		step.Uses = value
	}
}

func requireWorkflowStep(t *testing.T, steps []workflowStep, predicate func(workflowStep) bool) workflowStep {
	t.Helper()
	for _, step := range steps {
		if predicate(step) {
			return step
		}
	}
	t.Fatalf("required workflow step not found: %#v", steps)
	return workflowStep{}
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
