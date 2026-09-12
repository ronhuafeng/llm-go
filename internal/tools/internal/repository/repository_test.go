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

func TestProtocolSyncAndRepairAreNaturallyFailClosed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	syncText := readWorkflow(t, root, "codexsdk-upstream-protocol-sync.yml")
	proofText := readWorkflow(t, root, "codexsdk-protocol-proof.yml")
	repairText := readWorkflow(t, root, "codexsdk-upstream-protocol-repair.yml")

	syncProof, ok := workflowJobByID(syncText, "proof")
	if !ok {
		t.Fatal("normal sync must invoke a proof job")
	}
	if !strings.Contains(syncProof, "needs: sync") {
		t.Fatal("normal proof must depend on sync")
	}
	if !strings.Contains(syncProof, "uses: ./.github/workflows/codexsdk-protocol-proof.yml") {
		t.Fatal("normal sync must call the reusable protocol proof before publication")
	}

	syncPublish, ok := workflowJobByID(syncText, "publish")
	if !ok {
		t.Fatal("normal sync must have a publish job")
	}
	if !strings.Contains(syncPublish, "needs: [sync, proof]") && !strings.Contains(syncPublish, "needs: [proof, sync]") {
		t.Fatal("normal publication must naturally depend on proof")
	}
	if !strings.Contains(syncPublish, "--sync-mode metadata-sync") {
		t.Fatal("normal publication must use fixed metadata-sync")
	}
	if strings.Contains(syncPublish, "repair-sync") {
		t.Fatal("normal workflow cannot publish repair-sync")
	}
	if strings.Contains(jobIf(syncPublish), "always()") {
		t.Fatal("normal publication must not use always() to run after proof failure")
	}
	if !strings.Contains(syncPublish, "inputs.validation_only != true") {
		t.Fatal("validation-only comparison must skip publication")
	}

	if strings.Contains(syncText, "uses: ./.github/actions/codex-exec") {
		t.Fatal("normal sync workflow contains no Codex repair action")
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
	if !strings.Contains(repairOn, "repair-input/admission.json") {
		t.Fatal("repair must write normalized admission.json before Codex")
	}
	if !strings.Contains(repairOn, "repair-input/failed-logs") {
		t.Fatal("repair must collect failed proof logs from the validated run")
	}
	if !strings.Contains(repairOn, "-n generated-proof") {
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

	evidence, ok := workflowStepByID(syncText, "evidence")
	if !ok {
		t.Fatal("normal sync must pack exact failure evidence")
	}
	if !strings.Contains(evidence, "always()") {
		t.Fatal("evidence-upload may run on failure")
	}
	if strings.Contains(evidence, "--sync-mode") {
		t.Fatal("evidence-upload must not decide publication")
	}
	if strings.Contains(evidence, "unknown") {
		t.Fatal("evidence pack must not write unknown as a target identity")
	}

	if strings.Contains(syncText, "codexsdk_validate_sync.sh") {
		t.Fatal("protocol sync must not keep the shell validator as the generated-artifact owner")
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
