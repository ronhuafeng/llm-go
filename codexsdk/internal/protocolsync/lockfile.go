package protocolsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// prepareLockfile projects only local workspace version identities from Cargo's
// selected-source metadata. Dependency resolution remains Cargo's locked job.
func prepareLockfile(original, metadata []byte, root string) ([]byte, error) {
	var manifest struct {
		Members  []string `json:"workspace_members"`
		Packages []struct {
			ID           string  `json:"id"`
			Name         string  `json:"name"`
			Version      string  `json:"version"`
			Source       *string `json:"source"`
			ManifestPath string  `json:"manifest_path"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(metadata, &manifest); err != nil {
		return nil, fmt.Errorf("read workspace metadata: %w", err)
	}
	members := make(map[string]bool)
	for _, id := range manifest.Members {
		members[id] = true
	}
	versions := make(map[string]string)
	for _, p := range manifest.Packages {
		if !members[p.ID] {
			continue
		}
		rel, err := filepath.Rel(root, p.ManifestPath)
		if err != nil || !filepath.IsLocal(rel) || p.Source != nil || p.Name == "" || p.Version == "" || versions[p.Name] != "" {
			return nil, fmt.Errorf("invalid local workspace identity %q", p.Name)
		}
		versions[p.Name] = p.Version
		delete(members, p.ID)
	}
	if len(members) != 0 || len(versions) == 0 {
		return nil, fmt.Errorf("incomplete Cargo workspace metadata")
	}
	var lock map[string]any
	if err := toml.Unmarshal(original, &lock); err != nil {
		return nil, fmt.Errorf("parse upstream Cargo.lock: %w", err)
	}
	packages, ok := lock["package"].([]any)
	if !ok {
		return nil, fmt.Errorf("Cargo.lock has no package array")
	}
	replacements := make(map[string]string)
	found := make(map[string]bool)
	for _, value := range packages {
		p, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid Cargo.lock package")
		}
		name, _ := p["name"].(string)
		version, valid := p["version"].(string)
		if !valid || name == "" {
			return nil, fmt.Errorf("invalid locked package identity")
		}
		if _, external := p["source"]; external || versions[name] == "" {
			continue
		}
		if found[name] {
			return nil, fmt.Errorf("ambiguous local locked package %q", name)
		}
		found[name] = true
		if version != versions[name] {
			replacements[name+" "+version] = name + " " + versions[name]
			p["version"] = versions[name]
		}
	}
	for name := range versions {
		if !found[name] {
			return nil, fmt.Errorf("workspace package %q missing from Cargo.lock", name)
		}
	}
	if len(replacements) == 0 {
		return bytes.Clone(original), nil
	}
	for _, value := range packages {
		p := value.(map[string]any)
		if _, external := p["source"]; external {
			name, version := p["name"].(string), p["version"].(string)
			_, oldCollision := replacements[name+" "+version]
			if oldCollision || versions[name] == version {
				return nil, fmt.Errorf("ambiguous external/workspace locked identity %q", p["name"])
			}
		}
		if deps, ok := p["dependencies"].([]any); ok {
			for i, dep := range deps {
				ref, ok := dep.(string)
				if !ok {
					return nil, fmt.Errorf("invalid locked dependency reference")
				}
				if replacement, ok := replacements[ref]; ok {
					deps[i] = replacement
				}
			}
		}
	}
	prepared, err := toml.Marshal(lock)
	if err != nil {
		return nil, fmt.Errorf("encode prepared Cargo.lock: %w", err)
	}
	return prepared, nil
}
