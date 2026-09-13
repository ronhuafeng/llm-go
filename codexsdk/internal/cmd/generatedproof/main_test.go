package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONOutPreservesUnobservedOnFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated.json")
	code := run([]string{
		"-module-root", filepath.Join("..", "..", ".."),
		"-expected-repository-commit", "0000000000000000000000000000000000000000",
		"-json-out", out,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["repository_commit"] == nil || payload["repository_tree"] == nil {
		t.Fatalf("failure JSON dropped observed repository identity: %s", raw)
	}
	for _, key := range []string{"generated_artifacts_reproducible", "baseline_path_leak", "artifacts"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("unobserved field %q present in CLI JSON: %s", key, raw)
		}
	}
}
