package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
