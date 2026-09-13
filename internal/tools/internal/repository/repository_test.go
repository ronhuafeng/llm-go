package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const fixtureToolsPath = "example.com/repository/tools"

func TestCurrentRepositoryContract(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if violations := verifyArchitecture(root); len(violations) != 0 {
		t.Fatalf("repository contract violations:\n- %s", strings.Join(violations, "\n- "))
	}
}

func TestDependabotCoversWorkspaceModulesAndActions(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	uses, err := parseGoWork(filepath.Join(root, "go.work"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "package-ecosystem: github-actions") {
		t.Fatal("dependabot.yml must cover the github-actions ecosystem")
	}
	for _, use := range uses {
		directory := "/" + strings.TrimPrefix(filepath.ToSlash(use), "./")
		if !strings.Contains(text, "package-ecosystem: gomod") || !strings.Contains(text, "directory: "+directory) {
			t.Fatalf("dependabot.yml must cover gomod directory %s", directory)
		}
	}
}

func TestPostReleaseModuleSmokeObservesPublicProxyOnce(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "post-release-module-smoke.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, module := range []string{
		"github.com/ronhuafeng/llm-go/llmkit",
		"github.com/ronhuafeng/llm-go/codexsdk",
		"github.com/ronhuafeng/llm-go/llmcaller/codex",
	} {
		if !strings.Contains(text, module) {
			t.Fatalf("post-release smoke must map %s", module)
		}
	}
	if strings.Contains(text, "types: [published]") {
		t.Fatal("post-release smoke must not rely on GITHUB_TOKEN release.published")
	}
	if !strings.Contains(text, "GOPROXY=https://proxy.golang.org") && !strings.Contains(text, "GOPROXY: https://proxy.golang.org") {
		t.Fatal("post-release smoke must pin GOPROXY to proxy.golang.org")
	}
	if strings.Contains(text, "actions/checkout") {
		t.Fatal("post-release smoke must not check out repository source")
	}
	if strings.Contains(text, ",direct") {
		t.Fatal("post-release smoke must observe proxy.golang.org without a direct fallback")
	}
	for _, banned := range []string{"sleep ", "until ", "git tag", "git push"} {
		if strings.Contains(text, banned) {
			t.Fatalf("post-release smoke must not contain %q", banned)
		}
	}
	pr, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "pr-verification.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pr), "post-release-module-smoke") {
		t.Fatal("post-release smoke must not be part of PR verification")
	}
	release, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(release), "gh workflow run post-release-module-smoke.yml") {
		t.Fatal("Release public module must dispatch the observation workflow by filename")
	}
	if !strings.Contains(string(release), "continue-on-error: true") {
		t.Fatal("observation dispatch must not fail the release job after publication")
	}
}

func TestPRVerificationIsANativeProofGraph(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pr, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "pr-verification.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(pr)
	for _, want := range []string{
		"uses: ./.github/workflows/verify-go-module.yml",
		"uses: ./.github/workflows/verify-generated.yml",
		"generated-reproducibility",
		"current-source-replaces",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("PR verification missing %q", want)
		}
	}
	if strings.Contains(text, "go mod edit") || strings.Contains(text, "cp go.mod") {
		t.Fatal("PR verification must not own current-source composition in shell")
	}
	helper, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "verify-go-module.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(helper), "go run -C internal/moduleproof ./cmd/verifymodfile") {
		t.Fatal("verify-go-module must invoke native current-source replacement")
	}
}

func TestNativeProofObservesCleanWorktreeOnFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"verify-generated.yml", "verify-go-module.yml"} {
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
	generated := readWorkflow(t, root, "verify-generated.yml")
	if strings.Contains(generated, "continue-on-error") {
		t.Fatal("generated proof must not use continue-on-error to gather cleanliness")
	}
	module := readWorkflow(t, root, "verify-go-module.yml")
	if strings.Contains(module, "continue-on-error") {
		t.Fatal("module proof must not use continue-on-error to gather cleanliness")
	}
	proof := readWorkflow(t, root, "codexsdk-protocol-proof.yml")
	if strings.Contains(proof, "Worktree remains clean") {
		t.Fatal("protocol proof must not assert a clean checkout on intentional overlay worktrees")
	}
}

