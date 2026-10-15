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
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "pr-verification.yml")
	for _, want := range []string{"name: Root source verification", "name: Codex generated reproducibility", "name: Codex protocol provenance", "name: Live Codex integration", "github.event.pull_request.head.sha", "python3 .github/scripts/verify-protocol-gate.py source", "python3 .github/scripts/verify-protocol-gate.py provenance", "workflow_dispatch:"} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("required gate missing %q", want)
		}
	}
	owner := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_verify.go")
	for _, want := range []string{`"mod", "tidy", "-diff"`, `"vet", "./..."`, `"test", "-race", "./..."`, `"./internal/cmd/generatedcheck"`, `"build", "./codexsdk/protocolv2"`, `"verify-provenance-offline"`, `"./internal/tools/cmd/livecodex", "run"`} {
		if !strings.Contains(owner, want) {
			t.Fatalf("shared gate lost native proof %s", want)
		}
	}
	if strings.Contains(workflow, "continue-on-error") || strings.Contains(workflow, "gofmt") {
		t.Fatal("required verification must preserve command failure and committed source")
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
			"python3 .github/scripts/verify-protocol-gate.py generated",
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
			t.Errorf("%s must check out a triggering or explicit immutable revision", name)
		}
		for _, ref := range refs {
			if ref != "${{ github.sha }}" && ref != "${{ inputs.ref || github.sha }}" {
				t.Errorf("%s checkout ref %q is not the triggering/explicit revision", name, ref)
			}
		}
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing owner-local proof %q", name, want)
			}
		}
	}
}

func TestRequiredVerificationChecksOutExactPRHead(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pr := readWorkflow(t, root, "pr-verification.yml")
	if strings.Contains(pr, "merge_group:") {
		t.Fatal("auto-forward verification must not manufacture a merge-group acceptance identity")
	}
	source, ok := workflowJobByID(pr, "source")
	if !ok {
		t.Fatal("required source job missing")
	}
	refs := checkoutRefs(source)
	if len(refs) != 1 || refs[0] != "${{ github.event.pull_request.head.sha || github.sha }}" {
		t.Fatalf("required source checkout refs = %v, want exact PR H with non-PR fallback", refs)
	}
	if !strings.Contains(source, "repository: ${{ github.event.pull_request.head.repo.full_name || github.repository }}") {
		t.Fatal("source proof must bind the repository that owns exact PR H")
	}

	generated, ok := workflowJobByID(pr, "generated-reproducibility")
	if !ok {
		t.Fatal("generated required job missing")
	}
	for _, want := range []string{
		"repository: ${{ github.event.pull_request.head.repo.full_name || github.repository }}",
		"ref: ${{ github.event.pull_request.head.sha || github.sha }}",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated proof missing exact-H binding %q", want)
		}
	}
	called := readWorkflow(t, root, "verify-generated.yml")
	for _, want := range []string{
		"repository: ${{ inputs.repository || github.repository }}",
		"ref: ${{ inputs.ref || github.sha }}",
	} {
		if !strings.Contains(called, want) {
			t.Fatalf("generated reusable proof missing %q", want)
		}
	}
}

