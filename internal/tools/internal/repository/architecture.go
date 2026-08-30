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

func verifyArchitecture(root string, registered *registry) []string {
	var violations []string
	violations = append(violations, verifyOrchestrationRoot(root)...)
	violations = append(violations, verifyRegisteredModules(root, registered)...)
	violations = append(violations, verifyWorkspace(root, *registered)...)
	violations = append(violations, verifyModuleGraph(root, *registered)...)
	violations = append(violations, verifyDocumentLayout(root, *registered)...)
	return violations
}

func verifyDocumentLayout(root string, registered registry) []string {
	var violations []string
	required := []string{
		"NORTHSTAR.md",
		"DESIGN.md",
		"AGENTS.md",
		"docs/verify.md",
		"docs/release.md",
		"docs/issues.md",
	}
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			violations = append(violations, fmt.Sprintf("missing required document %s", rel))
		}
	}
	forbidden := []string{
		"CONTEXT-MAP.md",
		"docs/verification.md",
		"docs/releasing.md",
		"docs/coding-agent-guide.md",
	}
	for _, rel := range forbidden {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			violations = append(violations, fmt.Sprintf("forbidden leftover document %s", rel))
		}
	}
	for _, dir := range []string{"docs/architecture", "docs/agents"} {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir))); err == nil && info.IsDir() {
			violations = append(violations, fmt.Sprintf("forbidden leftover directory %s", dir))
		}
	}
	for _, candidate := range registered.Modules {
		if !candidate.Published {
			continue
		}
		for _, name := range []string{"CONTEXT.md", "README.md", "CHANGELOG.md", "UPGRADE.md"} {
			rel := filepath.ToSlash(filepath.Join(candidate.Dir, name))
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				violations = append(violations, fmt.Sprintf("module %s missing required document %s", candidate.ID, name))
			}
		}
		for _, rel := range []string{
			filepath.ToSlash(filepath.Join(candidate.Dir, "docs/release.md")),
			filepath.ToSlash(filepath.Join(candidate.Dir, "docs/agents")),
			filepath.ToSlash(filepath.Join(candidate.Dir, "CONTRIBUTING.md")),
			filepath.ToSlash(filepath.Join(candidate.Dir, "SUPPORT.md")),
			filepath.ToSlash(filepath.Join(candidate.Dir, "CODE_OF_CONDUCT.md")),
			filepath.ToSlash(filepath.Join(candidate.Dir, "SECURITY.md")),
			filepath.ToSlash(filepath.Join(candidate.Dir, "AGENTS.md")),
		} {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
				violations = append(violations, fmt.Sprintf("module %s has leftover document %s", candidate.ID, rel))
			}
		}
	}
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

func verifyRegisteredModules(root string, registered *registry) []string {
	var violations []string
	wantDirs := make(map[string]bool, len(registered.Modules))
	for index := range registered.Modules {
		candidate := &registered.Modules[index]
		wantDirs[candidate.Dir] = true
		metadata, err := parseGoMod(filepath.Join(root, filepath.FromSlash(candidate.Dir), "go.mod"))
		if err != nil {
			violations = append(violations, fmt.Sprintf("module %s: %v", candidate.ID, err))
			continue
		}
		candidate.path = metadata.path
		candidate.goVersion = metadata.goVersion
		candidate.requires = metadata.requires
		candidate.requireVersions = metadata.requireVersions
		candidate.replaces = metadata.replaces
		candidate.excludes = metadata.excludes
	}

	seenPaths := map[string]string{}
	for _, candidate := range registered.Modules {
		if candidate.path == "" {
			continue
		}
		if prior, exists := seenPaths[candidate.path]; exists {
			violations = append(violations, fmt.Sprintf("modules %s and %s declare duplicate path %s", prior, candidate.ID, candidate.path))
		} else {
			seenPaths[candidate.path] = candidate.ID
		}
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".git" {
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
		if !wantDirs[relative] {
			violations = append(violations, fmt.Sprintf("unregistered Go module at %s", relative))
		}
		return nil
	})
	if err != nil {
		violations = append(violations, fmt.Sprintf("discover Go modules: %v", err))
	}
	return violations
}

