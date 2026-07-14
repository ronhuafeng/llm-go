package repository

import (
	"bytes"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const evidenceFormatVersion = 1

type Evidence struct {
	FormatVersion int             `json:"format_version"`
	Subject       EvidenceSubject `json:"subject"`
	Checks        []EvidenceCheck `json:"checks"`
	DoesNotProve  []string        `json:"does_not_prove,omitempty"`
}

type EvidenceSubject struct {
	Kind   string `json:"kind"`
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
	Module string `json:"module,omitempty"`
	Stage  string `json:"stage,omitempty"`
}

type EvidenceCheck struct {
	Name    string   `json:"name"`
	Command []string `json:"command,omitempty"`
	Status  string   `json:"status"`
	Error   string   `json:"error,omitempty"`
}

type evidenceRecorder struct {
	evidence Evidence
}

func newEvidence(subject EvidenceSubject) *evidenceRecorder {
	return &evidenceRecorder{evidence: Evidence{
		FormatVersion: evidenceFormatVersion,
		Subject:       subject,
	}}
}

func (recorder *evidenceRecorder) check(name string, command []string, run func() error) error {
	entry := EvidenceCheck{Name: name, Command: command, Status: "passed"}
	if err := run(); err != nil {
		entry.Status = "failed"
		entry.Error = err.Error()
		recorder.evidence.Checks = append(recorder.evidence.Checks, entry)
		return fmt.Errorf("%s: %w", name, err)
	}
	recorder.evidence.Checks = append(recorder.evidence.Checks, entry)
	return nil
}

func WriteEvidence(path string, evidence Evidence) error {
	if evidence.FormatVersion != evidenceFormatVersion {
		return fmt.Errorf("unsupported evidence format_version %d", evidence.FormatVersion)
	}
	if evidence.Subject.Kind == "" || evidence.Subject.Commit == "" || evidence.Subject.Tree == "" {
		return fmt.Errorf("evidence subject is incomplete")
	}
	return writeJSON(path, evidence)
}

func VerifyModule(root, moduleID, stage string) (Evidence, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Evidence{}, fmt.Errorf("resolve repository root: %w", err)
	}
	registered, err := loadPopulatedRegistry(root)
	if err != nil {
		return Evidence{}, err
	}
	candidate, ok := findModule(registered, moduleID)
	if !ok {
		return Evidence{}, fmt.Errorf("unknown module %q", moduleID)
	}
	if stage != "minimum" && stage != "current" && stage != "race" {
		return Evidence{}, fmt.Errorf("unknown verification stage %q", stage)
	}
	commit, err := resolveCommit(root, "HEAD")
	if err != nil {
		return Evidence{}, err
	}
	tree, err := resolveTree(root, commit)
	if err != nil {
		return Evidence{}, err
	}
	recorder := newEvidence(EvidenceSubject{
		Kind:   "module_source",
		Commit: commit,
		Tree:   tree,
		Module: moduleID,
		Stage:  stage,
	})
	moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
	runner := commandRunner{directory: moduleRoot, environment: map[string]string{
		"GOWORK":                  "off",
		"GOTOOLCHAIN":             "local",
		"PYTHONDONTWRITEBYTECODE": "1",
	}}
	run := func(name string, args ...string) error {
		return recorder.check(name, args, func() error { return runner.run(args...) })
	}
	if err = recorder.check("source identity", []string{"git", "diff", "--quiet", "HEAD", "--"}, func() error {
		return verifySourceIdentity(root, nil)
	}); err != nil {
		return recorder.evidence, err
	}

	switch stage {
	case "minimum":
		err = run("minimum Go tests", "go", "test", "./...")
	case "race":
		err = run("race tests", "go", "test", "-race", "./...")
	case "current":
		if err = recorder.check("Go formatting", []string{"gofmt", "-l", "<module Go files>"}, func() error {
			return verifyFormatting(moduleRoot)
		}); err != nil {
			break
		}
		for _, check := range []struct {
			name string
			args []string
		}{
			{name: "module checksums", args: []string{"go", "mod", "verify"}},
			{name: "module metadata", args: []string{"go", "mod", "tidy", "-diff"}},
			{name: "Go vet", args: []string{"go", "vet", "./..."}},
			{name: "ordinary tests", args: []string{"go", "test", "./..."}},
		} {
			if err = run(check.name, check.args...); err != nil {
				break
			}
		}
		if err == nil {
			err = verifyAPISurface(recorder, runner, candidate.ID)
		}
		if err == nil && candidate.ID == "codexsdk" {
			command := []string{"./scripts/codexsdk_validate_sync.sh"}
			err = recorder.check("module-owned SDK generator validation", command, func() error {
				return runner.run(command...)
			})
		}
	}
	if err == nil {
		err = recorder.check("final source identity", []string{"git", "diff", "--quiet", "HEAD", "--"}, func() error {
			return verifySourceIdentity(root, nil)
		})
	}
	return recorder.evidence, err
}

