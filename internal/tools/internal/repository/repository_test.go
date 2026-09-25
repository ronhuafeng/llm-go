package repository

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCurrentRepositoryContract(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if violations := verifyArchitecture(root); len(violations) != 0 {
		t.Fatalf("repository contract violations:\n- %s", strings.Join(violations, "\n- "))
	}
}

func TestDependabotCoversRootModuleAndActions(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "package-ecosystem: gomod") != 1 {
		t.Fatal("dependabot.yml must have one gomod update")
	}
	if !strings.Contains(text, "package-ecosystem: github-actions") {
		t.Fatal("dependabot.yml must cover the github-actions ecosystem")
	}
	if !strings.Contains(text, "directory: /") {
		t.Fatal("dependabot.yml must cover the repository root")
	}
}

func TestPRVerificationIsRootModuleAndGeneratedReproducibility(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	text := readWorkflow(t, root, "pr-verification.yml")
	for _, want := range []string{
		"name: Root source verification",
		"name: Codex generated reproducibility",
		"gofmt",
		"git diff --check",
		"actionlint",
		"go mod tidy -diff",
		"go vet ./...",
		"go test -race ./...",
		"workflow_dispatch:",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("PR verification missing %q", want)
		}
	}
}

func TestManualNativeVerificationWorkflowsAreReadOnlyOwnerProofs(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflows := map[string][]string{
		"verify-llmkit.yml": {
			"go vet ./llmkit/...",
			"go test -race ./llmkit/...",
		},
		"verify-codexsdk.yml": {
			"go vet ./codexsdk/...",
			"go test -race ./codexsdk/...",
			"uses: ./.github/workflows/verify-generated.yml",
		},
		"verify-codex-adapter.yml": {
			"go vet ./llmcaller/codex/...",
			"go test -race ./llmcaller/codex/...",
		},
		"verify-generated.yml": {
			"go run ./internal/cmd/generatedcheck",
		},
	}
	for name, wants := range workflows {
		text := readWorkflow(t, root, name)
		if !strings.Contains(text, "workflow_dispatch:") {
			t.Errorf("%s must be manually dispatchable", name)
		}
		if !strings.Contains(text, "permissions:\n  contents: read") {
			t.Errorf("%s must be read-only", name)
		}
		for _, forbidden := range []string{"contents: write", "pull-requests: write", "secrets."} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden remote-verification authority %q", name, forbidden)
			}
		}
		refs := checkoutRefs(text)
		if len(refs) == 0 {
			t.Errorf("%s must check out the triggering revision", name)
		}
		for _, ref := range refs {
			if ref != "${{ github.sha }}" {
				t.Errorf("%s checkout ref %q is not the triggering revision", name, ref)
			}
		}
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing owner-local proof %q", name, want)
			}
		}
	}
}

func TestRequiredVerificationChecksOutTheMergeCandidate(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range requiredVerificationWorkflows(t, root) {
		text := readWorkflow(t, root, name)
		refs := checkoutRefs(text)
		if len(refs) == 0 {
			t.Fatalf("%s must check out the triggering revision", name)
		}
		for _, ref := range refs {
			if ref != "${{ github.sha }}" {
				t.Fatalf("%s checkout ref %q is not GitHub's triggering merge candidate", name, ref)
			}
		}
	}
}

func TestGeneratedVerificationIsADeterministicCheck(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pr := readWorkflow(t, root, "pr-verification.yml")
	command := "go run ./internal/cmd/generatedcheck"
	if !strings.Contains(pr, "name: Codex generated reproducibility") {
		t.Fatal("protected generated-reproducibility check name must remain")
	}
	if strings.Contains(pr, command) {
		return
	}
	called := reusableWorkflows(pr)
	if len(called) == 0 {
		t.Fatal("generated reproducibility must run the native generated check")
	}
	for _, name := range called {
		if !strings.Contains(readWorkflow(t, root, name), command) {
			t.Fatalf("%s must run the native generated check", name)
		}
	}
}

func TestGeneratedCheckObservesCleanWorktreeOnFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range requiredVerificationWorkflows(t, root) {
		text := readWorkflow(t, root, name)
		idx := strings.Index(text, "- name: Worktree remains clean")
		if idx < 0 {
			t.Fatalf("%s missing clean-tree observation", name)
		}
		chunk := text[idx:]
		if next := strings.Index(chunk[1:], "\n      - "); next >= 0 {
			chunk = chunk[:next+1]
		}
		if !strings.Contains(chunk, "always()") || !strings.Contains(chunk, "!cancelled()") {
			t.Fatalf("%s clean-tree observation is still implicit success-only", name)
		}
		if strings.Contains(chunk, "continue-on-error") {
			t.Fatalf("%s clean-tree observation must not use continue-on-error", name)
		}
	}
}

