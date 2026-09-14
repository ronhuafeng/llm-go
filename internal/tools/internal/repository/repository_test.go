package repository

import (
	"io/fs"
	"os"
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
	if !strings.Contains(text, "github.com/ronhuafeng/llm-go@") && !strings.Contains(text, `github.com/ronhuafeng/llm-go@$`) {
		t.Fatal("post-release smoke must resolve the root module github.com/ronhuafeng/llm-go")
	}
	for _, pkg := range []string{
		"github.com/ronhuafeng/llm-go/llmkit",
		"github.com/ronhuafeng/llm-go/codexsdk",
		"github.com/ronhuafeng/llm-go/llmcaller/codex",
	} {
		if !strings.Contains(text, pkg) {
			t.Fatalf("post-release smoke must load %s", pkg)
		}
	}
	if !strings.Contains(text, "go mod tidy") {
		t.Fatal("post-release smoke must complete consumer go.sum setup")
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
		t.Fatal("root-module release must dispatch the observation workflow by filename")
	}
	if !strings.Contains(string(release), "continue-on-error: true") {
		t.Fatal("observation dispatch must not fail the release job after publication")
	}
}

func TestPRVerificationIsRootModuleAndGeneratedReproducibility(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	text := readWorkflow(t, root, "pr-verification.yml")
	if workflowJobCount(text) != 2 {
		t.Fatal("PR verification must have exactly two jobs")
	}
	if _, ok := workflowJobByID(text, "source"); !ok {
		t.Fatal("PR verification must include root source verification")
	}
	if _, ok := workflowJobByID(text, "generated-reproducibility"); !ok {
		t.Fatal("PR verification must include generated reproducibility")
	}
	for _, want := range []string{
		"name: Root source verification",
		"name: Codex generated reproducibility",
		"uses: ./.github/workflows/verify-generated.yml",
		"go mod tidy -diff",
		"go vet ./...",
		"go test -race ./...",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("PR verification missing %q", want)
		}
	}
}

func TestNativeProofObservesCleanWorktreeOnFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"verify-generated.yml", "pr-verification.yml"} {
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
	if workflowJobCount(syncText) != 1 {
		t.Fatal("protocol sync must be one linear job")
	}
	if strings.Contains(syncText, "actions/download-artifact") || strings.Contains(syncText, "restore-worktree") {
		t.Fatal("linear protocol sync must not restore cross-run worktree or candidate artifacts")
	}
	if strings.Count(syncText, "uses: ./.github/actions/codex-exec") != 1 {
		t.Fatal("real drift must invoke at most one Agent pass")
	}
	agent, ok := workflowStepByID(syncText, "codex")
	if !ok {
		t.Fatal("drift branch must invoke the Codex Agent on the same worktree")
	}
	if !strings.Contains(agent, "success()") || !strings.Contains(agent, "steps.mechanical.outputs.outcome == 'applied'") {
		t.Fatal("clean comparison and earlier failures must not invoke the Agent")
	}
	if !strings.Contains(agent, "GITHUB_TOKEN: \"\"") || !strings.Contains(agent, "GH_TOKEN: \"\"") {
		t.Fatal("Agent must not inherit repository-write tokens")
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
	if !strings.Contains(checks, "gofmt") || !strings.Contains(checks, "git") || !strings.Contains(checks, "diff --check") {
		t.Fatal("deterministic checks must run gofmt and git diff --check")
	}
	if strings.Contains(checks, "if:") {
		t.Fatal("clean comparison must still run deterministic checks")
	}
	if strings.Index(syncJob, "id: codex") > strings.Index(syncJob, "id: checks") {
		t.Fatal("deterministic checks must run after the Agent")
	}
	publish, ok := workflowStepByID(syncText, "publish")
	if !ok {
		t.Fatal("protocol sync must publish from the same run after checks")
	}
	if !strings.Contains(publish, "success()") || !strings.Contains(publish, "steps.mechanical.outputs.outcome == 'applied'") {
		t.Fatal("clean comparison and failed checks must not publish")
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
	if strings.Contains(syncText, "git rebase") {
		t.Fatal("protocol sync must not rebase after checks")
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

	publishHelper, err := os.ReadFile(filepath.Join(root, "codexsdk", "scripts", "codexsdk_publish_sync_pr.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publishHelper), "git rebase") {
		t.Fatal("publication must not rebase after checks")
	}
	mechanical, err := os.ReadFile(filepath.Join(root, "codexsdk", "scripts", "codexsdk_mechanical_sync.py"))
	if err != nil {
		t.Fatal(err)
	}
	mechanicalText := string(mechanical)
	if strings.Contains(mechanicalText, "./internal/cmd/generatedproof") || strings.Contains(mechanicalText, `"go", "test"`) {
		t.Fatal("mechanical sync must not own generatedproof or go test correctness decisions")
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
		"    uses: ./.github/workflows/example.yml\n" +
		"  publish:\n" +
		"    needs: [sync, proof]\n" +
		"    if: always()\n"
	proof, ok := workflowJobByID(yaml, "proof")
	if !ok {
		t.Fatal("expected proof job")
	}
	if !strings.Contains(proof, "uses: ./.github/workflows/example.yml") {
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

func TestReleasePublishesRootVersion(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	text := readWorkflow(t, root, "release.yml")
	if !strings.Contains(text, "inputs.version") {
		t.Fatal("release dispatch must accept a root version")
	}
	if !strings.Contains(text, "tag=$VERSION") {
		t.Fatal("release must tag the dispatched version")
	}
	if !strings.Contains(text, "./codexsdk/internal/cmd/generatedproof") && !strings.Contains(text, "./internal/cmd/generatedproof") {
		t.Fatal("release verification must reuse native generated-artifact proof")
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
	release, err := os.ReadFile(filepath.Join(root, "docs", "release.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(release), "version=vX.Y.Z") {
		t.Fatal("docs/release.md must document the root version dispatch")
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
				writeFile(t, root, "go.mod", "module "+rootModulePath+"\n\ngo 1.26.0\n\nrequire example.com/alias v0.0.0\nreplace example.com/alias => ./codexsdk\n")
			},
			want: "root module contains prohibited replace example.com/alias => ./codexsdk",
		},
		{
			name: "excluded module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", "module "+rootModulePath+"\n\ngo 1.26.0\n\nexclude example.com/alias v1.0.0\n")
			},
			want: "root module contains prohibited exclude example.com/alias@v1.0.0",
		},
		{
			name: "sibling versioned require",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", "module "+rootModulePath+"\n\ngo 1.26.0\n\nrequire "+llmkitPath+" v0.13.0\n")
			},
			want: "root module requires sibling versioned module " + llmkitPath,
		},
		{
			name: "nested module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "shared/go.mod", "module example.com/shared\n\ngo 1.26.0\n")
			},
			want: "nested Go module shared is not allowed",
		},
		{
			name: "workspace file",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.work", "go 1.26.0\n\nuse .\n")
			},
			want: "repository must not contain go.work",
		},
		{
			name: "wrong go version",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", "module "+rootModulePath+"\n\ngo 1.25.0\n")
			},
			want: "root module go version is 1.25.0, want 1.26.0",
		},
		{
			name: "wrong module path",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.mod", "module example.com/facade\n\ngo 1.26.0\n")
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

func newArchitectureFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+rootModulePath+"\n\ngo 1.26.0\n")
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
