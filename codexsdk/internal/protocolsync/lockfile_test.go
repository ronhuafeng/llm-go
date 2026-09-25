package protocolsync

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestPrepareLockfilePreservesDependencyGraph(t *testing.T) {
	root := t.TempDir()
	metadata, err := json.Marshal(map[string]any{
		"workspace_members": []string{"cli-id", "core-id"},
		"packages": []any{
			map[string]any{"id": "cli-id", "name": "codex-cli", "version": "0.154.0", "manifest_path": filepath.Join(root, "cli", "Cargo.toml")},
			map[string]any{"id": "core-id", "name": "codex-core", "version": "0.154.0", "manifest_path": filepath.Join(root, "core", "Cargo.toml")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`version = 4
[[package]]
name = "codex-cli"
version = "0.0.0"
dependencies = ["codex-core 0.0.0", "serde"]
[[package]]
name = "codex-core"
version = "0.0.0"
dependencies = ["serde"]
[[package]]
name = "serde"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "exact-checksum"
dependencies = ["serde_derive"]
`)
	prepared, err := prepareLockfile(original, metadata, root)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected map[string]any
	if err := toml.Unmarshal(prepared, &actual); err != nil {
		t.Fatal(err)
	}
	// Only these three workspace identity occurrences may change. All third-party
	// identity, checksum, edges and other lock fields must remain equal.
	want := strings.ReplaceAll(string(original), "0.0.0", "0.154.0")
	if err := toml.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("changed dependency graph: %s", prepared)
	}
	again, err := prepareLockfile(original, metadata, root)
	if err != nil || !bytes.Equal(prepared, again) {
		t.Fatalf("non-deterministic preparation: %v", err)
	}
	unchanged, err := prepareLockfile(prepared, metadata, root)
	if err != nil || !bytes.Equal(prepared, unchanged) {
		t.Fatalf("consistent lock changed: %v", err)
	}
	for _, tc := range []struct{ name, lock string }{
		{"missing workspace package", strings.ReplaceAll(string(original), "codex-core", "other-core")},
		{"ambiguous package", string(original) + "\n[[package]]\nname = \"codex-core\"\nversion = \"0.0.0\"\n"},
		{"ambiguous new identity", string(original) + "\n[[package]]\nname = \"codex-core\"\nversion = \"0.154.0\"\nsource = \"registry+other\"\n"},
		{"ambiguous external identity", string(original) + "\n[[package]]\nname = \"codex-core\"\nversion = \"0.0.0\"\nsource = \"registry+other\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := prepareLockfile([]byte(tc.lock), metadata, root); err == nil {
				t.Fatal("unprovable preparation accepted")
			}
		})
	}
}