func TestProtocolSyncPreservesAuthorityAndPublicationBoundaries(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	syncText := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	if strings.Count(syncText, "uses: ./.github/actions/codex-exec") != 1 {
		t.Fatal("protocol sync must expose exactly one optional Agent invocation")
	}
	agent, ok := workflowStepByID(syncText, "codex")
	if !ok {
		t.Fatal("protocol sync must expose the Agent effect boundary")
	}
	if !strings.Contains(agent, "success()") || !strings.Contains(agent, "semantic_unresolved") {
		t.Fatal("Agent must run only after a successful read-only plan reports semantic_unresolved")
	}
	if !strings.Contains(agent, "GITHUB_TOKEN: \"\"") || !strings.Contains(agent, "GH_TOKEN: \"\"") {
		t.Fatal("Agent must not inherit repository-write tokens")
	}
	if !strings.Contains(agent, "inputs.validation_only != true") {
		t.Fatal("exact validation must not invoke the Agent")
	}
	exact, ok := workflowStepByID(syncText, "exact")
	if !ok || !strings.Contains(exact, "-validation-only") || !strings.Contains(exact, "-force-compare") {
		t.Fatal("workflow must pass exact validation and forced comparison to Go")
	}

	resume, ok := workflowStepByID(syncText, "resume")
	if !ok {
		t.Fatal("protocol sync must re-plan the same candidate after the Agent")
	}
	scope, ok := workflowStepByID(syncText, "proposal_scope")
	if !ok || !strings.Contains(scope, "allowed_agent_path") || !strings.Contains(scope, "git diff --name-only --no-renames -z HEAD") || !strings.Contains(scope, "git ls-files --others --exclude-standard -z") {
		t.Fatal("trusted workflow must inspect all Git-visible Agent changes before running proposed code")
	}
	if !strings.Contains(resume, "-candidate-sha256") || !strings.Contains(resume, "steps.mechanical.outputs.candidate_sha256") || !strings.Contains(resume, "protocolupgrade resume") {
		t.Fatal("resume must receive the initial candidate digest through immutable workflow outputs")
	}
	if strings.Index(syncText, "id: proposal_scope") > strings.Index(syncText, "id: resume") {
		t.Fatal("trusted proposal scope check must precede re-plan and apply")
	}
	checksAt := strings.Index(syncText, "id: checks")
	freezeAt := strings.Index(syncText, "id: freeze")
	controlAt := strings.Index(syncText, "id: control")
	agentAt := strings.Index(syncText, "id: codex")
	finalScopeAt := strings.Index(syncText, "Recheck Agent proposal scope after tests")
	publishAt := strings.Index(syncText, "id: publish")
	if controlAt < 0 || controlAt >= agentAt || freezeAt < 0 || freezeAt >= checksAt || checksAt < 0 || finalScopeAt <= checksAt || publishAt <= finalScopeAt || !strings.Contains(syncText, "run: *agent_proposal_scope") {
		t.Fatal("trusted proposal scope must be rechecked after tests and before publication")
	}

	checks, ok := workflowStepByID(syncText, "checks")
	if !ok {
		t.Fatal("protocol sync must expose deterministic protocol proof")
	}
	for _, want := range []string{
		"./internal/cmd/protocolupgrade",
		"check",
		"go vet ./...",
		"go test ./...",
		"gofmt",
		"diff --check",
	} {
		if !strings.Contains(checks, want) {
			t.Fatalf("deterministic protocol proof missing %q", want)
		}
	}
	if strings.Index(checks, "go test ./...") > strings.Index(checks, "go run ./internal/cmd/protocolupgrade") {
		t.Fatal("final deterministic check must run after executable tests")
	}
	freeze, ok := workflowStepByID(syncText, "freeze")
	if !ok || !strings.Contains(freeze, "git add -N") || !strings.Contains(freeze, "git diff --binary --no-ext-diff HEAD") || !strings.Contains(freeze, "patch_sha256=${digest}") || !strings.Contains(freeze, "${GITHUB_OUTPUT}") {
		t.Fatal("proof must seal proposal bytes in a completed step before executable tests")
	}

	publish, ok := workflowStepByID(syncText, "publish")
	if !ok {
		t.Fatal("protocol sync must expose a distinct publication effect")
	}
	if strings.Contains(publish, "always()") || strings.Contains(publish, "continue-on-error") {
		t.Fatal("publication must fail closed")
	}
	if strings.Contains(publish, "git rebase") {
		t.Fatal("publication must not change the proven base by rebasing")
	}
	control, ok := workflowStepByID(syncText, "control")
	if !ok || !strings.Contains(control, "go build -o") || !strings.Contains(control, "sha256sum") {
		t.Fatal("publication control must be built and fingerprinted before the Agent")
	}
	handoff, ok := workflowStepByID(syncText, "handoff")
	if !ok || !strings.Contains(handoff, "git -C \"${GITHUB_WORKSPACE}\" ls-files --others --exclude-standard -z") || !strings.Contains(handoff, "steps.freeze.outputs.patch_sha256") || !strings.Contains(handoff, "sha256sum -c -") || !strings.Contains(handoff, "steps.control.outputs.sha256") || !strings.Contains(handoff, "verify-candidate") || !strings.Contains(handoff, "patch_sha256=") {
		t.Fatal("handoff must seal unchanged source and candidate bytes after proof")
	}
	proofJob, ok := workflowJobByID(syncText, "sync")
	if !ok || !strings.Contains(proofJob, "contents: read") || strings.Contains(proofJob, "contents: write") || strings.Contains(proofJob, "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}") {
		t.Fatal("Agent and executable proposal tests must run without repository-write authority")
	}
	publishJob, ok := workflowJobByID(syncText, "publish")
	if !ok || !strings.Contains(publishJob, "needs: sync") || !strings.Contains(publishJob, "needs.sync.outputs.outcome == 'applied'") || !strings.Contains(publishJob, "contents: write") || strings.Contains(publishJob, "codex-exec") || strings.Contains(publishJob, "go test ./...") {
		t.Fatal("credentialed publication must run on a separate runner after successful proof")
	}
	if !strings.Contains(publish, "needs.sync.outputs.patch_sha256") || !strings.Contains(publish, "sha256sum -c -") || !strings.Contains(publish, "\"${control}\" stage") || !strings.Contains(publish, "\"${control}\" publish") || strings.Contains(publish, "go run ./internal/cmd/protocolupgrade") {
		t.Fatal("publication must apply the sealed patch using trusted control only")
	}

	if !strings.Contains(syncText, "./internal/cmd/protocolupgrade") {
		t.Fatal("protocol synchronization semantics must remain Go-native")
	}
	if strings.Contains(syncText, "continue-on-error") {
		t.Fatal("protocol sync must fail closed")
	}
	for _, moving := range []string{
		"uses: actions/upload-artifact@v",
		"uses: actions/download-artifact@v",
		"uses: actions/checkout@v",
		"uses: actions/setup-go@v",
	} {
		if strings.Contains(syncText, moving) {
			t.Fatalf("protocol sync uses moving third-party Action tag %q", moving)
		}
	}
}

