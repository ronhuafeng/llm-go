package architecture

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestHandwrittenPublicAPI(t *testing.T) {
	root := repoRoot(t)
	loader := &sourceImporter{
		root:  root,
		fset:  token.NewFileSet(),
		cache: make(map[string]*types.Package),
	}
	var declarations []string
	for _, name := range []string{"llmschema", "llmadapter", "settle", "llmstep"} {
		path := "github.com/ronhuafeng/llmkit-go/" + name
		pkg, err := loader.Import(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		declarations = append(declarations, exportedDeclarations(pkg)...)
	}
	sort.Strings(declarations)
	actual := strings.Join(declarations, "\n") + "\n"
	allowlistPath := filepath.Join(root, "internal", "architecture", "testdata", "handwritten-api.txt")
	if os.Getenv("UPDATE_HANDWRITTEN_API") == "1" {
		if err := os.WriteFile(allowlistPath, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(allowlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if actual != string(want) {
		t.Fatalf("handwritten public API changed; update the normative plan first, then review this canonical allowlist:\n%s", actual)
	}
}

type sourceImporter struct {
	root     string
	fset     *token.FileSet
	cache    map[string]*types.Package
	compiled types.Importer
}

func (i *sourceImporter) Import(path string) (*types.Package, error) {
	if pkg := i.cache[path]; pkg != nil {
		return pkg, nil
	}
	const module = "github.com/ronhuafeng/llmkit-go/"
	if !strings.HasPrefix(path, module) {
		if i.compiled == nil {
			i.compiled = importer.ForCompiler(i.fset, "gc", i.openExport)
		}
		return i.compiled.Import(path)
	}
	dir := filepath.Join(i.root, strings.TrimPrefix(path, module))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(i.fset, filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	config := types.Config{Importer: i}
	pkg, err := config.Check(path, i.fset, files, nil)
	if err != nil {
		return nil, err
	}
	i.cache[path] = pkg
	return pkg, nil
}

func (i *sourceImporter) openExport(path string) (io.ReadCloser, error) {
	command := exec.Command("go", "list", "-export", "-json", path)
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s: %w", path, err)
	}
	var listed struct {
		Export string
	}
	if err := json.Unmarshal(output, &listed); err != nil {
		return nil, fmt.Errorf("decode go list %s: %w", path, err)
	}
	return os.Open(listed.Export)
}

func exportedDeclarations(pkg *types.Package) []string {
	qualifier := func(other *types.Package) string { return other.Path() }
	var declarations []string
	for _, name := range pkg.Scope().Names() {
		object := pkg.Scope().Lookup(name)
		if !object.Exported() {
			continue
		}
		declarations = append(declarations, types.ObjectString(object, qualifier))
		typeName, ok := object.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok {
			continue
		}
		methods := types.NewMethodSet(types.NewPointer(named))
		for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
			method := methods.At(methodIndex).Obj()
			if method.Exported() {
				declarations = append(declarations, fmt.Sprintf("method %s.%s.%s%s", pkg.Path(), named.Obj().Name(), method.Name(), types.TypeString(method.Type(), qualifier)))
			}
		}
	}
	return declarations
}

func TestLLMKitImportBoundaries(t *testing.T) {
	root := repoRoot(t)
	rules := []importRule{
		{
			dir:            "settle",
			stdlibOnly:     true,
			violationLabel: "settle must remain a stdlib-only stable loop primitive",
		},
		{
			dir: "llmschema",
			forbidden: []string{
				"github.com/ronhuafeng/codexsdk-go",
				"github.com/ronhuafeng/llmcaller-codex-go",
				"smart-contract",
			},
			violationLabel: "llmschema must remain provider- and business-independent",
		},
		{
			dir: "llmadapter",
			forbidden: []string{
				"github.com/ronhuafeng/codexsdk-go",
				"github.com/ronhuafeng/llmcaller-codex-go",
				"smart-contract",
			},
			violationLabel: "llmadapter must not bind to a concrete provider SDK or business package",
		},
	}

	for _, rule := range rules {
		checkImportRule(t, root, rule)
	}
}

func TestOnlyLLMSchemaOwnsGoTypeSchemaProjection(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := relPath(root, path)
		if strings.HasPrefix(rel, "llmschema/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(raw)
		for _, token := range []string{"jsonschema.For[", "jsonschema.ForType"} {
			if strings.Contains(source, token) {
				t.Fatalf("only llmschema may implement Go type schema projection: %s contains %q", rel, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type importRule struct {
	dir            string
	stdlibOnly     bool
	forbidden      []string
	violationLabel string
}

func checkImportRule(t *testing.T, root string, rule importRule) {
	t.Helper()
	base := filepath.Join(root, filepath.FromSlash(rule.dir))
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if rule.stdlibOnly && !isStdlibImport(importPath) {
				t.Fatalf("%s: %s imports %q", rule.violationLabel, relPath(root, path), importPath)
			}
			for _, forbidden := range rule.forbidden {
				if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
					t.Fatalf("%s: %s imports %q", rule.violationLabel, relPath(root, path), importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func relPath(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func isStdlibImport(importPath string) bool {
	return !strings.Contains(importPath, ".")
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".mypy_cache", ".pytest_cache", ".ruff_cache", ".venv", "__pycache__", "build", "dist", "node_modules", "vendor":
		return true
	default:
		return false
	}
}
