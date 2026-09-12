package repository

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestProtocolSyncRunsGeneratedProofOnComparison(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "codexsdk-upstream-protocol-sync.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "./internal/cmd/generatedproof") {
		t.Fatal("protocol sync must run the native generated-artifact proof")
	}
	if !strings.Contains(text, "steps.mechanical.outputs.escalate != 'true'") {
		t.Fatal("protocol sync must run generated proof on ordinary comparison, not only escalation")
	}
	for _, id := range []string{
		"generated-proof",
		"owner-local-tests",
		"schema-state",
		"script-tests",
		"reproof-generated",
		"reproof-owner",
		"reproof-schema",
		"reproof-scripts",
	} {
		step, ok := workflowStepByID(text, id)
		if !ok {
			t.Fatalf("missing protocol sync step id %s", id)
		}
		if !strings.Contains(step, "working-directory: codexsdk") {
			t.Fatalf("%s must set working-directory: codexsdk on that step", id)
		}
	}
	reproofGenerated, ok := workflowStepByID(text, "reproof-generated")
	if !ok {
		t.Fatal("missing reproof-generated step")
	}
	if !strings.Contains(reproofGenerated, "./internal/cmd/generatedproof") {
		t.Fatal("post-repair generated proof must run generatedproof inside the codexsdk module")
	}
	if strings.Index(text, "id: reproof-generated") > strings.Index(text, "name: Upload generated proof") {
		t.Fatal("generated proof upload must follow post-repair generated proof")
	}
	if !strings.Contains(text, "always() && (steps.generated-proof.outcome == 'success' || steps.generated-proof.outcome == 'failure'") {
		t.Fatal("generated proof artifacts must upload on proof failure, including post-repair proof")
	}
	if strings.Contains(text, `"${OUTCOME}" == "implemented"`) || strings.Contains(text, `"${PUBLISH}" == "true"`) {
		t.Fatal("protocol sync report must not treat retired mechanical implemented/publish outputs as success")
	}
	if !strings.Contains(text, `"${OUTCOME}" == "applied"`) {
		t.Fatal("protocol sync report must describe mechanical applied outcome")
	}
	if !strings.Contains(text, `"${OUTCOME}" == "current"`) {
		t.Fatal("protocol sync report must reserve already-current language for outcome=current")
	}
	if strings.Contains(text, "codexsdk_validate_sync.sh") {
		t.Fatal("protocol sync must not keep the shell validator as the generated-artifact owner")
	}
	mechanical, err := os.ReadFile(filepath.Join(root, "codexsdk", "scripts", "codexsdk_mechanical_sync.py"))
	if err != nil {
		t.Fatal(err)
	}
	mechanicalText := string(mechanical)
	if strings.Contains(mechanicalText, "./internal/cmd/generatedproof") || strings.Contains(mechanicalText, `"go", "test"`) {
		t.Fatal("mechanical sync must not own generatedproof or go test correctness decisions")
	}
	publish, err := os.ReadFile(filepath.Join(root, "codexsdk", "scripts", "codexsdk_publish_sync_pr.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publish), "generatedproof") || strings.Contains(string(publish), "go test") {
		t.Fatal("publish script must not own generatedproof or go test correctness decisions")
	}
}

func TestProtocolSyncEscalatesAppliedProofFailures(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "codexsdk-upstream-protocol-sync.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	continueOnError := "continue-on-error: ${{ steps.mechanical.outputs.applied == 'true' }}"
	for _, id := range []string{"generated-proof", "owner-local-tests", "schema-state", "script-tests"} {
		step, ok := workflowStepByID(text, id)
		if !ok {
			t.Fatalf("missing protocol sync step id %s", id)
		}
		if !strings.Contains(step, continueOnError) {
			t.Fatalf("%s must continue-on-error only after mechanical apply so outcome stays failure", id)
		}
	}
	for _, id := range []string{"reproof-gate", "fail-closed"} {
		step, ok := workflowStepByID(text, id)
		if !ok {
			t.Fatalf("missing protocol sync step id %s", id)
		}
		if strings.Contains(step, "continue-on-error:") {
			t.Fatalf("%s must not continue-on-error; re-proof and the gate fail closed", id)
		}
	}

	recoveryIDs := []string{
		"record-proof-failure",
		"codex",
		"escalation-claim",
		"reproof-generated",
		"reproof-owner",
		"reproof-schema",
		"reproof-scripts",
		"reproof-gate",
		"provenance",
		"capture",
		"commit",
		"publish",
		"fail-closed",
	}
	for _, id := range recoveryIDs {
		step, ok := workflowStepByID(text, id)
		if !ok {
			t.Fatalf("missing recovery step id %s", id)
		}
		if !strings.Contains(step, "always()") || !strings.Contains(step, "!cancelled()") {
			t.Fatalf("%s must use always() && !cancelled() so implicit success() cannot skip repair", id)
		}
		if strings.Contains(step, "failure()") {
			t.Fatalf("%s must not use failure(); continue-on-error keeps the job successful so failure() is false", id)
		}
	}

	record, ok := workflowStepByID(text, "record-proof-failure")
	if !ok {
		t.Fatal("missing record-proof-failure step")
	}
	for _, want := range []string{
		"steps.generated-proof.outcome == 'failure'",
		"steps.owner-local-tests.outcome == 'failure'",
		"steps.schema-state.outcome == 'failure'",
		"steps.script-tests.outcome == 'failure'",
	} {
		if !strings.Contains(record, want) {
			t.Fatalf("record-proof-failure must treat %s as escalation evidence", want)
		}
	}

	gate, ok := workflowStepByID(text, "fail-closed")
	if !ok {
		t.Fatal("missing fail-closed step")
	}
	for _, want := range []string{
		`"comparison"`,
		`"current"`,
		`"applied"`,
		"reproof-gate",
		"original proofs or successful reproof-gate",
		"comparison/current requires successful generated proof",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("fail-closed missing %q", want)
		}
	}
	if strings.Index(text, "id: fail-closed") < strings.Index(text, "id: reproof-gate") {
		t.Fatal("fail-closed must run after reproof-gate")
	}
}

