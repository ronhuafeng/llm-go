package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const registryFilename = "module-registry.json"

type registry struct {
	FormatVersion int      `json:"format_version"`
	Modules       []module `json:"modules"`
}

type module struct {
	ID        string `json:"id"`
	Dir       string `json:"dir"`
	Published bool   `json:"published"`
	path      string
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

var modulePolicies = map[string]struct {
	published bool
	allowed   map[string]bool
}{
	"llmkit": {
		published: true,
		allowed:   set("llmkit"),
	},
	"codexsdk": {
		published: true,
		allowed:   set("codexsdk"),
	},
	"codex-adapter": {
		published: true,
		allowed:   set("codex-adapter", "llmkit", "codexsdk"),
	},
	"repo-tools": {
		published: false,
		allowed:   set("repo-tools", "codex-adapter", "llmkit", "codexsdk"),
	},
}

func set(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func loadRegistry(root string) (registry, error) {
	data, err := os.ReadFile(filepath.Join(root, registryFilename))
	if err != nil {
		return registry{}, fmt.Errorf("read %s: %w", registryFilename, err)
	}

	var got registry
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		return registry{}, fmt.Errorf("decode %s: %w", registryFilename, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return registry{}, fmt.Errorf("decode %s: trailing JSON value", registryFilename)
		}
		return registry{}, fmt.Errorf("decode %s: %w", registryFilename, err)
	}
	if got.FormatVersion != 1 {
		return registry{}, fmt.Errorf("%s: unsupported format_version %d", registryFilename, got.FormatVersion)
	}
	if len(got.Modules) != len(modulePolicies) {
		return registry{}, fmt.Errorf("%s: got %d modules, want exactly %d", registryFilename, len(got.Modules), len(modulePolicies))
	}

	ids := make(map[string]bool, len(got.Modules))
	dirs := make(map[string]bool, len(got.Modules))
	for index := range got.Modules {
		candidate := &got.Modules[index]
		policy, ok := modulePolicies[candidate.ID]
		if !ok {
			return registry{}, fmt.Errorf("%s: unknown module id %q", registryFilename, candidate.ID)
		}
		if ids[candidate.ID] {
			return registry{}, fmt.Errorf("%s: duplicate module id %q", registryFilename, candidate.ID)
		}
		ids[candidate.ID] = true
		if candidate.Published != policy.published {
			return registry{}, fmt.Errorf("%s: module %q published=%t, want %t", registryFilename, candidate.ID, candidate.Published, policy.published)
		}

		cleanDir := filepath.ToSlash(filepath.Clean(candidate.Dir))
		if candidate.Dir == "" || cleanDir != candidate.Dir || filepath.IsAbs(candidate.Dir) || candidate.Dir == "." || cleanDir == ".." || len(cleanDir) > 3 && cleanDir[:3] == "../" {
			return registry{}, fmt.Errorf("%s: module %q has invalid directory %q", registryFilename, candidate.ID, candidate.Dir)
		}
		if dirs[candidate.Dir] {
			return registry{}, fmt.Errorf("%s: duplicate module directory %q", registryFilename, candidate.Dir)
		}
		dirs[candidate.Dir] = true
	}

	for id := range modulePolicies {
		if !ids[id] {
			return registry{}, fmt.Errorf("%s: missing module id %q", registryFilename, id)
		}
	}
	for _, candidate := range got.Modules {
		if candidate.ID == "repo-tools" && candidate.Dir != "internal/tools" {
			return registry{}, fmt.Errorf("%s: repo-tools must live in internal/tools", registryFilename)
		}
	}

	sort.Slice(got.Modules, func(i, j int) bool { return got.Modules[i].ID < got.Modules[j].ID })
	return got, nil
}
