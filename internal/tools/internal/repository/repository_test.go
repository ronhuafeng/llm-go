package repository

import (
	"encoding/json"
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

func TestProvenanceValidation(t *testing.T) {
	fixture := newProvenanceFixture(t)
	registered := registry{Modules: []module{{ID: "llmkit", Dir: "llmkit", Published: true}}}

	t.Run("accepts exact recorded graph", func(t *testing.T) {
		writeManifest(t, fixture.root, fixture.manifest)
		if violations := verifyProvenance(fixture.root, registered, fixture.git); len(violations) != 0 {
			t.Fatalf("verifyProvenance violations = %v", violations)
		}
	})

	tests := []struct {
		name   string
		mutate func(*provenanceFixture)
		want   string
	}{
		{
			name: "stable but wrong source tag",
			mutate: func(candidate *provenanceFixture) {
				candidate.manifest.Imports[0].Source.Tag = "v9.9.9"
			},
			want: "tag evidence names \"v0.5.0\", manifest records \"v9.9.9\"",
		},
		{
			name: "wrong source tree",
			mutate: func(candidate *provenanceFixture) {
				candidate.manifest.Imports[0].Source.Tree = strings.Repeat("9", 40)
			},
			want: "source tree is",
		},
		{
			name: "wrong relocation parent",
			mutate: func(candidate *provenanceFixture) {
				candidate.manifest.Imports[0].Relocation.Parent = strings.Repeat("8", 40)
			},
			want: "relocation parent does not equal source commit",
		},
		{
			name: "wrong merge parent",
			mutate: func(candidate *provenanceFixture) {
				candidate.manifest.Imports[0].Merge.SecondParent = strings.Repeat("7", 40)
			},
			want: "merge second parent does not equal relocation commit",
		},
		{
			name: "colliding root tag",
			mutate: func(candidate *provenanceFixture) {
				candidate.git.responses[key("tag", "--list", "v*")] = "v0.5.0"
			},
			want: "colliding root legacy tags are prohibited",
		},
		{
			name: "dangling source",
			mutate: func(candidate *provenanceFixture) {
				delete(candidate.git.responses, key("merge-base", "--is-ancestor", candidate.manifest.Imports[0].Source.Commit, "HEAD"))
			},
			want: "source commit is not an ancestor of HEAD",
		},
		{
			name: "dangling relocation",
			mutate: func(candidate *provenanceFixture) {
				delete(candidate.git.responses, key("merge-base", "--is-ancestor", candidate.manifest.Imports[0].Relocation.Commit, "HEAD"))
			},
			want: "relocation commit is not an ancestor of HEAD",
		},
		{
			name: "dangling merge",
			mutate: func(candidate *provenanceFixture) {
				delete(candidate.git.responses, key("merge-base", "--is-ancestor", candidate.manifest.Imports[0].Merge.Commit, "HEAD"))
			},
			want: "merge commit is not an ancestor of HEAD",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := fixture.clone()
			test.mutate(&candidate)
			writeManifest(t, candidate.root, candidate.manifest)
			violations := strings.Join(verifyProvenance(candidate.root, registered, candidate.git), "\n")
			if !strings.Contains(violations, test.want) {
				t.Fatalf("violations %q do not contain %q", violations, test.want)
			}
		})
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
		writeFile(t, root, filepath.Join(directory, "go.mod"), fmt.Sprintf("module %s\n\ngo 1.23.0\n", modulePath))
		writeFile(t, root, filepath.Join(directory, "package.go"), "package fixture\n")
	}
	return root
}

type fakeGit struct {
	responses map[string]string
}

func (git fakeGit) output(args ...string) (string, error) {
	response, ok := git.responses[key(args...)]
	if !ok {
		return "", fmt.Errorf("unexpected git command: %s", strings.Join(args, " "))
	}
	return response, nil
}

type provenanceFixture struct {
	root     string
	manifest provenanceManifest
	git      fakeGit
}

func (fixture provenanceFixture) clone() provenanceFixture {
	data, _ := json.Marshal(fixture.manifest)
	var clonedManifest provenanceManifest
	_ = json.Unmarshal(data, &clonedManifest)
	responses := make(map[string]string, len(fixture.git.responses))
	for command, response := range fixture.git.responses {
		responses[command] = response
	}
	return provenanceFixture{root: fixture.root, manifest: clonedManifest, git: fakeGit{responses: responses}}
}

func newProvenanceFixture(t *testing.T) provenanceFixture {
	t.Helper()
	root := t.TempDir()
	source := strings.Repeat("1", 40)
	tree := strings.Repeat("2", 40)
	relocation := strings.Repeat("3", 40)
	base := strings.Repeat("4", 40)
	merge := strings.Repeat("5", 40)
	manifest := provenanceManifest{FormatVersion: 2, Imports: []provenanceImport{{ID: "llmkit"}}}
	imported := &manifest.Imports[0]
	imported.Source.Repository = "https://example.com/llmkit-go"
	imported.Source.Tag = "v0.5.0"
	imported.Source.TagEvidence = "docs/migration/tag-objects/llmkit-v0.5.0.tag"
	imported.Source.Commit = source
	imported.Source.Tree = tree
	tagPayload := []byte("object " + source + "\ntype commit\ntag v0.5.0\ntagger Test <test@example.com> 0 +0000\n\nv0.5.0\n")
	imported.Source.TagObject = tagObjectID(tagPayload)
	writeFile(t, root, imported.Source.TagEvidence, string(tagPayload))
	imported.Destination.Directory = "llmkit"
	imported.Destination.Module = "example.com/llm-go/llmkit"
	imported.Destination.FirstTag = "llmkit/v0.6.0"
	imported.Relocation.Commit = relocation
	imported.Relocation.Parent = source
	imported.Relocation.Subtree = tree
	imported.Merge.Commit = merge
	imported.Merge.FirstParent = base
	imported.Merge.SecondParent = relocation
	responses := map[string]string{
		key("tag", "--list", "v*"):                             "",
		key("rev-parse", source+"^{tree}"):                     tree,
		key("show", "-s", "--format=%P", relocation):           source,
		key("rev-parse", relocation+":llmkit"):                 tree,
		key("ls-tree", "--name-only", relocation):              "llmkit",
		key("show", "-s", "--format=%P", merge):                base + " " + relocation,
		key("rev-parse", merge+":llmkit"):                      tree,
		key("merge-base", "--is-ancestor", source, "HEAD"):     "",
		key("merge-base", "--is-ancestor", relocation, "HEAD"): "",
		key("merge-base", "--is-ancestor", merge, "HEAD"):      "",
	}
	return provenanceFixture{root: root, manifest: manifest, git: fakeGit{responses: responses}}
}

func writeManifest(t *testing.T, root string, manifest provenanceManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, provenanceFilename, string(data))
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

func key(args ...string) string {
	return strings.Join(args, "\x00")
}