func verifyWorkspace(root string, registered registry) []string {
	uses, err := parseGoWork(filepath.Join(root, "go.work"))
	if err != nil {
		return []string{err.Error()}
	}
	want := make(map[string]bool, len(registered.Modules))
	for _, candidate := range registered.Modules {
		want[candidate.Dir] = true
	}
	got := make(map[string]bool, len(uses))
	var violations []string
	for _, use := range uses {
		if got[use] {
			violations = append(violations, fmt.Sprintf("go.work contains duplicate use %s", use))
		}
		got[use] = true
		if !want[use] {
			violations = append(violations, fmt.Sprintf("go.work contains unregistered module %s", use))
		}
	}
	for directory := range want {
		if !got[directory] {
			violations = append(violations, fmt.Sprintf("go.work is missing registered module %s", directory))
		}
	}
	return violations
}

func verifyModuleGraph(root string, registered registry) []string {
	owners := make(map[string]string, len(registered.Modules))
	modulesByID := make(map[string]module, len(registered.Modules))
	for _, candidate := range registered.Modules {
		modulesByID[candidate.ID] = candidate
		if candidate.path != "" {
			owners[candidate.path] = candidate.ID
		}
	}

	var violations []string
	for _, candidate := range registered.Modules {
		policy := modulePolicies[candidate.ID]
		for _, replacement := range candidate.replaces {
			oldModule := replacement.oldPath
			if replacement.oldVersion != "" {
				oldModule += "@" + replacement.oldVersion
			}
			newModule := replacement.newPath
			if replacement.newVersion != "" {
				newModule += "@" + replacement.newVersion
			}
			violations = append(violations, fmt.Sprintf("module %s contains prohibited replace %s => %s", candidate.ID, oldModule, newModule))
		}
		for _, excluded := range candidate.excludes {
			violations = append(violations, fmt.Sprintf("module %s contains prohibited exclude %s", candidate.ID, excluded))
		}
		for _, required := range candidate.requires {
			if target := ownerForImport(required, owners); target != "" && !policy.allowed[target] {
				violations = append(violations, fmt.Sprintf("module %s requires forbidden repository module %s", candidate.ID, target))
			}
		}

		moduleRoot := filepath.Join(root, filepath.FromSlash(candidate.Dir))
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
				if target != "" && !policy.allowed[target] {
					relative, _ := filepath.Rel(root, path)
					violations = append(violations, fmt.Sprintf("module %s file %s imports forbidden repository module %s", candidate.ID, filepath.ToSlash(relative), target))
				}
			}
			return nil
		})
		if err != nil {
			violations = append(violations, fmt.Sprintf("scan module %s imports: %v", candidate.ID, err))
		}
	}
	violations = append(violations, verifyAdapterUpstreamRequirements(modulesByID)...)
	return violations
}

func verifyAdapterUpstreamRequirements(modulesByID map[string]module) []string {
	adapter, ok := modulesByID["codex-adapter"]
	if !ok || adapter.path == "" {
		return nil
	}
	var violations []string
	for _, upstreamID := range []string{"llmkit", "codexsdk"} {
		upstream, ok := modulesByID[upstreamID]
		if !ok || upstream.path == "" {
			continue
		}
		version, ok := adapter.requireVersions[upstream.path]
		if !ok {
			violations = append(violations, fmt.Sprintf("module codex-adapter must directly require repository module %s", upstreamID))
			continue
		}
		if !isStableVersion(version) {
			violations = append(violations, fmt.Sprintf("module codex-adapter requires repository module %s at non-stable version %q", upstreamID, version))
		}
	}
	return violations
}

func ownerForImport(importPath string, owners map[string]string) string {
	type match struct {
		path string
		id   string
	}
	var matches []match
	for modulePath, id := range owners {
		if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
			matches = append(matches, match{path: modulePath, id: id})
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool { return len(matches[i].path) > len(matches[j].path) })
	return matches[0].id
}

type moduleMetadata struct {
	path            string
	goVersion       string
	requires        []string
	requireVersions map[string]string
	replaces        []moduleReplacement
	excludes        []string
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