func TestProtocolSyncProposalPatchBindsTrackedAndNewFiles(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	run(root, "", "init", "-q")
	run(root, "", "config", "user.email", "fixture@example.com")
	run(root, "", "config", "user.name", "Fixture")
	writeFile(t, root, "codexsdk/source.go", "package codexsdk\nconst A = 1\n")
	run(root, "", "add", ".")
	run(root, "", "commit", "-qm", "baseline")
	writeFile(t, root, "codexsdk/source.go", "package codexsdk\nconst A = 2\n")
	writeFile(t, root, "codexsdk/new.go", "package codexsdk\nconst B = 3\n")
	run(root, "codexsdk/new.go\x00", "add", "-N", "--pathspec-from-file=-", "--pathspec-file-nul")
	patch := run(root, "", "diff", "--binary", "--no-ext-diff", "HEAD", "--")
	if !strings.Contains(patch, "codexsdk/source.go") || !strings.Contains(patch, "codexsdk/new.go") {
		t.Fatal("sealed proposal omitted tracked or newly created source")
	}
	writeFile(t, root, "codexsdk/new.go", "package codexsdk\nconst B = 4\n")
	if patch == run(root, "", "diff", "--binary", "--no-ext-diff", "HEAD", "--") {
		t.Fatal("a test-side source rewrite must invalidate the sealed proposal")
	}
	writeFile(t, root, "codexsdk/new.go", "package codexsdk\nconst B = 3\n")
	writeFile(t, root, "codexsdk/late.go", "package codexsdk\nconst C = 4\n")
	run(root, "codexsdk/late.go\x00", "add", "-N", "--pathspec-from-file=-", "--pathspec-file-nul")
	if patch == run(root, "", "diff", "--binary", "--no-ext-diff", "HEAD", "--") {
		t.Fatal("a new file created by tests must invalidate the sealed proposal")
	}
	clean := t.TempDir()
	run(clean, "", "clone", "-q", root, ".")
	run(clean, patch, "apply", "-")
	for _, name := range []string{"codexsdk/source.go", "codexsdk/new.go"} {
		data, err := os.ReadFile(filepath.Join(clean, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "codexsdk/new.go" && !strings.Contains(string(data), "B = 3") {
			t.Fatal("publication patch did not reproduce the proven new-file bytes")
		}
	}
}

func TestProtocolDiagnosisHasReadOnlyJobBoundary(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	diagnose, ok := workflowJobByID(workflow, "diagnose")
	if !ok {
		t.Fatal("diagnostic job is missing")
	}
	if !strings.Contains(diagnose, "inputs.diagnostic_only == true") || !strings.Contains(diagnose, "contents: read") || !strings.Contains(diagnose, "pull-requests: read") {
		t.Fatal("diagnosis must have a separate read-only job")
	}
	if strings.Contains(diagnose, "contents: write") || strings.Contains(diagnose, "pull-requests: write") || strings.Contains(diagnose, "codex-exec") || strings.Contains(diagnose, "protocolupgrade publish") || strings.Contains(diagnose, "protocolupgrade resume") {
		t.Fatal("diagnosis must not contain Agent, Apply, or publication authority")
	}
	if !strings.Contains(diagnose, "go run ./internal/cmd/protocolupgrade \"${args[@]}\" -json") || !strings.Contains(diagnose, "diagnose\n") {
		t.Fatal("diagnosis must invoke the native read-only Plan entrypoint")
	}
	for _, ref := range checkoutRefs(diagnose) {
		if ref != "${{ github.sha }}" {
			t.Fatalf("diagnosis checks out %q, want triggering commit", ref)
		}
	}
	sync, ok := workflowJobByID(workflow, "sync")
	if !ok || !strings.Contains(sync, "inputs.diagnostic_only != true") {
		t.Fatal("write-capable sync job must be skipped during diagnosis")
	}
}

func TestExactProtocolValidationUsesReadOnlyJob(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	readOnly, ok := workflowJobByID(workflow, "diagnose")
	if !ok || !strings.Contains(readOnly, "inputs.validation_only == true") || !strings.Contains(readOnly, "contents: read") || !strings.Contains(readOnly, "pull-requests: read") {
		t.Fatal("exact validation must run in a read-only job")
	}
	if strings.Contains(readOnly, "contents: write") || strings.Contains(readOnly, "pull-requests: write") || strings.Contains(readOnly, "codex-exec") || strings.Contains(readOnly, "protocolupgrade publish") || strings.Contains(readOnly, "protocolupgrade resume") {
		t.Fatal("exact validation job must not have Agent or publication authority")
	}
	for _, ref := range checkoutRefs(readOnly) {
		if ref != "${{ github.sha }}" {
			t.Fatalf("exact validation checks out %q, want triggering commit", ref)
		}
	}
	sync, ok := workflowJobByID(workflow, "sync")
	if !ok || !strings.Contains(sync, "inputs.validation_only != true") {
		t.Fatal("sync preparation job must be skipped during exact validation")
	}
}

func TestProtocolAgentProposalScopeRunsBeforeUntrustedGo(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	step, ok := workflowStepByID(workflow, "proposal_scope")
	if !ok {
		t.Fatal("trusted Agent proposal scope step is missing")
	}
	script := workflowRunScript(t, step)
	for _, test := range []struct {
		name, path      string
		allowed         bool
		allowMechanical bool
	}{
		{name: "generator correction", path: "codexsdk/internal/protocolgen/type_plan.go", allowed: true},
		{name: "semantic source", path: "codexsdk/internal/protocolupgrade/manifest.go", allowed: true},
		{name: "resume control", path: "codexsdk/internal/protocolsync/sync.go"},
		{name: "acceptance control", path: "codexsdk/internal/protocolupgrade/plan.go"},
		{name: "publication control", path: "codexsdk/internal/protocolsync/publish.go"},
		{name: "generated artifact", path: "codexsdk/protocolv2/method_registry.gen.go"},
		{name: "generated artifact after Apply", path: "codexsdk/protocolv2/method_registry.gen.go", allowed: true, allowMechanical: true},
		{name: "schema artifact after Apply", path: "codexsdk/internal/protocolschema/appserver/v2/manifest.json", allowed: true, allowMechanical: true},
		{name: "publication control after tests", path: "codexsdk/internal/protocolsync/publish.go", allowMechanical: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"add", "README.md"}, {"commit", "-qm", "fixture"}} {
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			path := filepath.Join(dir, filepath.FromSlash(test.path))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("package fixture\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Dir = dir
			if test.allowMechanical {
				cmd.Env = append(os.Environ(), "ALLOW_MECHANICAL=true")
			}
			out, err := cmd.CombinedOutput()
			if test.allowed && err != nil || !test.allowed && err == nil {
				t.Fatalf("scope allowed=%v, err=%v, output=%s", test.allowed, err, out)
			}
		})
	}
}

func workflowRunScript(t *testing.T, step string) string {
	t.Helper()
	lines := strings.Split(step, "\n")
	for index, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "run: ") || !strings.HasSuffix(strings.TrimSpace(line), "|") {
			continue
		}
		indent := countLeadingSpaces(line)
		var script []string
		for _, body := range lines[index+1:] {
			if strings.TrimSpace(body) != "" && countLeadingSpaces(body) <= indent {
				break
			}
			if len(body) >= indent+2 {
				script = append(script, body[indent+2:])
			} else {
				script = append(script, "")
			}
		}
		return strings.Join(script, "\n")
	}
	t.Fatal("workflow step has no run script")
	return ""
}

