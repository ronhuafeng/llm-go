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
	if !strings.Contains(agent, "success()") {
		t.Fatal("earlier deterministic failures must prevent Agent invocation")
	}
	if !strings.Contains(agent, "GITHUB_TOKEN: \"\\"") || !strings.Contains(agent, "GH_TOKEN: \"\\"") {
		t.Fatal("Agent must not inherit repository-write tokens")
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

	publish, ok := workflowStepByID(syncText, "publish")
	if !ok {
		t.Fatal("protocol sync must expose a distinct publication effect")
	}
	if !strings.Contains(publish, "success()") {
		t.Fatal("failed deterministic proof must not publish")
	}
	if !strings.Contains(publish, "inputs.validation_only != true") {
		t.Fatal("validation-only protocol proof must not publish")
	}
	if strings.Contains(publish, "always()") || strings.Contains(publish, "continue-on-error") {
		t.Fatal("publication must fail closed")
	}
	if strings.Contains(publish, "git rebase") {
		t.Fatal("publication must not change the proven base by rebasing")
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
