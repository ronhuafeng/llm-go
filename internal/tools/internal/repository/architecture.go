package repository

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

const (
	rootModulePath = "github.com/ronhuafeng/llm-go"
	requiredGo     = "1.25.0"
	llmkitPath     = "github.com/ronhuafeng/llm-go/llmkit"
	codexSDKPath   = "github.com/ronhuafeng/llm-go/codexsdk"
	adapterPath    = "github.com/ronhuafeng/llm-go/llmcaller/codex"
)

type moduleMetadata struct {
	path      string
	goVersion string
	requires  []string
	replaces  []moduleReplacement
	excludes  []string
}

type moduleReplacement struct {
	oldPath    string
	oldVersion string
	newPath    string
	newVersion string
}

func verifyArchitecture(root string) []string {
	var violations []string
	violations = append(violations, verifyOrchestrationRoot(root)...)
	violations = append(violations, verifyNoWorkspace(root)...)
	violations = append(violations, verifySingleRootModule(root)...)
	violations = append(violations, verifyPackageFamiliesExist(root)...)
	violations = append(violations, verifyPackageImports(root)...)
	return violations
}

func verifyOrchestrationRoot(root string) []string {
	var violations []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return []string{fmt.Sprintf("read repository root: %v", err)}
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			violations = append(violations, fmt.Sprintf("repository root Go file %s would create a root facade", entry.Name()))
		}
	}
	return violations
}

func verifyNoWorkspace(root string) []string {
	var violations []string
	for _, name := range []string{"go.work", "go.work.sum"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			violations = append(violations, fmt.Sprintf("repository must not contain %s", name))
		} else if !os.IsNotExist(err) {
			violations = append(violations, fmt.Sprintf("inspect %s: %v", name, err))
		}
	}
	return violations
}

func verifySingleRootModule(root string) []string {
	var violations []string
	metadata, err := parseGoMod(filepath.Join(root, "go.mod"))
	if err != nil {
		return []string{fmt.Sprintf("root module: %v", err)}
	}
	if metadata.path != rootModulePath {
		violations = append(violations, fmt.Sprintf("root module path is %s, want %s", metadata.path, rootModulePath))
	}
	if metadata.goVersion != requiredGo {
		violations = append(violations, fmt.Sprintf("root module go version is %s, want %s", metadata.goVersion, requiredGo))
	}
	for _, required := range metadata.requires {
		if required == llmkitPath || required == codexSDKPath || required == adapterPath || strings.HasPrefix(required, llmkitPath+"/") || strings.HasPrefix(required, codexSDKPath+"/") || strings.HasPrefix(required, adapterPath+"/") {
			violations = append(violations, fmt.Sprintf("root module requires sibling versioned module %s", required))
		}
	}
	for _, replacement := range metadata.replaces {
		oldModule := replacement.oldPath
		if replacement.oldVersion != "" {
			oldModule += "@" + replacement.oldVersion
		}
		newModule := replacement.newPath
		if replacement.newVersion != "" {
			newModule += "@" + replacement.newVersion
		}
		violations = append(violations, fmt.Sprintf("root module contains prohibited replace %s => %s", oldModule, newModule))
	}
	for _, excluded := range metadata.excludes {
		violations = append(violations, fmt.Sprintf("root module contains prohibited exclude %s", excluded))
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Name() != "go.mod" {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) != "." {
			violations = append(violations, fmt.Sprintf("nested Go module %s is not allowed", filepath.ToSlash(relative)))
		}
		return nil
	})
	if err != nil {
		violations = append(violations, fmt.Sprintf("discover Go modules: %v", err))
	}
	return violations
}

func verifyPackageFamiliesExist(root string) []string {
	var violations []string
	for _, rel := range []string{"llmkit", "codexsdk", "llmcaller/codex"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			violations = append(violations, fmt.Sprintf("missing package family directory %s", rel))
		}
	}
	return violations
}

func verifyPackageImports(root string) []string {
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		source := packageFamilyOf(relative)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports in %s: %w", path, err)
		}
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("decode import in %s: %w", path, err)
			}
			if source != "repository" && strings.HasPrefix(importPath, rootModulePath+"/internal/") {
				violations = append(violations, fmt.Sprintf("%s file %s imports forbidden repository internal package %s", source, relative, importPath))
			}
			target := packageFamilyOfImport(importPath)
			if target != "" && !allowedPackageFamilyDependency(source, target) {
				violations = append(violations, fmt.Sprintf("%s file %s imports forbidden package family %s", source, relative, target))
			}
		}
		return nil
	})
	if err != nil {
		violations = append(violations, fmt.Sprintf("scan package imports: %v", err))
	}
	return violations
}

func packageFamilyOf(relativePath string) string {
	switch {
	case relativePath == "llmkit" || strings.HasPrefix(relativePath, "llmkit/"):
		return "llmkit"
	case relativePath == "codexsdk" || strings.HasPrefix(relativePath, "codexsdk/"):
		return "codexsdk"
	case relativePath == "llmcaller/codex" || strings.HasPrefix(relativePath, "llmcaller/codex/"):
		return "codex-adapter"
	default:
		return "repository"
	}
}

func packageFamilyOfImport(importPath string) string {
	families := []string{adapterPath, llmkitPath, codexSDKPath}
	sort.Slice(families, func(i, j int) bool { return len(families[i]) > len(families[j]) })
	for _, family := range families {
		if importPath == family || strings.HasPrefix(importPath, family+"/") {
			switch family {
			case llmkitPath:
				return "llmkit"
			case codexSDKPath:
				return "codexsdk"
			case adapterPath:
				return "codex-adapter"
			}
		}
	}
	return ""
}

func allowedPackageFamilyDependency(source, target string) bool {
	switch source {
	case "llmkit":
		return target == "llmkit"
	case "codexsdk":
		return target == "codexsdk"
	case "codex-adapter":
		return target == "codex-adapter" || target == "llmkit" || target == "codexsdk"
	default:
		return true
	}
}

func parseGoMod(path string) (moduleMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return moduleMetadata{}, fmt.Errorf("read go.mod: %w", err)
	}
	parsed, err := modfile.Parse(path, data, nil)
	if err != nil {
		return moduleMetadata{}, fmt.Errorf("parse go.mod: %w", err)
	}
	if parsed.Module == nil || parsed.Module.Mod.Path == "" {
		return moduleMetadata{}, fmt.Errorf("go.mod has no module directive")
	}
	metadata := moduleMetadata{path: parsed.Module.Mod.Path}
	if parsed.Go == nil || parsed.Go.Version == "" {
		return moduleMetadata{}, fmt.Errorf("go.mod has no go directive")
	}
	metadata.goVersion = parsed.Go.Version
	for _, required := range parsed.Require {
		metadata.requires = append(metadata.requires, required.Mod.Path)
	}
	for _, replacement := range parsed.Replace {
		metadata.replaces = append(metadata.replaces, moduleReplacement{
			oldPath:    replacement.Old.Path,
			oldVersion: replacement.Old.Version,
			newPath:    replacement.New.Path,
			newVersion: replacement.New.Version,
		})
	}
	for _, excluded := range parsed.Exclude {
		value := excluded.Mod.Path
		if excluded.Mod.Version != "" {
			value += "@" + excluded.Mod.Version
		}
		metadata.excludes = append(metadata.excludes, value)
	}
	return metadata, nil
}
