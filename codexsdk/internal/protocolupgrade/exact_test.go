package protocolupgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareExactBaselineRejectsStaleSemanticArtifacts(t *testing.T) {
	acceptedRoot := t.TempDir()
	rebuiltRoot := t.TempDir()
	accepted := filepath.Join(acceptedRoot, "schema")
	rebuilt := filepath.Join(rebuiltRoot, "schema")
	writeJSONFile(t, filepath.Join(accepted, "ClientRequest.json"), map[string]any{
		"title": "ClientRequest", "type": "object",
		"properties": map[string]any{"method": map[string]any{"type": "string"}},
	})
	writeJSONFile(t, filepath.Join(accepted, "baseline_metadata.json"), map[string]any{
		"source_commit": strings.Repeat("b", 40), "generated_at": "2026-09-24T00:00:00Z",
	})
	writeJSONFile(t, filepath.Join(accepted, "manifest_generation.json"), map[string]any{
		"inputs": map[string]any{"source_commit": strings.Repeat("b", 40)},
	})
	writeJSONFile(t, filepath.Join(accepted, "manifest.json"), manifestFile{
		Entries: []manifestEntry{{
			Kind: "request", Method: "thread/start", Stability: "stable",
			SourceRef: map[string]string{"response_mapping": "exact common.rs mapping"},
		}},
	})
	writeJSONFile(t, filepath.Join(accepted, "coverage_matrix.json"), coverageFile{
		Fields: []map[string]any{{"path": "ClientRequest.json#/properties/method", "required": true}},
	})
	if err := copyTree(accepted, rebuilt); err != nil {
		t.Fatal(err)
	}
	for _, rel := range generatedProtocolArtifacts {
		for _, root := range []string{acceptedRoot, rebuiltRoot} {
			path := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("// generated from exact input\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeJSONFile(t, filepath.Join(rebuilt, "baseline_metadata.json"), map[string]any{
		"source_commit": strings.Repeat("b", 40), "generated_at": "2026-09-25T00:00:00Z",
	})
	if err := compareExactBaseline(accepted, rebuilt, acceptedRoot, rebuiltRoot); err != nil {
		t.Fatalf("matching exact artifacts and differing observation time: %v", err)
	}

	for _, test := range []struct {
		name, artifact string
		mutate         func(string)
	}{
		{name: "requiredness", artifact: "coverage_matrix.json", mutate: func(root string) {
			writeJSONFile(t, filepath.Join(root, "coverage_matrix.json"), coverageFile{
				Fields: []map[string]any{{"path": "ClientRequest.json#/properties/method", "required": false}},
			})
		}},
		{name: "method stability", artifact: "manifest.json", mutate: func(root string) {
			writeJSONFile(t, filepath.Join(root, "manifest.json"), manifestFile{
				Entries: []manifestEntry{{Kind: "request", Method: "thread/start", Stability: "experimental", SourceRef: map[string]string{"response_mapping": "exact common.rs mapping"}}},
			})
		}},
		{name: "response mapping", artifact: "manifest.json", mutate: func(root string) {
			writeJSONFile(t, filepath.Join(root, "manifest.json"), manifestFile{
				Entries: []manifestEntry{{Kind: "request", Method: "thread/start", Stability: "stable", SourceRef: map[string]string{"response_mapping": "stale mapping"}}},
			})
		}},
		{name: "upstream source", artifact: "baseline_metadata.json", mutate: func(root string) {
			writeJSONFile(t, filepath.Join(root, "baseline_metadata.json"), map[string]any{"source_commit": strings.Repeat("c", 40)})
		}},
		{name: "stable or complete schema", artifact: "schema mismatch", mutate: func(root string) {
			if err := os.Remove(filepath.Join(root, "ClientRequest.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "generated Go", artifact: "method_registry.gen.go", mutate: func(root string) {
			if err := os.WriteFile(filepath.Join(acceptedRoot, "protocolv2/method_registry.gen.go"), []byte("// stale generated Go\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "schema")
			if err := copyTree(rebuilt, root); err != nil {
				t.Fatal(err)
			}
			test.mutate(root)
			err := compareExactBaseline(root, rebuilt, acceptedRoot, rebuiltRoot)
			if err == nil || !strings.Contains(err.Error(), test.artifact) {
				t.Fatalf("got %v, want %s mismatch", err, test.artifact)
			}
		})
	}
}

func TestVerifyExactRejectsDisabledDerivation(t *testing.T) {
	for _, req := range []ApplyRequest{{}, {ModuleRoot: t.TempDir(), SkipCodegen: true}} {
		if _, err := VerifyExact(req); err == nil || !strings.Contains(err.Error(), "requires module root") {
			t.Fatalf("got %v, want complete derivation requirement", err)
		}
	}
}