func VerifyCheckout(root string, plan AffectedPlan) (Evidence, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Evidence{}, fmt.Errorf("resolve repository root: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return Evidence{}, err
	}
	commit, err := resolveCommit(root, "HEAD")
	if err != nil {
		return Evidence{}, err
	}
	if commit != plan.Head {
		return Evidence{}, fmt.Errorf("affected plan head %s does not match checkout %s", plan.Head, commit)
	}
	recomputed, err := Affected(root, plan.Base, plan.Head)
	if err != nil {
		return Evidence{}, fmt.Errorf("recompute affected plan: %w", err)
	}
	if !reflect.DeepEqual(plan, recomputed) {
		return Evidence{}, fmt.Errorf("affected plan does not match the checkout diff and module graph")
	}
	tree, err := resolveTree(root, commit)
	if err != nil {
		return Evidence{}, err
	}
	registered, err := loadPopulatedRegistry(root)
	if err != nil {
		return Evidence{}, err
	}
	if err := validatePlanModules(plan, registered); err != nil {
		return Evidence{}, err
	}
	recorder := newEvidence(EvidenceSubject{Kind: "checkout_source", Commit: commit, Tree: tree})
	recorder.evidence.DoesNotProve = []string{
		"published module artifact identity",
		"public proxy availability",
		"module checksum database records",
	}
	if err = recorder.check("source identity", []string{"git", "diff", "--quiet", "HEAD", "--"}, func() error {
		return verifySourceIdentity(root, map[string]bool{"affected-plan.json": true})
	}); err != nil {
		return recorder.evidence, err
	}
	if err = recorder.check("repository contract", []string{"repoctl", "verify"}, func() error { return Verify(root) }); err != nil {
		return recorder.evidence, err
	}
	toolsRunner := commandRunner{
		directory:   filepath.Join(root, "internal", "tools"),
		environment: map[string]string{"GOWORK": "off", "GOTOOLCHAIN": "local"},
	}
	if err = recorder.check("repository tool tests", []string{"go", "test", "./..."}, func() error {
		return toolsRunner.run("go", "test", "./...")
	}); err != nil {
		return recorder.evidence, err
	}
	if plan.WorkspaceRequired {
		workspaceRunner := commandRunner{directory: root, environment: map[string]string{"GOWORK": filepath.Join(root, "go.work")}}
		command := []string{"go", "test", "./llmcaller/codex/llmcaller/codex", "-run", "^TestThreeLayerCanaryFast$", "-count=1", "-v"}
		if err = recorder.check("workspace three-layer canary", command, func() error {
			return workspaceRunner.run(command...)
		}); err != nil {
			return recorder.evidence, err
		}
	}
	for _, affected := range plan.PublicModules {
		candidate, ok := findModule(registered, affected.ID)
		if !ok {
			return recorder.evidence, fmt.Errorf("affected plan names unknown module %q", affected.ID)
		}
		name := "ephemeral source consumer: " + affected.ID
		if err = recorder.check(name, []string{"go", "test", "<isolated source consumer>"}, func() error {
			return verifySourceConsumer(root, candidate)
		}); err != nil {
			return recorder.evidence, err
		}
	}
	if err = recorder.check("final source identity", []string{"git", "diff", "--quiet", "HEAD", "--"}, func() error {
		return verifySourceIdentity(root, map[string]bool{"affected-plan.json": true})
	}); err != nil {
		return recorder.evidence, err
	}
	return recorder.evidence, nil
}

func resolveTree(root, commit string) (string, error) {
	output, err := gitOutput(root, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("resolve source tree: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func verifySourceIdentity(root string, allowedUntracked map[string]bool) error {
	command := exec.Command("git", "-C", root, "diff", "--quiet", "HEAD", "--")
	if err := command.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("tracked checkout differs from HEAD")
		}
		return fmt.Errorf("inspect tracked checkout: %w", err)
	}
	untracked, err := gitBytes(root, "ls-files", "--others", "-z")
	if err != nil {
		return err
	}
	var unexpected []string
	for _, path := range splitNUL(untracked) {
		if !allowedUntracked[filepath.ToSlash(path)] {
			unexpected = append(unexpected, filepath.ToSlash(path))
		}
	}
	if len(unexpected) != 0 {
		sort.Strings(unexpected)
		return fmt.Errorf("checkout contains untracked source: %s", strings.Join(unexpected, ", "))
	}
	return nil
}