func TestProtocolSyncReprovesFullCohortAfterRepair(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "codexsdk-upstream-protocol-sync.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	reproofIDs := []string{"reproof-generated", "reproof-owner", "reproof-schema", "reproof-scripts"}
	for _, id := range reproofIDs {
		step, ok := workflowStepByID(text, id)
		if !ok {
			t.Fatalf("missing post-repair proof step %s", id)
		}
		if !strings.Contains(step, "continue-on-error: true") {
			t.Fatalf("%s must continue-on-error so one re-proof failure still observes the rest", id)
		}
		if strings.Contains(step, "steps.reproof-generated.outcome") && id != "reproof-generated" {
			t.Fatalf("%s must not wait on another re-proof outcome", id)
		}
		if strings.Contains(step, "steps.reproof-owner.outcome") && id != "reproof-owner" {
			t.Fatalf("%s must not wait on another re-proof outcome", id)
		}
	}

	generated, ok := workflowStepByID(text, "reproof-generated")
	if !ok {
		t.Fatal("missing reproof-generated")
	}
	for _, want := range []string{
		"working-directory: codexsdk",
		"-expected-repository-commit",
		"-expected-upstream-commit",
		"-expected-upstream-ref",
		"./internal/cmd/generatedproof",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("reproof-generated missing %q", want)
		}
	}

	schema, ok := workflowStepByID(text, "reproof-schema")
	if !ok {
		t.Fatal("missing reproof-schema")
	}
	if !strings.Contains(schema, `CANDIDATE: ${{ steps.mechanical.outputs.candidate }}`) {
		t.Fatal("reproof-schema must consume the exact mechanical candidate output")
	}
	if !strings.Contains(schema, "codexsdk_sync_state.py") {
		t.Fatal("reproof-schema must rerun candidate schema-state")
	}

	scripts, ok := workflowStepByID(text, "reproof-scripts")
	if !ok {
		t.Fatal("missing reproof-scripts")
	}
	if !strings.Contains(scripts, "python3 -m unittest discover -s scripts -p '*_test.py'") {
		t.Fatal("reproof-scripts must rerun retained sync-script tests")
	}

	gate, ok := workflowStepByID(text, "reproof-gate")
	if !ok {
		t.Fatal("missing reproof-gate")
	}
	if strings.Contains(gate, "continue-on-error:") {
		t.Fatal("reproof-gate must not continue-on-error")
	}
	for _, want := range []string{
		`steps.reproof-generated.outcome`,
		`steps.reproof-owner.outcome`,
		`steps.reproof-schema.outcome`,
		`steps.reproof-scripts.outcome`,
		`[[ "${GENERATED}" == "success" ]]`,
		`[[ "${OWNER}" == "success" ]]`,
		`[[ "${SCHEMA}" == "success" ]]`,
		`[[ "${SCRIPTS}" == "success" ]]`,
		"generated-artifacts",
		"owner-local-tests",
		"schema-state",
		"script-tests",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("reproof-gate missing %q", want)
		}
	}

	for _, id := range []string{"provenance", "capture", "commit", "publish", "fail-closed"} {
		step, ok := workflowStepByID(text, id)
		if !ok {
			t.Fatalf("missing %s", id)
		}
		if !strings.Contains(step, "steps.reproof-gate.outcome == 'success'") && !strings.Contains(step, "steps.reproof-gate.outcome") {
			t.Fatalf("%s must use reproof-gate as the recovery success signal", id)
		}
		if strings.Contains(step, "escalation-validation") {
			t.Fatalf("%s must not accept retired escalation-validation as recovery", id)
		}
		if strings.Contains(step, "steps.reproof-generated.outcome == 'success'") {
			t.Fatalf("%s must not treat generated re-proof alone as full recovery", id)
		}
		if strings.Contains(step, "steps.reproof-owner.outcome == 'success'") {
			t.Fatalf("%s must not treat owner-local re-proof alone as full recovery", id)
		}
	}

	record, ok := workflowStepByID(text, "record-proof-failure")
	if !ok {
		t.Fatal("missing record-proof-failure")
	}
	for _, want := range []string{
		"proof_failure_evidence",
		`"generated-proof": os.environ.get("GENERATED", "")`,
		`"schema-state": os.environ.get("SCHEMA", "")`,
		`"script-tests": os.environ.get("SCRIPTS", "")`,
	} {
		if !strings.Contains(record, want) {
			t.Fatalf("record-proof-failure missing %q", want)
		}
	}

	mechanical, err := os.ReadFile(filepath.Join(root, "codexsdk", "scripts", "codexsdk_mechanical_sync.py"))
	if err != nil {
		t.Fatal(err)
	}
	mechanicalText := string(mechanical)
	if !strings.Contains(mechanicalText, "emit_outcome(module_root, \"escalate\", inputs, reason=reason, **candidate_output(sync_out))") {
		t.Fatal("mechanical escalate must publish the exact candidate path")
	}
	if !strings.Contains(mechanicalText, "if state not in OBSERVED_PROOF_STATES") {
		t.Fatal("escalation evidence must omit unobserved proof states")
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