func TestPRExactProtocolValidationIsIndependentReadOnlyProof(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "pr-verification.yml")
	exact, ok := workflowJobByID(workflow, "exact-protocol")
	if !ok {
		t.Fatal("automatic protocol provenance job missing")
	}
	for _, want := range []string{
		"name: Codex protocol provenance",
		"contents: read",
		"repository: ${{ github.event.pull_request.head.repo.full_name }}",
		"ref: ${{ github.event.pull_request.head.sha }}",
		"id: classify",
		"codexsdk",
		"python3 .github/scripts/verify-protocol-gate.py provenance",
		"protocol-goal-provenance-${{ steps.goal_gate.outputs.head }}-${{ github.run_attempt }}",
		"HEAD_SHA: ${{ github.event.pull_request.head.sha || github.sha }}",
	} {
		if !strings.Contains(exact, want) {
			t.Fatalf("automatic protocol provenance missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"contents: write",
		"pull-requests: write",
		"secrets.",
		"codex-exec",
		"protocolupgrade publish",
		"protocolupgrade resume",
		"protocolupgrade check",
		"gofmt",
		"go vet ./...",
		"go test ./...",
		"INTEGRATION_SHA",
	} {
		if strings.Contains(exact, forbidden) {
			t.Fatalf("automatic protocol provenance has unrelated or forbidden responsibility %q", forbidden)
		}
	}
	owner := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_verify.go")
	if !strings.Contains(owner, `"-force-compare", "-validation-only"`) {
		t.Fatal("shared CI provenance must force exact read-only rebuild")
	}
	refs := checkoutRefs(exact)
	if len(refs) != 1 || refs[0] != "${{ github.event.pull_request.head.sha }}" {
		t.Fatalf("automatic provenance checkout refs = %v", refs)
	}
}

func TestAutoForwardWorkflowOnlyFastForwardsVerifiedHead(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workflow := readWorkflow(t, root, "auto-forward.yml")
	for _, want := range []string{
		"workflow_dispatch:",
		"group: auto-forward-main",
		"name: Verify auto-forwardable head",
		"git merge-base --is-ancestor",
		"go run ./codexsdk/internal/cmd/checkacceptance",
		"name: Fast-forward verified head",
		"AUTO_FORWARD_APP_CLIENT_ID",
		"AUTO_FORWARD_APP_PRIVATE_KEY",
		"permission-contents: write",
		"git push origin",
		"refs/heads/",
		"main readback",
		"Delete integrated head branch",
		"refusing to delete ref",
		"another open PR still uses",
		"git push origin --delete",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("auto-forward workflow missing %q", want)
		}
	}
	if strings.Index(workflow, "main readback") > strings.Index(workflow, "Delete integrated head branch") ||
		strings.Index(workflow, "Delete integrated head branch") > strings.Index(workflow, "git push origin --delete") {
		t.Fatal("head deletion must follow a successful main readback")
	}
	for _, forbidden := range []string{
		"git merge ",
		"git rebase ",
		"git cherry-pick ",
		"git commit ",
		"git push --force",
		"git push -f",
		"merge_pull_request",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("auto-forward workflow contains forbidden integration mechanism %q", forbidden)
		}
	}
	if !strings.Contains(workflow, "Require current-head checks and review policy") || !strings.Contains(workflow, "Reconfirm current-head checks and review policy") {
		t.Fatal("auto-forward must check acceptance before and immediately before the write effect")
	}
}