func validatePlanModules(plan AffectedPlan, registered registry) error {
	wantPublic := make([]AffectedModule, 0)
	for _, affected := range plan.Modules {
		candidate, ok := findModule(registered, affected.ID)
		if !ok {
			return fmt.Errorf("affected plan names unknown module %q", affected.ID)
		}
		want := AffectedModule{
			ID:        candidate.ID,
			Dir:       candidate.Dir,
			Published: candidate.Published,
			MinimumGo: candidate.goVersion,
		}
		if affected != want {
			return fmt.Errorf("affected plan facts for module %q do not match the checkout", affected.ID)
		}
		if want.Published {
			wantPublic = append(wantPublic, want)
		}
	}
	if len(wantPublic) != len(plan.PublicModules) {
		return fmt.Errorf("affected plan public_modules do not match modules")
	}
	for index := range wantPublic {
		if wantPublic[index] != plan.PublicModules[index] {
			return fmt.Errorf("affected plan public_modules do not match modules")
		}
	}
	return nil
}

func findModule(registered registry, id string) (module, bool) {
	for _, candidate := range registered.Modules {
		if candidate.ID == id {
			return candidate, true
		}
	}
	return module{}, false
}

func verifyAPISurface(recorder *evidenceRecorder, runner commandRunner, moduleID string) error {
	commands := map[string][]string{
		"llmkit":        {"go", "test", "./internal/architecture", "-run", "^TestHandwrittenPublicAPI$", "-count=1"},
		"codexsdk":      {"go", "test", "./codexsdk", "-run", "^Test(HandwrittenPublicAPI|GeneratedFacadeAccessorsReturnConcreteOpaqueValues)$", "-count=1"},
		"codex-adapter": {"go", "test", "./internal/architecture", "-run", "^TestHandwrittenPublicAPI$", "-count=1"},
	}
	command, ok := commands[moduleID]
	if !ok {
		return nil
	}
	return recorder.check("public API inventory", command, func() error { return runner.run(command...) })
}

func verifyFormatting(root string) error {
	var unformatted []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(source)
		if err != nil {
			return fmt.Errorf("format %s: %w", path, err)
		}
		if !bytes.Equal(source, formatted) {
			relative, _ := filepath.Rel(root, path)
			unformatted = append(unformatted, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(unformatted)
	if len(unformatted) != 0 {
		return fmt.Errorf("unformatted Go files: %s", strings.Join(unformatted, ", "))
	}
	return nil
}

func verifySourceConsumer(root string, candidate module) error {
	imports := map[string]string{
		"llmkit":        candidate.path + "/llmschema",
		"codexsdk":      candidate.path + "/codexsdk",
		"codex-adapter": candidate.path + "/llmcaller/codex",
	}
	importPath, ok := imports[candidate.ID]
	if !ok {
		return fmt.Errorf("module %s has no source-consumer seam", candidate.ID)
	}
	temporary, err := os.MkdirTemp("", "llm-go-source-consumer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	runner := commandRunner{directory: temporary, environment: map[string]string{
		"GOWORK":      "off",
		"GOTOOLCHAIN": "local",
	}}
	if err := runner.run("go", "mod", "init", "example.test/llm-go-source-consumer"); err != nil {
		return err
	}
	sourceRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
	if err := runner.run("go", "mod", "edit", "-replace="+candidate.path+"="+sourceRoot); err != nil {
		return err
	}
	if err := runner.run("go", "mod", "edit", "-require="+candidate.path+"@v0.0.0"); err != nil {
		return err
	}
	source := []byte("package consumer\n\nimport _ \"" + importPath + "\"\n")
	if err := os.WriteFile(filepath.Join(temporary, "consumer_test.go"), source, 0o644); err != nil {
		return err
	}
	if err := runner.run("go", "mod", "tidy"); err != nil {
		return err
	}
	return runner.run("go", "test", "./...")
}

type commandRunner struct {
	directory   string
	environment map[string]string
}

func (runner commandRunner) run(args ...string) error {
	command := runner.command(args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (runner commandRunner) command(args ...string) *exec.Cmd {
	command := exec.Command(args[0], args[1:]...)
	command.Dir = runner.directory
	command.Env = overriddenEnvironment(runner.environment)
	return command
}

func overriddenEnvironment(overrides map[string]string) []string {
	environment := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			environment[name] = value
		}
	}
	for name, value := range overrides {
		environment[name] = value
	}
	result := make([]string, 0, len(environment))
	for name, value := range environment {
		result = append(result, name+"="+value)
	}
	sort.Strings(result)
	return result
}