func TestWorkflowJobByIDIsolatesJobs(t *testing.T) {
	yaml := "" +
		"jobs:\n" +
		"  sync:\n" +
		"    steps:\n" +
		"      - run: echo sync\n" +
		"  generated:\n" +
		"    needs: sync\n" +
		"    uses: ./.github/workflows/example.yml\n" +
		"  publish:\n" +
		"    needs: [sync, generated]\n" +
		"    if: always()\n"
	generated, ok := workflowJobByID(yaml, "generated")
	if !ok {
		t.Fatal("expected generated job")
	}
	if !strings.Contains(generated, "uses: ./.github/workflows/example.yml") {
		t.Fatalf("missing uses: %s", generated)
	}
	if strings.Contains(generated, "if: always()") || strings.Contains(generated, "run: echo sync") {
		t.Fatalf("job extractor leaked siblings: %s", generated)
	}
}

func TestWorkflowStepByIDKeepsWorkingDirectoryOnOwningStep(t *testing.T) {
	yaml := "" +
		"    steps:\n" +
		"      - name: Leaked sibling\n" +
		"        id: leaked-step\n" +
		"        working-directory: leaked\n" +
		"        run: echo leaked\n" +
		"      - name: Current step\n" +
		"        id: current-step\n" +
		"        working-directory: codexsdk\n" +
		"        run: echo current\n" +
		"      - name: Later sibling\n" +
		"        if: always()\n"
	step, ok := workflowStepByID(yaml, "current-step")
	if !ok {
		t.Fatal("expected current-step")
	}
	if !strings.Contains(step, "working-directory: codexsdk") {
		t.Fatalf("missing owning working-directory: %s", step)
	}
	if strings.Contains(step, "working-directory: leaked") {
		t.Fatalf("leaked sibling working-directory into current-step: %s", step)
	}
	if strings.Contains(step, "id: leaked-step") || strings.Contains(step, "Later sibling") {
		t.Fatalf("step extractor included siblings: %s", step)
	}

	missingCWD := strings.Replace(yaml, "        working-directory: codexsdk\n", "", 1)
	leaky, ok := workflowStepByID(missingCWD, "current-step")
	if !ok {
		t.Fatal("expected current-step after removing its working-directory")
	}
	if strings.Contains(leaky, "working-directory: codexsdk") {
		t.Fatal("removed current-step working-directory still visible on that step")
	}
	if !strings.Contains(missingCWD, "id: current-step") || !strings.Contains(missingCWD, "working-directory: leaked") {
		t.Fatal("fixture must still contain a sibling working-directory so a file-wide search would pass")
	}
}