func TestProtocolSyncIsOneLinearSameRunWorkflow(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	syncText := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	syncJob, ok := workflowJobByID(syncText, "sync")
	if !ok {
		t.Fatal("protocol sync must have one sync job")
	}
	if _, exists := workflowJobByID(syncText, "proof"); exists {
		t.Fatal("linear protocol sync must not call a separate proof job")
	}
	if _, exists := workflowJobByID(syncText, "publish"); exists {
		t.Fatal("linear protocol sync must publish in the same job, not a restored-artifact publish job")
	}
	if strings.Contains(syncText, "codexsdk-protocol-proof.yml") {
		t.Fatal("linear protocol sync must not call the reusable protocol proof workflow")
	}
	if strings.Contains(syncText, "actions/download-artifact") || strings.Contains(syncText, "restore-worktree") {
		t.Fatal("linear protocol sync must not restore cross-run worktree or candidate artifacts")
	}
	if strings.Contains(syncText, "codexsdk_repair_evidence.py") || strings.Contains(syncText, "failed_run") {
		t.Fatal("linear protocol sync must not admit failed-run repair evidence")
	}
	if strings.Count(syncText, "uses: ./.github/actions/codex-exec") != 1 {
		t.Fatal("real drift must invoke at most one Agent pass")
	}
	agent, ok := workflowStepByID(syncText, "codex")
	if !ok {
		t.Fatal("drift branch must invoke the Codex Agent on the same worktree")
	}
	if !strings.Contains(agent, "steps.mechanical.outputs.outcome == 'applied'") {
		t.Fatal("clean comparison must not invoke the Agent")
	}
	if strings.Index(syncJob, "id: mechanical") > strings.Index(syncJob, "id: codex") {
		t.Fatal("Agent must run after mechanical compare/apply")
	}
	checks, ok := workflowStepByID(syncText, "checks")
	if !ok {
		t.Fatal("protocol sync must run deterministic checks in the same run")
	}
	if !strings.Contains(checks, "./internal/cmd/protocolupgrade") || !strings.Contains(checks, "check") {
		t.Fatal("deterministic checks must use native protocolupgrade check")
	}
	if !strings.Contains(checks, "go vet ./...") || !strings.Contains(checks, "go test ./...") {
		t.Fatal("deterministic checks must run owner-local vet and test")
	}
	if strings.Index(syncJob, "id: codex") > strings.Index(syncJob, "id: checks") {
		t.Fatal("deterministic checks must run after the Agent")
	}
	publish, ok := workflowStepByID(syncText, "publish")
	if !ok {
		t.Fatal("protocol sync must publish from the same run after checks")
	}
	if !strings.Contains(publish, "steps.mechanical.outputs.outcome == 'applied'") {
		t.Fatal("clean comparison must not publish")
	}
	if !strings.Contains(publish, "inputs.validation_only != true") {
		t.Fatal("validation-only comparison must skip publication")
	}
	if strings.Contains(publish, "always()") {
		t.Fatal("failed checks must not publish")
	}
	if strings.Index(syncJob, "id: checks") > strings.Index(syncJob, "id: publish") {
		t.Fatal("publication must run after deterministic checks")
	}
	if !strings.Contains(publish, "--sync-mode metadata-sync") {
		t.Fatal("publication must use the ordinary protocol-sync mode")
	}
	if strings.Contains(publish, "repair-sync") {
		t.Fatal("ordinary protocol sync cannot publish repair-sync")
	}
	if strings.Contains(syncText, "git rebase") {
		t.Fatal("protocol sync must not rebase after checks")
	}

	for _, banned := range []string{"reproof-gate", "original_ok", "reproof_ok", "failed=()", "continue-on-error"} {
		if strings.Contains(syncText, banned) {
			t.Fatalf("protocol sync still contains recovery construct %q", banned)
		}
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

func TestProtocolSyncAndRepairAreNaturallyFailClosed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	syncText := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	proofText := readWorkflow(t, root, "codexsdk-protocol-proof.yml")
	repairText := readWorkflow(t, root, "codexsdk-upstream-protocol-repair.yml")

	if strings.Contains(syncText, "uses: ./.github/workflows/codexsdk-protocol-proof.yml") {
		t.Fatal("linear protocol sync must not call the reusable protocol proof")
	}
	if _, exists := workflowJobByID(syncText, "publish"); exists {
		t.Fatal("linear protocol sync must not keep a restored-artifact publish job")
	}
	for _, banned := range []string{"reproof-gate", "original_ok", "reproof_ok", "failed=()", "continue-on-error"} {
		if strings.Contains(syncText, banned) {
			t.Fatalf("normal sync still contains recovery construct %q", banned)
		}
	}

	for _, id := range []string{"generated", "owner-local", "schema-state", "script-tests"} {
		job, ok := workflowJobByID(proofText, id)
		if !ok {
			t.Fatalf("reusable protocol proof missing owner job %s", id)
		}
		if strings.Contains(job, "continue-on-error") {
			t.Fatalf("required proof job %s must not use continue-on-error", id)
		}
	}
	generated, _ := workflowJobByID(proofText, "generated")
	if !strings.Contains(generated, "./internal/cmd/generatedproof") {
		t.Fatal("reusable generated proof must run generatedproof")
	}
	if !strings.Contains(proofText, "target-kind:") {
		t.Fatal("reusable protocol proof must require target-kind")
	}
	if !strings.Contains(generated, "-expected-upstream-kind") {
		t.Fatal("generated proof must bind expected source_ref_kind")
	}
	owner, _ := workflowJobByID(proofText, "owner-local")
	if !strings.Contains(owner, "go test ./...") {
		t.Fatal("reusable owner-local job must run go test")
	}
	schema, _ := workflowJobByID(proofText, "schema-state")
	if !strings.Contains(schema, "codexsdk_sync_state.py") {
		t.Fatal("reusable schema-state job must run candidate schema-state")
	}
	scripts, _ := workflowJobByID(proofText, "script-tests")
	if !strings.Contains(scripts, "python3 -m unittest discover -s scripts -p '*_test.py'") {
		t.Fatal("reusable script-tests job must run retained script tests")
	}

	repairOn, ok := workflowJobByID(repairText, "repair")
	if !ok {
		t.Fatal("repair workflow missing repair job")
	}
	if !strings.Contains(repairText, "failed_run_id:") {
		t.Fatal("repair workflow must require an exact failed-run identity")
	}
	if strings.Index(repairOn, "codexsdk_repair_evidence.py admit") < 0 {
		t.Fatal("repair workflow must admit failed-run evidence")
	}
	if strings.Index(repairOn, "codexsdk_repair_evidence.py admit") > strings.Index(repairOn, "uses: ./.github/actions/codex-exec") {
		t.Fatal("repair workflow must admit failed-run evidence before Codex")
	}
	if strings.Index(repairOn, "uses: ./.github/actions/codex-exec") < 0 {
		t.Fatal("repair workflow must invoke Codex")
	}
	admit, ok := workflowStepByID(repairText, "admission")
	if !ok {
		t.Fatal("repair workflow missing admission step")
	}
	if strings.Contains(admit, "continue-on-error") {
		t.Fatal("admission must fail closed; Codex cannot run when admission rejects")
	}
	if !strings.Contains(repairOn, "failed-run/jobs.json") {
		t.Fatal("repair admission must observe failed-run jobs")
	}
	if !strings.Contains(repairOn, "/attempts/") {
		t.Fatal("repair must fetch attempt-specific source-run jobs")
	}
	if !strings.Contains(proofText, "generated-proof-attempt-") {
		t.Fatal("reusable proof artifacts must be attempt-addressable")
	}
	if !strings.Contains(repairText, "failed_run_attempt:") {
		t.Fatal("repair workflow must accept an exact source run attempt")
	}
	if !strings.Contains(repairOn, "repair-input/admission.json") {
		t.Fatal("repair must write normalized admission.json before Codex")
	}
	if !strings.Contains(repairOn, "repair-input/failed-logs") {
		t.Fatal("repair must collect failed proof logs from the validated run")
	}
	if strings.Contains(repairOn, "--log-failed") && strings.Contains(repairOn, "|| true") {
		t.Fatal("repair log retrieval must not fail open")
	}
	if !strings.Contains(repairOn, "require-logs") {
		t.Fatal("repair must fail closed unless required failed-owner logs exist")
	}
	if !strings.Contains(repairOn, "git checkout HEAD -- .github/actions/codex-exec") {
		t.Fatal("repair must restore historical control-plane helper before packing the proposal")
	}
	if !strings.Contains(repairOn, "--product-only") {
		t.Fatal("repair pack must reject non-product control-plane paths")
	}
	if !strings.Contains(repairOn, "generated_proof_artifact") {
		t.Fatal("repair must consume generated-proof JSON when the source run produced it")
	}

	repairProof, ok := workflowJobByID(repairText, "proof")
	if !ok {
		t.Fatal("repair workflow must invoke protocol proof")
	}
	if !strings.Contains(repairProof, "needs: repair") {
		t.Fatal("repair proof must run after Codex repair")
	}
	if !strings.Contains(repairProof, "uses: ./.github/workflows/codexsdk-protocol-proof.yml") {
		t.Fatal("repair workflow must reuse the same protocol proof")
	}
	if !strings.Contains(repairProof, "target-kind:") {
		t.Fatal("repair must pass the exact admitted target kind")
	}
	if strings.Index(repairText, "id: codex") > strings.Index(repairText, "uses: ./.github/workflows/codexsdk-protocol-proof.yml") {
		t.Fatal("repair workflow must invoke Codex before the reusable protocol proof")
	}

	repairPublish, ok := workflowJobByID(repairText, "publish")
	if !ok {
		t.Fatal("repair workflow missing publish job")
	}
	if !strings.Contains(repairPublish, "needs: [repair, proof]") && !strings.Contains(repairPublish, "needs: [proof, repair]") {
		t.Fatal("repair publication must naturally depend on proof")
	}
	if !strings.Contains(repairPublish, "--sync-mode repair-sync") {
		t.Fatal("repair publication must use fixed repair-sync")
	}
	if strings.Contains(repairPublish, "metadata-sync") {
		t.Fatal("repair workflow cannot publish metadata-sync")
	}
	if strings.Contains(jobIf(repairPublish), "always()") {
		t.Fatal("repair publication must not run when proof fails")
	}
	if strings.Contains(repairPublish, "Confirm generated baseline provenance") || strings.Contains(repairPublish, "baseline_metadata.json") {
		t.Fatal("repair publication must not re-own generated baseline provenance in Python")
	}
	if !strings.Contains(repairPublish, "--proved-tree") {
		t.Fatal("repair publication must bind the exact proved tree")
	}
	if strings.Contains(repairPublish, "git rebase") {
		t.Fatal("repair publication must not rebase after proof")
	}

	publishHelper, err := os.ReadFile(filepath.Join(root, "codexsdk", "scripts", "codexsdk_publish_sync_pr.sh"))
	if err != nil {
		t.Fatal(err)
	}
	publishHelperText := string(publishHelper)
	if strings.Contains(publishHelperText, "git rebase") {
		t.Fatal("publish helper must not rebase a proved commit onto a later landing ref")
	}
	if strings.Contains(publishHelperText, `sync_mode="repair-sync"`) || strings.Contains(publishHelperText, "Defaults to repair-sync") {
		t.Fatal("publish helper must not default --sync-mode")
	}
	if strings.Contains(publishHelperText, "--candidate") {
		t.Fatal("publish helper must not keep unused --candidate")
	}

	mechanical, err := os.ReadFile(filepath.Join(root, "codexsdk", "scripts", "codexsdk_mechanical_sync.py"))
	if err != nil {
		t.Fatal(err)
	}
	mechanicalText := string(mechanical)
	if strings.Contains(mechanicalText, "metadata-sync") || strings.Contains(mechanicalText, "repair-sync") || strings.Contains(mechanicalText, "sync_mode") {
		t.Fatal("mechanical sync must not own final publication mode")
	}
	if strings.Contains(mechanicalText, "./internal/cmd/generatedproof") || strings.Contains(mechanicalText, `"go", "test"`) {
		t.Fatal("mechanical sync must not own generatedproof or go test correctness decisions")
	}

	if strings.Contains(syncText, "codexsdk_validate_sync.sh") {
		t.Fatal("protocol sync must not keep the shell validator as the generated-artifact owner")
	}

	repairSummary, ok := workflowJobByID(repairText, "summary")
	if !ok {
		t.Fatal("repair must emit an observation-only proof summary")
	}
	if strings.Contains(repairPublish, "summary") {
		t.Fatal("repair publication must not depend on the summary job")
	}
	if !strings.Contains(repairSummary, "--source-run-id") {
		t.Fatal("repair summary must identify the source failed run")
	}

	for _, item := range []struct {
		name string
		text string
	}{
		{"codexsdk-upstream-protocol-sync.yml", syncText},
		{"codexsdk-upstream-protocol-repair.yml", repairText},
		{"codexsdk-protocol-proof.yml", proofText},
	} {
		for _, moving := range []string{
			"uses: actions/upload-artifact@v",
			"uses: actions/download-artifact@v",
			"uses: actions/checkout@v",
			"uses: actions/setup-go@v",
		} {
			if strings.Contains(item.text, moving) {
				t.Fatalf("%s uses moving third-party Action tag %q", item.name, moving)
			}
		}
	}
}

func TestWorkflowJobByIDIsolatesJobs(t *testing.T) {
	yaml := "" +
		"jobs:\n" +
		"  sync:\n" +
		"    steps:\n" +
		"      - run: echo sync\n" +
		"  proof:\n" +
		"    needs: sync\n" +
		"    uses: ./.github/workflows/codexsdk-protocol-proof.yml\n" +
		"  publish:\n" +
		"    needs: [sync, proof]\n" +
		"    if: always()\n"
	proof, ok := workflowJobByID(yaml, "proof")
	if !ok {
		t.Fatal("expected proof job")
	}
	if !strings.Contains(proof, "uses: ./.github/workflows/codexsdk-protocol-proof.yml") {
		t.Fatalf("missing uses: %s", proof)
	}
	if strings.Contains(proof, "if: always()") || strings.Contains(proof, "run: echo sync") {
		t.Fatalf("job extractor leaked siblings: %s", proof)
	}
}

func TestWorkflowStepByIDKeepsWorkingDirectoryOnOwningStep(t *testing.T) {
	yaml := "" +
		"    steps:\n" +
		"      - name: Prove checked-in generated artifacts\n" +
		"        id: generated-proof\n" +
		"        working-directory: leaked\n" +
		"        run: go run ./internal/cmd/generatedproof\n" +
		"      - name: Validate escalated protocol implementation\n" +
		"        id: escalation-validation\n" +
		"        working-directory: codexsdk\n" +
		"        run: go run ./internal/cmd/generatedproof\n" +
		"      - name: Upload generated proof\n" +
		"        if: always()\n"
	step, ok := workflowStepByID(yaml, "escalation-validation")
	if !ok {
		t.Fatal("expected escalation-validation step")
	}
	if !strings.Contains(step, "working-directory: codexsdk") {
		t.Fatalf("missing owning working-directory: %s", step)
	}
	if strings.Contains(step, "working-directory: leaked") {
		t.Fatalf("leaked sibling working-directory into escalation step: %s", step)
	}
	if strings.Contains(step, "id: generated-proof") || strings.Contains(step, "Upload generated proof") {
		t.Fatalf("step extractor included siblings: %s", step)
	}

	missingCWD := strings.Replace(yaml, "        working-directory: codexsdk\n", "", 1)
	leaky, ok := workflowStepByID(missingCWD, "escalation-validation")
	if !ok {
		t.Fatal("expected escalation-validation step after removing its working-directory")
	}
	if strings.Contains(leaky, "working-directory: codexsdk") {
		t.Fatal("removed escalation working-directory still visible on that step")
	}
	if !strings.Contains(missingCWD, "id: escalation-validation") || !strings.Contains(missingCWD, "working-directory: leaked") {
		t.Fatal("fixture must still contain a sibling working-directory so a file-wide search would pass")
	}
}

func TestReleaseReusesNativeProofEntryPoints(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "go mod edit") || strings.Contains(text, "cp go.mod") {
		t.Fatal("release verification must not own current-source composition in shell")
	}
	if !strings.Contains(text, "go run -C internal/moduleproof ./cmd/verifymodfile") {
		t.Fatal("release verification must use native current-source replacement")
	}
	if !strings.Contains(text, "./internal/cmd/generatedproof") {
		t.Fatal("release verification must reuse native generated-artifact proof")
	}
	if !strings.Contains(text, "if: inputs.module == 'codexsdk'") {
		t.Fatal("release generated-artifact proof must be owned by the codexsdk module only")
	}
}

