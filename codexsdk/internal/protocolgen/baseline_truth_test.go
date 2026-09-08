package protocolgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var sourceCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestCheckedInBaselineMetadataMatchesSchemaSet(t *testing.T) {
	root := filepath.Join("..", "protocolschema", "appserver", "v2")
	raw, err := os.ReadFile(filepath.Join(root, "baseline_metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		SchemaFileCount int    `json:"schema_file_count"`
		SourceCommit    string `json:"source_commit"`
		SourceRefKind   string `json:"source_ref_kind"`
		SourceRefName   string `json:"source_ref_name"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if !sourceCommitPattern.MatchString(metadata.SourceCommit) {
		t.Errorf("source_commit = %q, want 40 lowercase hex characters", metadata.SourceCommit)
	}
	if metadata.SourceRefName == "" {
		t.Error("source_ref_name is empty")
	}
	switch metadata.SourceRefKind {
	case "stable_rust_tag", "manual_ref", "manual_commit":
	default:
		t.Errorf("source_ref_kind = %q, want canonical upstream identity kind", metadata.SourceRefKind)
	}

	metadataFiles := map[string]bool{
		"baseline_metadata.json":      true,
		"coverage_matrix.json":        true,
		"drift_report.json":           true,
		"manifest.json":               true,
		"manifest_generation.json":    true,
		"matrix_update_skeleton.json": true,
	}
	count := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || metadataFiles[entry.Name()] {
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != metadata.SchemaFileCount {
		t.Errorf("schema_file_count = %d, actual checked-in schema files = %d", metadata.SchemaFileCount, count)
	}
}

func TestCheckedInManifestReferencesExistingSchemas(t *testing.T) {
	root := filepath.Join("..", "protocolschema", "appserver", "v2")
	manifest, err := LoadManifest(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Entries {
		for label, rel := range map[string]string{
			"source_schema":   entry.SourceSchema,
			"response_schema": entry.ResponseSchema,
		} {
			if rel == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				t.Errorf("%s %q for method %q: %v", label, rel, entry.Method, err)
			}
		}
	}
}

func TestCheckedInProtocolBaselineContainsNoLocalPaths(t *testing.T) {
	root := filepath.Join("..", "protocolschema", "appserver", "v2")
	markers := []string{
		"/Users/",
		"/home/",
		".cache/codexsdk-upstream",
		".cache/openai-codex",
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		for _, marker := range markers {
			if strings.Contains(text, marker) {
				t.Errorf("%s contains local/cache path marker %q", filepath.ToSlash(path), marker)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
