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