func TestWorkflowLintUsesPinnedGoActionlint(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "pr-verification.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "curl -fsSL") || strings.Contains(text, "download-actionlint.bash") {
		t.Fatal("workflow lint must not extract actionlint with curl")
	}
	if !strings.Contains(text, "go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12") {
		t.Fatal("workflow lint must run version-pinned actionlint through Go")
	}
}

func TestCurrentDocsDescribeOneRootModule(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "CHANGELOG.md")); err != nil {
		t.Fatal("root CHANGELOG.md must be the current changelog authority")
	}
	northstar, err := os.ReadFile(filepath.Join(root, "NORTHSTAR.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(northstar), "one Go module (`github.com/ronhuafeng/llm-go`)") {
		t.Fatal("NORTHSTAR.md must name the root module")
	}
	verify, err := os.ReadFile(filepath.Join(root, "docs", "verify.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(verify), "go test -race ./...") {
		t.Fatal("docs/verify.md must document root-module race tests")
	}
	if !strings.Contains(string(verify), "merge candidate") {
		t.Fatal("docs/verify.md must say required PR checks validate the merge candidate")
	}
	for _, workflow := range []string{"Verify llmkit", "Verify codexsdk", "Verify Codex adapter"} {
		if !strings.Contains(string(verify), workflow) {
			t.Fatalf("docs/verify.md must route remote owner proof through %s", workflow)
		}
	}
	release, err := os.ReadFile(filepath.Join(root, "docs", "release.md"))
	if err != nil {
		t.Fatal(err)
	}
	releaseDoc := string(release)
	if !strings.Contains(releaseDoc, "required CI") {
		t.Fatal("docs/release.md must say required CI owns source correctness")
	}
	if !strings.Contains(releaseDoc, "immutable") {
		t.Fatal("docs/release.md must say version/tag identity is immutable")
	}
	security, err := os.ReadFile(filepath.Join(root, "SECURITY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(security), "github.com/ronhuafeng/llm-go") {
		t.Fatal("SECURITY.md must name the root module")
	}
}

func TestWorkflowsUseRootGoModFloor(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(filepath.Join(root, ".github", "workflows"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		if !strings.Contains(text, "go-version-file:") {
			return nil
		}
		if !strings.Contains(text, "go-version-file: go.mod") {
			relative, _ := filepath.Rel(root, path)
			t.Errorf("%s must use the root go.mod Go floor", filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProtocolAgentUsesGPT6SolExtraHigh(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	agent, ok := workflowStepByID(workflow, "codex")
	if !ok {
		t.Fatal("protocol sync must expose the Agent step")
	}
	if !strings.Contains(agent, "model: gpt-6-sol") {
		t.Fatal("protocol Agent workflow must select gpt-6-sol")
	}
	if !strings.Contains(agent, "reasoning-effort: xhigh") {
		t.Fatal("protocol Agent workflow must select xhigh reasoning effort")
	}

	data, err := os.ReadFile(filepath.Join(root, ".github", "actions", "codex-exec", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	action := string(data)
	if !strings.Contains(action, "--model \"${MODEL}\"") || !strings.Contains(action, "REASONING_EFFORT: ${{ inputs.reasoning-effort }}") {
		t.Fatal("codex-exec must consume the workflow-owned model and reasoning effort")
	}
}

func TestSecretBearingCodexProxyIsPinned(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		filepath.Join(".github", "workflows", "live-codex-smoke.yml"),
		filepath.Join(".github", "actions", "codex-exec", "action.yml"),
	}
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "@openai/codex-responses-api-proxy@latest") {
			t.Fatalf("%s must not install a floating credential-handling proxy", rel)
		}
		if !strings.Contains(text, "@openai/codex-responses-api-proxy@") {
			t.Fatalf("%s must install an explicit credential-handling proxy version", rel)
		}
		if strings.Contains(rel, "live-codex-smoke.yml") && !strings.Contains(text, "@openai/codex@latest") {
			t.Fatalf("%s must keep the live Codex CLI as @latest", rel)
		}
	}
}

func TestLiveCodexSmokeProviderHasSingleAuthority(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "live-codex-smoke.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "LLMGO_LIVE_CODEX_PROVIDER: llm-go-smoke") {
		t.Fatal("live Codex smoke must declare LLMGO_LIVE_CODEX_PROVIDER once")
	}
	if !strings.Contains(text, `model_provider = "${LLMGO_LIVE_CODEX_PROVIDER}"`) {
		t.Fatal("isolated config.toml must consume LLMGO_LIVE_CODEX_PROVIDER")
	}
	if !strings.Contains(text, "[model_providers.${LLMGO_LIVE_CODEX_PROVIDER}]") {
		t.Fatal("isolated provider table must consume LLMGO_LIVE_CODEX_PROVIDER")
	}
	if strings.Count(text, "llm-go-smoke") != 1 {
		t.Fatalf("live provider id copies = %d, want 1", strings.Count(text, "llm-go-smoke"))
	}
	if strings.Contains(text, "LLMGO_LIVE_CODEX_PROXY_VERSION") {
		t.Fatal("proxy version must come from the installed package, not a diagnostic env copy")
	}
}

func TestArchitectureRejectsBoundaryViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		want   string
	}{
		{
			name: "toolkit imports sdk",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/forbidden.go", "package llmkit\nimport _ \"github.com/ronhuafeng/llm-go/codexsdk\"\n")
			},
			want: "llmkit file llmkit/forbidden.go imports forbidden package family codexsdk",
		},
		{
			name: "sdk imports toolkit",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "codexsdk/forbidden.go", "package codexsdk\nimport _ \"github.com/ronhuafeng/llm-go/llmkit\"\n")
			},
			want: "codexsdk file codexsdk/forbidden.go imports forbidden package family llmkit",
		},
		{
			name: "local replacement",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", goMod(rootModulePath, "require example.com/alias v0.0.0\nreplace example.com/alias => ./codexsdk\n"))
			},
			want: "root module contains prohibited replace example.com/alias => ./codexsdk",
		},
		{
			name: "excluded module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", goMod(rootModulePath, "exclude example.com/alias v1.0.0\n"))
			},
			want: "root module contains prohibited exclude example.com/alias@v1.0.0",
		},
		{
			name: "sibling versioned require",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", goMod(rootModulePath, "require "+llmkitPath+" v0.13.0\n"))
			},
			want: "root module requires sibling versioned module " + llmkitPath,
		},
		{
			name: "nested module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "shared/go.mod", goMod("example.com/shared", ""))
			},
			want: "nested Go module shared is not allowed",
		},
		{
			name: "workspace file",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.work", "go 1.21\n\nuse .\n")
			},
			want: "repository must not contain go.work",
		},
		{
			name: "wrong module path",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", goMod("example.com/facade", ""))
			},
			want: "root module path is example.com/facade, want " + rootModulePath,
		},
		{
			name: "public package imports repository internal",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/forbidden.go", "package llmkit\nimport _ \"github.com/ronhuafeng/llm-go/internal/tools/internal/repository\"\n")
			},
			want: "llmkit file llmkit/forbidden.go imports forbidden repository internal package github.com/ronhuafeng/llm-go/internal/tools/internal/repository",
		},
		{
			name: "root facade",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "facade.go", "package llmgo\n")
			},
			want: "repository root Go file facade.go would create a root facade",
		},
		{
			name: "missing go directive",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", "module "+rootModulePath+"\n")
			},
			want: "root module: go.mod has no go directive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newArchitectureFixture(t)
			test.mutate(t, root)
			violations := strings.Join(verifyArchitecture(root), "\n")
			if !strings.Contains(violations, test.want) {
				t.Fatalf("violations %q do not contain %q", violations, test.want)
			}
		})
	}
}