func TestGeneratedVerificationIsADeterministicCheck(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	pr := readWorkflow(t, root, "pr-verification.yml")
	generated, ok := workflowJobByID(pr, "generated-reproducibility")
	if !ok || !strings.Contains(generated, "uses: ./.github/workflows/verify-generated.yml") {
		t.Fatal("required generated check is disconnected")
	}
	called := readWorkflow(t, root, "verify-generated.yml")
	for _, want := range []string{"python3 .github/scripts/verify-protocol-gate.py generated", "protocol-goal-generated-${{ steps.goal_gate.outputs.head }}-${{ github.run_attempt }}", "if-no-files-found: error", "exit \"${status}\""} {
		if !strings.Contains(called, want) {
			t.Fatalf("generated gate missing %q", want)
		}
	}
	owner := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_verify.go")
	for _, want := range []string{`"./internal/cmd/generatedcheck"`, `"-write-artifacts"`, `"build", "./codexsdk/protocolv2"`} {
		if !strings.Contains(owner, want) {
			t.Fatalf("native deterministic generation proof missing %s", want)
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
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, ok := workflowJobByID(workflow, "sync")
	if !ok {
		t.Fatal("native goal job missing")
	}
	for _, want := range []string{"inputs.diagnostic_only != true && inputs.validation_only != true", "contents: read", "actions: read", "Require accepted main control", "go build -o", "goal-prepare", "\"${CONTROL}\" goal ", "-expected-head", "-pr-number", "-timeout 300m", "GH_READ_TOKEN: ${{ secrets.GITHUB_TOKEN }}"} {
		if !strings.Contains(job, want) {
			t.Fatalf("native goal boundary missing %q", want)
		}
	}
	if strings.Count(job, `"${CONTROL}" goal `) != 1 {
		t.Fatal("workflow must launch exactly one native goal")
	}
	for _, bad := range []string{"codex-exec", "continue-on-error", "continue-verifier", "publish-verifier", "gh pr merge", "gh pr ready", "git push --force"} {
		if strings.Contains(workflow, bad) {
			t.Fatalf("retired or unsafe goal authority %q", bad)
		}
	}
	flow := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_flow.go")
	for _, want := range []string{"runner.RunGoal(", "op.local(", "op.publish(", "op.observe(", "local.SnapshotRoot", "local.SnapshotDigest", "native.Status != \"complete\"", "checkacceptance.Succeeded", "candidate changed after accepted verification"} {
		if !strings.Contains(flow, want) {
			t.Fatalf("fixed same-goal pipeline missing %q", want)
		}
	}
	if strings.Index(flow, "op.local(") > strings.Index(flow, "op.publish(") || strings.Index(flow, "op.publish(") > strings.Index(flow, "op.observe(") {
		t.Fatal("native tool must verify frozen bytes before publishing and observing CI")
	}
	publisher := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_publication.go")
	for _, want := range []string{"!pr.Draft", "goalSnapshot(", `"commit-tree", tree, "-p", req.ExpectedHead`, `"push", "--porcelain"`, "checkacceptance.Observe"} {
		if !strings.Contains(publisher, want) {
			t.Fatalf("publisher/observer lost boundary %q", want)
		}
	}
	for _, bad := range []string{"--force", "/merge", `Request("PATCH"`} {
		if strings.Contains(publisher, bad) {
			t.Fatalf("Draft append publisher gained forbidden effect %q", bad)
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

func TestTrustedProtocolScopeRejectsAuthorityChanges(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	runner := t.TempDir()
	control := filepath.Join(runner, "protocolupgrade-control")
	build := exec.Command("go", "build", "-o", control, "./codexsdk/internal/cmd/protocolupgrade")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build trusted scope owner: %v: %s", err, out)
	}
	for _, test := range []struct {
		name, path      string
		allowed         bool
		allowMechanical bool
		tracked         bool
	}{
		{name: "generator correction", path: "codexsdk/internal/protocolgen/type_plan.go", allowed: true},
		{name: "current fact projection", path: "codexsdk/internal/protocolupgrade/current_facts.go", allowed: true},
		{name: "semantic source", path: "codexsdk/internal/protocolupgrade/manifest.go", allowed: true},
		{name: "tracked control", path: "codexsdk/internal/protocolsync/sync.go", tracked: true},
		{name: "resume control", path: "codexsdk/internal/protocolsync/sync.go"},
		{name: "acceptance control", path: "codexsdk/internal/protocolupgrade/plan.go"},
		{name: "publication control", path: "codexsdk/internal/protocolsync/publish.go"},
		{name: "generated artifact", path: "codexsdk/protocolv2/method_registry.gen.go"},
		{name: "generated artifact after Apply", path: "codexsdk/protocolv2/method_registry.gen.go", allowed: true, allowMechanical: true},
		{name: "schema artifact after Apply", path: "codexsdk/internal/protocolschema/appserver/v2/manifest.json", allowed: true, allowMechanical: true},
		{name: "baseline control after tests", path: "codexsdk/internal/protocolschema/appserver/v2/baseline.go", allowMechanical: true},
		{name: "new baseline test after tests", path: "codexsdk/internal/protocolschema/appserver/v2/new_test.go", allowMechanical: true},
		{name: "unknown generated output", path: "codexsdk/protocolv2/unknown.gen.go", allowMechanical: true},
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
			if test.tracked {
				for _, args := range [][]string{{"add", test.path}, {"commit", "-qm", "tracked fixture"}} {
					cmd := exec.Command("git", args...)
					cmd.Dir = dir
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("track fixture: %v: %s", err, out)
					}
				}
				if err := os.WriteFile(path, []byte("package changed\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			phase := "agent"
			if test.allowMechanical {
				phase = "final"
			}
			cmd := exec.Command(control, "scope", "-repo-root", dir, "-phase", phase)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GH_TOKEN=", "GITHUB_TOKEN=")
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
	root, _ := filepath.Abs("../../../..")
	text := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_verify.go")
	if !strings.Contains(text, `"run", "github.com/rhysd/actionlint/cmd/actionlint@v1.7.12"`) {
		t.Fatal("shared source gate must invoke version-pinned actionlint through Go")
	}
	for _, bad := range []string{"curl -fsSL", "download-actionlint.bash"} {
		if strings.Contains(text, bad) {
			t.Fatal("untrusted actionlint installation")
		}
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

func TestNativeGoalUsesFixedModelAndReasoning(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	command := repositorySource(t, root, "codexsdk/internal/cmd/protocolupgrade/goal_command.go")
	for _, want := range []string{`Model: "gpt-6.1-sol"`, `Reasoning: "high"`, "GoalSandbox:", "RunUpstreamGoal("} {
		if !strings.Contains(command, want) {
			t.Fatalf("trusted goal launcher missing %q", want)
		}
	}
}

func TestGoalUsesRunSelectedNativeCodex(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, _ := workflowJobByID(workflow, "sync")
	if strings.Count(job, "uses: ./.github/actions/setup-codex") != 1 || !strings.Contains(job, "CODEX_CLI_VERSION: ${{ steps.runtime.outputs.version }}") {
		t.Fatal("goal must inherit one official run-selected Codex runtime")
	}
	action := repositorySource(t, root, ".github/actions/setup-codex/action.yml")
	if !strings.Contains(action, "go run ./internal/tools/cmd/livecodex version") || !strings.Contains(action, "@openai/codex@${version}") || !strings.Contains(action, `test "${observed}" = "codex-cli ${version}"`) {
		t.Fatal("official latest runtime setup lost")
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
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, _ := workflowJobByID(workflow, "sync")
	if !strings.Contains(job, `goal-summary -evidence-dir`) || !strings.Contains(job, `GOAL_OUTCOME: ${{ steps.goal.outcome }}`) || strings.Contains(job, "continue-on-error") {
		t.Fatal("summary must consume the actual goal step outcome without masking native failures")
	}
	summary := repositorySource(t, root, "codexsdk/internal/cmd/protocolupgrade/goal_summary.go")
	if strings.Contains(summary, "exec.Command") || strings.Contains(summary, "checkacceptance.Observe(") {
		t.Fatal("summary must remain an offline view, not acquire checks or perform effects")
	}
	flow := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_flow.go")
	for _, want := range []string{"!accepted || native.Status != \"complete\"", "native goal completed without exact-candidate acceptance", "final exact-head CI observation is not successful"} {
		if !strings.Contains(flow, want) {
			t.Fatalf("goal completion lost independent acceptance guard %q", want)
		}
	}
}

func TestProtocolPublicationUsesRepositoryScopedApp(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	workflow := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	job, _ := workflowJobByID(workflow, "sync")
	mint, ok := workflowStepByID(job, "app_token")
	if !ok {
		t.Fatal("repository-scoped App identity missing")
	}
	for _, want := range []string{"actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1", "owner: ${{ github.repository_owner }}", "repositories: ${{ github.event.repository.name }}", "permission-contents: write", "permission-pull-requests: write"} {
		if !strings.Contains(mint, want) {
			t.Fatalf("App grant missing %s", want)
		}
	}
	for _, bad := range []string{"skip-token-revoke: true", "permission-administration", "permission-workflows", "permission-actions"} {
		if strings.Contains(mint, bad) {
			t.Fatalf("App authority expanded: %s", bad)
		}
	}
	if strings.Count(job, "GH_TOKEN: ${{ steps.app_token.outputs.token }}") != 1 {
		t.Fatal("long-running goal must not reuse the seed publication token")
	}
	for _, want := range []string{"PROTOCOL_SYNC_APP_CLIENT_ID:", "PROTOCOL_SYNC_APP_INSTALLATION_ID: ${{ steps.app_token.outputs.installation-id }}", "PROTOCOL_SYNC_APP_PRIVATE_KEY:", "GH_READ_TOKEN: ${{ secrets.GITHUB_TOKEN }}"} {
		if !strings.Contains(job, want) {
			t.Fatalf("trusted goal publication credentials missing %q", want)
		}
	}
	diagnose, _ := workflowJobByID(workflow, "diagnose")
	if strings.Contains(diagnose, "PROTOCOL_SYNC_APP_PRIVATE_KEY") || strings.Contains(diagnose, "app_token.outputs.token") {
		t.Fatal("read-only diagnosis acquired publication identity")
	}
	sandbox := repositorySource(t, root, "codexsdk/internal/codexexec/goal_sandbox.go")
	for _, want := range []string{"--unshare-all", "--tmpfs", "--ro-bind", "cmd.Env"} {
		if !strings.Contains(sandbox, want) {
			t.Fatalf("native process boundary missing %q", want)
		}
	}
	executor := repositorySource(t, root, "codexsdk/internal/codexexec/executor.go")
	if !strings.Contains(executor, `[]string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SYSTEMROOT", "LANG", "LC_ALL"}`) || strings.Contains(executor, "os.Environ()") || !strings.Contains(executor, `shell_environment_policy.exclude=["AZURE_OPENAI_API_KEY"]`) {
		t.Fatal("native runtime secret stripping missing")
	}
}

func TestNativeGoalExposesOnlyFixedVerificationTool(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	native := repositorySource(t, root, "codexsdk/internal/codexexec/goal.go")
	for _, want := range []string{`goalVerificationTool = "verify_upstream"`, `len(arguments) != 0`, `features.goals=true`, "goalSandboxCommand", "client.Threads().Start("} {
		if !strings.Contains(native, want) {
			t.Fatalf("native goal boundary missing %q", want)
		}
	}
	if strings.Count(native, "client.Threads().GoalSet(") != 1 || strings.Contains(native, "client.Turns().Start(") || strings.Contains(native, "client.Threads().GoalClear(") {
		t.Fatal("host must set one persistent native goal without authoring repair turns")
	}
	if _, err := os.Stat(filepath.Join(root, ".github/actions/codex-exec/action.yml")); !os.IsNotExist(err) {
		t.Fatal("retired split executor remains callable")
	}
}

func TestDraftGoalCannotTriggerAutomaticIntegration(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	forward := readWorkflow(t, root, "auto-forward.yml")
	for _, bad := range []string{"workflow_run:", "pull_request:", "resolve-verifier"} {
		if strings.Contains(forward, bad) {
			t.Fatalf("goal gained automatic integration route %q", bad)
		}
	}
	for _, want := range []string{"workflow_dispatch:", "checkacceptance", "git merge-base --is-ancestor"} {
		if !strings.Contains(forward, want) {
			t.Fatalf("manual integration lost guard %q", want)
		}
	}
	prepare := repositorySource(t, root, "codexsdk/internal/protocolsync/goal_prepare.go")
	for _, want := range []string{`"draft": true`, "tree != baseTree", "parents != req.BaseSHA"} {
		if !strings.Contains(prepare, want) {
			t.Fatalf("fixed Draft seed invariant missing %q", want)
		}
	}
}

func repositorySource(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCIGateLauncherPreservesNativeOwnerAndFailures(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	wrapper := repositorySource(t, root, ".github/scripts/verify-protocol-gate.py")
	for _, want := range []string{`"source", "generated", "provenance", "live"`, `"./codexsdk/internal/cmd/protocolupgrade", "verify-ci-gate"`, `"-gate", gate`, `"gate_launch_failure"`, `"passed": False`, "return status", "return status or 1", "path.parent == directory", "not path.is_symlink()"} {
		if !strings.Contains(wrapper, want) {
			t.Fatalf("CI gate launcher lost failure/identity boundary %q", want)
		}
	}
}
