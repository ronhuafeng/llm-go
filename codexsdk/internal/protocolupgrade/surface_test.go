package protocolupgrade

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSurfaceConstructionDoesNotReadPersistedFactProjections(t *testing.T) {
	fix := writeCompleteApplyFixture(t)
	var oldManifest manifestFile
	var oldCoverage coverageFile
	if err := loadJSON(filepath.Join(fix.baseline, "manifest.json"), &oldManifest); err != nil {
		t.Fatal(err)
	}
	if err := loadJSON(filepath.Join(fix.baseline, "coverage_matrix.json"), &oldCoverage); err != nil {
		t.Fatal(err)
	}
	mappings, err := parseRequestMappings(fix.commonRS)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := buildManifest(fix.candidate, fix.stable, oldManifest, mappings, fix.sha)
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := buildCoverage(fix.candidate, fix.stable, oldCoverage, manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Neither schema input has persisted derived facts. Hostile files with those
	// names must not become a second authority for this fresh construction.
	for _, root := range []string{fix.candidate, fix.stable} {
		for _, name := range []string{"manifest.json", "coverage_matrix.json"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte("not a fact source"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	surface, files, err := deriveSurface(fix.stable, fix.candidate, fix.module, manifest, coverage)
	if err != nil {
		t.Fatal(err)
	}
	// Once constructed, annotation/projection edits cannot change wire facts.
	for _, field := range coverage.Fields {
		required, _ := field["required"].(bool)
		field["required"] = !required
		field["status"] = "historical annotation"
	}
	for _, typ := range coverage.Types {
		typ["schema"] = "historical/missing.json"
	}
	for _, root := range []string{fix.candidate, fix.stable} {
		for _, name := range []string{"manifest.json", "coverage_matrix.json"} {
			if err := os.Remove(filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	againSurface, againFiles, err := deriveSurface(fix.stable, fix.candidate, fix.module, manifest, coverage)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(surface, againSurface) || !reflect.DeepEqual(files, againFiles) {
		t.Fatal("persisted projections changed fresh generation")
	}
}