func goMod(module, extra string) string {
	text := "module " + module + "\n\ngo 1.21\n"
	if extra != "" {
		text += extra
		if !strings.HasSuffix(extra, "\n") {
			text += "\n"
		}
	}
	return text
}

func newArchitectureFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", goMod(rootModulePath, ""))
	for _, directory := range []string{"llmkit", "codexsdk", "llmcaller/codex"} {
		writeFile(t, root, filepath.Join(directory, "package.go"), "package fixture\n")
	}
	return root
}

func writeFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readWorkflow(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requiredVerificationWorkflows(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{"pr-verification.yml": true}
	names := []string{"pr-verification.yml"}
	for i := 0; i < len(names); i++ {
		for _, called := range reusableWorkflows(readWorkflow(t, root, names[i])) {
			if seen[called] {
				continue
			}
			seen[called] = true
			names = append(names, called)
		}
	}
	return names
}

func reusableWorkflows(yaml string) []string {
	var names []string
	for _, line := range strings.Split(yaml, "\n") {
		trimmed := strings.TrimSpace(line)
		const prefix = "uses: ./.github/workflows/"
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		called := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if called != "" {
			names = append(names, called)
		}
	}
	return names
}

func checkoutRefs(yaml string) []string {
	var refs []string
	lines := strings.Split(yaml, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "actions/checkout") || (!strings.HasPrefix(trimmed, "- uses:") && !strings.HasPrefix(trimmed, "uses:")) {
			continue
		}
		startIndent := countLeadingSpaces(line)
		end := i + 1
		for end < len(lines) {
			next := lines[end]
			if strings.TrimSpace(next) == "" {
				end++
				continue
			}
			indent := countLeadingSpaces(next)
			if indent <= startIndent {
				break
			}
			end++
		}
		ref := "${{ github.sha }}"
		for _, stepLine := range lines[i:end] {
			stepTrimmed := strings.TrimSpace(stepLine)
			if strings.HasPrefix(stepTrimmed, "ref:") {
				ref = strings.TrimSpace(strings.TrimPrefix(stepTrimmed, "ref:"))
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

var workflowJobHeader = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):$`)

func workflowJobCount(yaml string) int {
	n := 0
	inJobs := false
	for _, line := range strings.Split(yaml, "\n") {
		if line == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		if workflowJobHeader.MatchString(line) {
			n++
		}
	}
	return n
}

func workflowJobByID(yaml, id string) (string, bool) {
	lines := strings.Split(yaml, "\n")
	inJobs := false
	start := -1
	for i, line := range lines {
		if line == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		match := workflowJobHeader.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if start >= 0 {
			return strings.Join(lines[start:i], "\n"), true
		}
		if match[1] == id {
			start = i
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n"), true
	}
	return "", false
}

func workflowStepByID(yaml, id string) (string, bool) {
	lines := strings.Split(yaml, "\n")
	idLine := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "id: "+id {
			idLine = i
			break
		}
	}
	if idLine < 0 {
		return "", false
	}
	idIndent := countLeadingSpaces(lines[idLine])
	start := idLine
	for start > 0 {
		line := lines[start]
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "- ") && countLeadingSpaces(line) < idIndent {
			break
		}
		start--
	}
	if !strings.HasPrefix(strings.TrimLeft(lines[start], " "), "- ") {
		return "", false
	}
	startIndent := countLeadingSpaces(lines[start])
	end := idLine + 1
	for end < len(lines) {
		line := lines[end]
		trimmed := strings.TrimLeft(line, " ")
		if strings.TrimSpace(line) != "" && (countLeadingSpaces(line) < startIndent || (strings.HasPrefix(trimmed, "- ") && countLeadingSpaces(line) == startIndent)) {
			break
		}
		end++
	}
	return strings.Join(lines[start:end], "\n"), true
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}

func TestProtocolSummaryNeverConvertsFailureIntoSuccess(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, ok := workflowJobByID(workflow, "summary")
	if !ok || !strings.Contains(job, "always()") || !strings.Contains(job, "permissions: {}") {
		t.Fatal("summary must report failures without effect authority")
	}
	step, ok := workflowStepByID(job, "observed_summary")
	if !ok {
		t.Fatal("missing summary")
	}
	script := workflowRunScript(t, step)
	for _, test := range []struct{ name, sync, publish, outcome, checks, want string }{
		{"compile failure", "failure", "skipped", "applied", "failure", "failed"},
		{"publication failure", "success", "failure", "applied", "success", "failed"},
		{"timeout", "cancelled", "skipped", "semantic_unresolved", "skipped", "failed"},
		{"pending", "success", "success", "pr_pending", "success", "pr_pending"},
		{"identity only", "success", "skipped", "baseline_matches", "success", "baseline_matches"},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary := filepath.Join(t.TempDir(), "summary")
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "DIAGNOSE_RESULT=skipped", "SYNC_RESULT="+test.sync, "PUBLISH_RESULT="+test.publish, "OUTCOME="+test.outcome, "CHECKS="+test.checks, "STAGE=apply", "CATEGORY=", "TARGET_REF=rust-v0.1.0", "TARGET_SHA=known-sha", "AGENT=skipped", "SCOPE=skipped", "HANDOFF=skipped", "PR_URL=", "GITHUB_SHA=repo-sha", "GITHUB_SERVER_URL=https://github.com", "GITHUB_REPOSITORY=fixture/repo", "GITHUB_RUN_ID=1", "GITHUB_RUN_ATTEMPT=1", "GITHUB_STEP_SUMMARY="+summary)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("summary: %v %s", err, out)
			}
			raw, err := os.ReadFile(summary)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "- Result: "+test.want+"\n") {
				t.Fatalf("summary = %s", raw)
			}
		})
	}
}
