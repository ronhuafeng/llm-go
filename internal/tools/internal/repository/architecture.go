package repository

import (
	"bufio"
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
	llmkitPath  = "github.com/ronhuafeng/llm-go/llmkit"
	codexSDKPath = "github.com/ronhuafeng/llm-go/codexsdk"
	adapterPath = "github.com/ronhuafeng/llm-go/llmcaller/codex"
	toolsPath   = "github.com/ronhuafeng/llm-go/internal/tools"
)

type workspaceModule struct {
	dir      string
	metadata moduleMetadata
}

type moduleMetadata struct {
	path            string
	goVersion       string
	requires        []string
	requireVersions map[string]string
	replaces        []moduleReplacement
	excludes        []string
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

	uses, err := parseGoWork(filepath.Join(root, "go.work"))
	if err != nil {
		return append(violations, err.Error())
	}
	useSet := make(map[string]bool, len(uses))
	for _, use := range uses {
		if useSet[use] {
			violations = append(violations, fmt.Sprintf("go.work contains duplicate use %s", use))
		}
		useSet[use] = true
	}
	violations = append(violations, verifyEveryModuleIsInWorkspace(root, useSet)...)

	modules := make([]workspaceModule, 0, len(uses))
	owners := make(map[string]string, len(uses))
	for _, dir := range uses {
		metadata, err := parseGoMod(filepath.Join(root, filepath.FromSlash(dir), "go.mod"))
		if err != nil {
			violations = append(violations, fmt.Sprintf("module %s: %v", dir, err))
			continue
		}
		if want := expectedModuleDir(metadata.path); want == "" {
			violations = append(violations, fmt.Sprintf("workspace module %s has unsupported repository module path %s", dir, metadata.path))
		} else if dir != want {
			violations = append(violations, fmt.Sprintf("module %s must live at %s, got %s", moduleLabel(metadata.path), want, dir))
		}
		if prior, exists := owners[metadata.path]; exists {
			violations = append(violations, fmt.Sprintf("workspace modules %s and %s declare duplicate path %s", prior, dir, metadata.path))
		} else {
			owners[metadata.path] = dir
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
			violations = append(violations, fmt.Sprintf("module %s contains prohibited replace %s => %s", moduleLabel(metadata.path), oldModule, newModule))
		}
		for _, excluded := range metadata.excludes {
			violations = append(violations, fmt.Sprintf("module %s contains prohibited exclude %s", moduleLabel(metadata.path), excluded))
		}
		modules = append(modules, workspaceModule{dir: dir, metadata: metadata})
	}

	for _, candidate := range modules {
		for _, required := range candidate.metadata.requires {
			if target := ownerForImport(required, owners); target != "" && !allowedRepositoryDependency(candidate.metadata.path, target) {
				violations = append(violations, fmt.Sprintf("module %s requires forbidden repository module %s", moduleLabel(candidate.metadata.path), moduleLabel(target)))
			}
		}
		violations = append(violations, verifyModuleImports(root, candidate, owners)...)
	}
	violations = append(violations, verifyAdapterUpstreamRequirements(modules)...)
	return violations
}

func verifyOrchestrationRoot(root string) []string {
	var violations []string
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		violations = append(violations, "repository root must not contain go.mod")
	} else if !os.IsNotExist(err) {
		violations = append(violations, fmt.Sprintf("inspect root go.mod: %v", err))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return append(violations, fmt.Sprintf("read repository root: %v", err))
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			violations = append(violations, fmt.Sprintf("repository root Go file %s would create a root facade", entry.Name()))
		}
	}
	return violations
}

func verifyEveryModuleIsInWorkspace(root string, uses map[string]bool) []string {
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}
		if !uses[relative] {
			violations = append(violations, fmt.Sprintf("Go module %s is not listed in go.work", relative))
		}
		return nil
	})
	if err != nil {
		violations = append(violations, fmt.Sprintf("discover Go modules: %v", err))
	}
	return violations
}

