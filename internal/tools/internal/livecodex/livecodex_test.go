package livecodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionRequiresConsistentInstallableBaseline(t *testing.T) {
	for _, test := range []struct {
		name, version, ref string
		valid              bool
	}{
		{"stable", "codex-cli 0.159.0", "rust-v0.159.0", true},
		{"candidate", "codex-cli 0.160.0", "rust-v0.160.0", true},
		{"mismatch", "codex-cli 0.159.0", "rust-v0.160.0", false},
		{"floating", "codex-cli latest", "rust-vlatest", false},
		{"missing", "", "rust-v0.159.0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, baselinePath)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"codex_version":"`+test.version+`","source_ref_name":"`+test.ref+`"}`), 0600); err != nil {
				t.Fatal(err)
			}
			version, err := Version(root)
			if (err == nil) != test.valid {
				t.Fatalf("Version: valid=%v, error=%v", test.valid, err)
			}
			if test.valid && version != strings.TrimPrefix(test.ref, "rust-v") {
				t.Fatalf("version=%q", version)
			}
		})
	}
}

func TestRelevanceIncludesSharedCodeAndUnknownPaths(t *testing.T) {
	for _, test := range []struct {
		path     string
		required bool
	}{
		{"codexsdk/client.go", true},
		{"codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json", true},
		{"llmcaller/codex/caller.go", true},
		{"llmkit/llmschema/validate.go", true},
		{"internal/tools/integration/live_codex_smoke_test.go", true},
		{"go.sum", true},
		{".github/workflows/auto-forward.yml", true},
		{".github/workflows/pr-verification.yml", true},
		{"new-runtime-input", true},
		{"README.md", false},
		{"docs/issues.md", false},
		{".github/workflows/govulncheck.yml", false},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := Required([]string{test.path}); got != test.required {
				t.Fatalf("required=%v, want %v", got, test.required)
			}
		})
	}
	if Required(nil) {
		t.Fatal("empty diff must be not applicable")
	}
}