func TestWorkflowLintUsesPinnedGoActionlint(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "verify-workflows.yml"))
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

func TestArchitectureAllowsAdditionalToolingWorkspaceModule(t *testing.T) {
	root := newArchitectureFixture(t)
	writeFile(t, root, "go.work", "go 1.23.0\n\nuse (\n\t./llmkit\n\t./codexsdk\n\t./llmcaller/codex\n\t./internal/tools\n\t./tools/extra\n)\n")
	writeFile(t, root, "tools/extra/go.mod", "module example.com/repository/extra\n\ngo 1.23.0\n")
	writeFile(t, root, "tools/extra/package.go", "package extra\n")
	if violations := verifyArchitecture(root); len(violations) != 0 {
		t.Fatalf("additional tooling module created topology violations:\n- %s", strings.Join(violations, "\n- "))
	}
}

func TestPublicModulesRejectLocalReplace(t *testing.T) {
	modules := []struct {
		dir   string
		path  string
		label string
	}{
		{dir: "llmkit", path: llmkitPath, label: "llmkit"},
		{dir: "codexsdk", path: codexSDKPath, label: "codexsdk"},
		{dir: "llmcaller/codex", path: adapterPath, label: "codex-adapter"},
	}
	for _, module := range modules {
		t.Run(module.label, func(t *testing.T) {
			root := newArchitectureFixture(t)
			writeFile(t, root, filepath.Join(module.dir, "go.mod"), "module "+module.path+"\n\ngo 1.23.0\n\nrequire example.com/alias v0.0.0\nreplace example.com/alias => ../../llmkit\n")
			want := "module " + module.label + " contains prohibited replace example.com/alias => ../../llmkit"
			violations := strings.Join(verifyArchitecture(root), "\n")
			if !strings.Contains(violations, want) {
				t.Fatalf("violations %q do not contain %q", violations, want)
			}
		})
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
			want: "module llmkit file llmkit/forbidden.go imports forbidden repository module codexsdk",
		},
		{
			name: "sdk imports toolkit",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "codexsdk/forbidden.go", "package codexsdk\nimport _ \"github.com/ronhuafeng/llm-go/llmkit\"\n")
			},
			want: "module codexsdk file codexsdk/forbidden.go imports forbidden repository module llmkit",
		},
		{
			name: "public module imports repository tools",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/forbidden.go", "package codex\nimport _ \""+fixtureToolsPath+"/helper\"\n")
			},
			want: "module codex-adapter file llmcaller/codex/forbidden.go imports forbidden repository module " + fixtureToolsPath,
		},
		{
			name: "toolkit requires sdk",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/go.mod", "module "+llmkitPath+"\n\ngo 1.23.0\n\nrequire "+codexSDKPath+" v0.8.0\n")
			},
			want: "module llmkit requires forbidden repository module codexsdk",
		},
		{
			name: "module omits minimum Go version",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/go.mod", "module "+llmkitPath+"\n")
			},
			want: "module llmkit: go.mod has no go directive",
		},
		{
			name: "local replacement",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/go.mod", "module "+llmkitPath+"\n\ngo 1.23.0\n\nrequire example.com/alias v0.0.0\nreplace example.com/alias => ../codexsdk\n")
			},
			want: "module llmkit contains prohibited replace example.com/alias => ../codexsdk",
		},
		{
			name: "version replacement",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "codexsdk/go.mod", "module "+codexSDKPath+"\n\ngo 1.23.0\n\nrequire example.com/alias v1.0.0\nreplace example.com/alias v1.0.0 => example.com/other v1.0.1\n")
			},
			want: "module codexsdk contains prohibited replace example.com/alias@v1.0.0 => example.com/other@v1.0.1",
		},
		{
			name: "excluded module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/go.mod", "module "+adapterPath+"\n\ngo 1.23.0\n\nexclude example.com/alias v1.0.0\n")
			},
			want: "module codex-adapter contains prohibited exclude example.com/alias@v1.0.0",
		},
		{
			name: "adapter omits toolkit",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/go.mod", "module "+adapterPath+"\n\ngo 1.23.0\n\nrequire "+codexSDKPath+" v0.8.0\n")
			},
			want: "module codex-adapter must directly require repository module llmkit",
		},
		{
			name: "adapter uses pseudo-version",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/go.mod", "module "+adapterPath+"\n\ngo 1.23.0\n\nrequire (\n\t"+llmkitPath+" v0.12.1-0.20260715000000-0123456789ab\n\t"+codexSDKPath+" v0.8.0\n)\n")
			},
			want: "module codex-adapter requires repository module llmkit at non-stable version",
		},
		{
			name: "untracked workspace module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "shared/go.mod", "module example.com/shared\n\ngo 1.23.0\n")
			},
			want: "Go module shared is not listed in go.work",
		},
		{
			name: "semantic owner omission",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.work", "go 1.23.0\n\nuse (\n\t./llmkit\n\t./llmcaller/codex\n\t./internal/tools\n)\n")
			},
			want: "go.work is missing semantic owner codexsdk",
		},
		{
			name: "root module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", "module example.com/facade\n\ngo 1.23.0\n")
			},
			want: "repository root must not contain go.mod",
		},
		{
			name: "root facade",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "facade.go", "package llmgo\n")
			},
			want: "repository root Go file facade.go would create a root facade",
		},
		{
			name: "workspace tooling omission",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.work", "go 1.23.0\n\nuse (\n\t./llmkit\n\t./codexsdk\n\t./llmcaller/codex\n)\n")
			},
			want: "Go module internal/tools is not listed in go.work",
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

func newArchitectureFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.23.0\n\nuse (\n\t./llmkit\n\t./codexsdk\n\t./llmcaller/codex\n\t./internal/tools\n)\n")
	modules := map[string]string{
		"llmkit":          llmkitPath,
		"codexsdk":        codexSDKPath,
		"llmcaller/codex": adapterPath,
		"internal/tools":  fixtureToolsPath,
	}
	for directory, modulePath := range modules {
		contents := fmt.Sprintf("module %s\n\ngo 1.23.0\n", modulePath)
		if directory == "llmcaller/codex" {
			contents += "\nrequire (\n\t" + llmkitPath + " v0.12.0\n\t" + codexSDKPath + " v0.8.0\n)\n"
		}
		writeFile(t, root, filepath.Join(directory, "go.mod"), contents)
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

var workflowJobHeader = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):$`)

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

func jobIf(job string) string {
	for _, line := range strings.Split(job, "\n") {
		if strings.HasPrefix(line, "    if:") && !strings.HasPrefix(line, "     ") {
			return strings.TrimSpace(line)
		}
	}
	return ""
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
		if strings.HasPrefix(trimmed, "- ") && countLeadingSpaces(line) == startIndent {
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
