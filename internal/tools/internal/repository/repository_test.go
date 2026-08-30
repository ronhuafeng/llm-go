package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentRepositoryContract(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(root); err != nil {
		t.Fatal(err)
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
				writeFile(t, root, "llmkit/forbidden.go", "package llmkit\nimport _ \"example.com/codexsdk\"\n")
			},
			want: "module llmkit file llmkit/forbidden.go imports forbidden repository module codexsdk",
		},
		{
			name: "sdk imports toolkit",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "codexsdk/forbidden.go", "package codexsdk\nimport _ \"example.com/llmkit\"\n")
			},
			want: "module codexsdk file codexsdk/forbidden.go imports forbidden repository module llmkit",
		},
		{
			name: "public module imports repository tools",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/forbidden.go", "package codex\nimport _ \"example.com/llm-go/internal/tools/helper\"\n")
			},
			want: "module codex-adapter file llmcaller/codex/forbidden.go imports forbidden repository module repo-tools",
		},
		{
			name: "toolkit requires sdk",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/go.mod", "module example.com/llmkit\n\ngo 1.23.0\n\nrequire example.com/codexsdk v0.0.0\n")
			},
			want: "module llmkit requires forbidden repository module codexsdk",
		},
		{
			name: "module omits minimum Go version",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/go.mod", "module example.com/llmkit\n")
			},
			want: "module llmkit: go.mod has no go directive",
		},
		{
			name: "local replacement",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmkit/go.mod", "module example.com/llmkit\n\ngo 1.23.0\n\nrequire example.com/alias v0.0.0\nreplace example.com/alias => ../codexsdk\n")
			},
			want: "module llmkit contains prohibited replace example.com/alias => ../codexsdk",
		},
		{
			name: "version replacement",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "codexsdk/go.mod", "module example.com/codexsdk\n\ngo 1.23.0\n\nrequire example.com/alias v1.0.0\nreplace example.com/alias v1.0.0 => example.com/other v1.0.1\n")
			},
			want: "module codexsdk contains prohibited replace example.com/alias@v1.0.0 => example.com/other@v1.0.1",
		},
		{
			name: "excluded module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/go.mod", "module example.com/llmcaller/codex\n\ngo 1.23.0\n\nexclude example.com/alias v1.0.0\n")
			},
			want: "module codex-adapter contains prohibited exclude example.com/alias@v1.0.0",
		},
		{
			name: "adapter omits toolkit",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/go.mod", "module example.com/llmcaller/codex\n\ngo 1.23.0\n\nrequire example.com/codexsdk v0.6.0\n")
			},
			want: "module codex-adapter must directly require repository module llmkit",
		},
		{
			name: "adapter uses pseudo-version",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/go.mod", "module example.com/llmcaller/codex\n\ngo 1.23.0\n\nrequire (\n\texample.com/llmkit v0.6.1-0.20260715000000-0123456789ab\n\texample.com/codexsdk v0.6.0\n)\n")
			},
			want: "module codex-adapter requires repository module llmkit at non-stable version",
		},
		{
			name: "adapter uses prerelease",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "llmcaller/codex/go.mod", "module example.com/llmcaller/codex\n\ngo 1.23.0\n\nrequire (\n\texample.com/llmkit v0.6.0\n\texample.com/codexsdk v0.7.0-rc.1\n)\n")
			},
			want: "module codex-adapter requires repository module codexsdk at non-stable version",
		},
		{
			name: "unregistered module",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "shared/go.mod", "module example.com/shared\n\ngo 1.23.0\n")
			},
			want: "unregistered Go module at shared",
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
			name: "workspace omission",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "go.work", "go 1.23.0\n\nuse (\n\t./llmkit\n\t./codexsdk\n\t./llmcaller/codex\n)\n")
			},
			want: "go.work is missing registered module internal/tools",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newArchitectureFixture(t)
			test.mutate(t, root)
			registered, err := loadRegistry(root)
			if err != nil {
				t.Fatal(err)
			}
			violations := strings.Join(verifyArchitecture(root, &registered), "\n")
			if !strings.Contains(violations, test.want) {
				t.Fatalf("violations %q do not contain %q", violations, test.want)
			}
		})
	}
}

func TestDocumentLayoutRejectsMissingAndLeftoverDocs(t *testing.T) {
	root := newArchitectureFixture(t)
	registered, err := loadRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	violations := strings.Join(verifyArchitecture(root, &registered), "\n")
	for _, want := range []string{
		"missing required document NORTHSTAR.md",
		"module llmkit missing required document CONTEXT.md",
	} {
		if !strings.Contains(violations, want) {
			t.Fatalf("violations %q do not contain %q", violations, want)
		}
	}
	writeFile(t, root, "CONTEXT-MAP.md", "# leftover\n")
	writeFile(t, root, "NORTHSTAR.md", "# northstar\n")
	writeFile(t, root, "DESIGN.md", "# design\n")
	writeFile(t, root, "AGENTS.md", "# agents\n")
	writeFile(t, root, "docs/verify.md", "# verify\n")
	writeFile(t, root, "docs/release.md", "# release\n")
	for _, directory := range []string{"llmkit", "codexsdk", "llmcaller/codex"} {
		writeFile(t, root, filepath.Join(directory, "CONTEXT.md"), "# ctx\n")
		writeFile(t, root, filepath.Join(directory, "README.md"), "# readme\n")
		writeFile(t, root, filepath.Join(directory, "CHANGELOG.md"), "# log\n")
	}
	registered, err = loadRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	violations = strings.Join(verifyArchitecture(root, &registered), "\n")
	if !strings.Contains(violations, "forbidden leftover document CONTEXT-MAP.md") {
		t.Fatalf("violations %q do not contain leftover CONTEXT-MAP.md", violations)
	}
}

func TestRegistryRejectsMirroredModuleFacts(t *testing.T) {
	root := newArchitectureFixture(t)
	writeFile(t, root, registryFilename, `{
  "format_version": 1,
  "modules": [
    {"id":"llmkit","dir":"llmkit","published":true,"path":"example.com/llmkit"},
    {"id":"codexsdk","dir":"codexsdk","published":true},
    {"id":"codex-adapter","dir":"llmcaller/codex","published":true},
    {"id":"repo-tools","dir":"internal/tools","published":false}
  ]
}`)
	_, err := loadRegistry(root)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("loadRegistry error = %v, want unknown-field rejection", err)
	}
}

func newArchitectureFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, registryFilename, `{
  "format_version": 1,
  "modules": [
    {"id":"llmkit","dir":"llmkit","published":true},
    {"id":"codexsdk","dir":"codexsdk","published":true},
    {"id":"codex-adapter","dir":"llmcaller/codex","published":true},
    {"id":"repo-tools","dir":"internal/tools","published":false}
  ]
}`)
	writeFile(t, root, "go.work", "go 1.23.0\n\nuse (\n\t./llmkit\n\t./codexsdk\n\t./llmcaller/codex\n\t./internal/tools\n)\n")
	modules := map[string]string{
		"llmkit":          "example.com/llmkit",
		"codexsdk":        "example.com/codexsdk",
		"llmcaller/codex": "example.com/llmcaller/codex",
		"internal/tools":  "example.com/llm-go/internal/tools",
	}
	for directory, modulePath := range modules {
		contents := fmt.Sprintf("module %s\n\ngo 1.23.0\n", modulePath)
		if directory == "llmcaller/codex" {
			contents += "\nrequire (\n\texample.com/llmkit v0.6.0\n\texample.com/codexsdk v0.6.0\n)\n"
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