func verifyModuleImports(root string, candidate workspaceModule, owners map[string]string) []string {
	var violations []string
	moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.dir))
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports in %s: %w", path, err)
		}
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("decode import in %s: %w", path, err)
			}
			target := ownerForImport(importPath, owners)
			if target != "" && !allowedRepositoryDependency(candidate.metadata.path, target) {
				relative, _ := filepath.Rel(root, path)
				violations = append(violations, fmt.Sprintf("module %s file %s imports forbidden repository module %s", moduleLabel(candidate.metadata.path), filepath.ToSlash(relative), moduleLabel(target)))
			}
		}
		return nil
	})
	if err != nil {
		violations = append(violations, fmt.Sprintf("scan module %s imports: %v", moduleLabel(candidate.metadata.path), err))
	}
	return violations
}

func verifyAdapterUpstreamRequirements(modules []workspaceModule) []string {
	var adapter *workspaceModule
	for index := range modules {
		if modules[index].metadata.path == adapterPath {
			adapter = &modules[index]
			break
		}
	}
	if adapter == nil {
		return []string{"go.work is missing the codex adapter module"}
	}
	var violations []string
	for _, upstream := range []string{llmkitPath, codexSDKPath} {
		version, ok := adapter.metadata.requireVersions[upstream]
		if !ok {
			violations = append(violations, fmt.Sprintf("module codex-adapter must directly require repository module %s", moduleLabel(upstream)))
			continue
		}
		if !isStableVersion(version) {
			violations = append(violations, fmt.Sprintf("module codex-adapter requires repository module %s at non-stable version %q", moduleLabel(upstream), version))
		}
	}
	return violations
}

func expectedModuleDir(modulePath string) string {
	switch modulePath {
	case llmkitPath:
		return "llmkit"
	case codexSDKPath:
		return "codexsdk"
	case adapterPath:
		return "llmcaller/codex"
	case toolsPath:
		return "internal/tools"
	default:
		return ""
	}
}

func moduleLabel(modulePath string) string {
	switch modulePath {
	case llmkitPath:
		return "llmkit"
	case codexSDKPath:
		return "codexsdk"
	case adapterPath:
		return "codex-adapter"
	case toolsPath:
		return "repo-tools"
	default:
		return modulePath
	}
}

func allowedRepositoryDependency(source, target string) bool {
	switch source {
	case llmkitPath:
		return target == llmkitPath
	case codexSDKPath:
		return target == codexSDKPath
	case adapterPath:
		return target == adapterPath || target == llmkitPath || target == codexSDKPath
	case toolsPath:
		return true
	default:
		return false
	}
}

func ownerForImport(importPath string, owners map[string]string) string {
	var matches []string
	for modulePath := range owners {
		if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
			matches = append(matches, modulePath)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool { return len(matches[i]) > len(matches[j]) })
	return matches[0]
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
	metadata := moduleMetadata{path: parsed.Module.Mod.Path, requireVersions: map[string]string{}}
	if parsed.Go == nil || parsed.Go.Version == "" {
		return moduleMetadata{}, fmt.Errorf("go.mod has no go directive")
	}
	metadata.goVersion = parsed.Go.Version
	for _, required := range parsed.Require {
		metadata.requires = append(metadata.requires, required.Mod.Path)
		metadata.requireVersions[required.Mod.Path] = required.Mod.Version
	}
	for _, replacement := range parsed.Replace {
		metadata.replaces = append(metadata.replaces, moduleReplacement{
			oldPath: replacement.Old.Path, oldVersion: replacement.Old.Version,
			newPath: replacement.New.Path, newVersion: replacement.New.Version,
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

func parseGoWork(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read go.work: %w", err)
	}
	defer file.Close()
	var uses []string
	inUseBlock := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "//", 2)[0])
		if line == "" {
			continue
		}
		if inUseBlock {
			if line == ")" {
				inUseBlock = false
				continue
			}
			uses = append(uses, normalizeWorkspaceUse(line))
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "use" && fields[1] == "(" {
			inUseBlock = true
			continue
		}
		if len(fields) == 2 && fields[0] == "use" {
			uses = append(uses, normalizeWorkspaceUse(fields[1]))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read go.work: %w", err)
	}
	if inUseBlock {
		return nil, fmt.Errorf("read go.work: unterminated use block")
	}
	return uses, nil
}

func normalizeWorkspaceUse(value string) string {
	value = unquoteToken(strings.Fields(value)[0])
	value = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(value)), "./")
	return value
}

func unquoteToken(value string) string {
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}
