package protocolsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialSchemaComparisonBindsExactSource(t *testing.T) {
	for _, mode := range []string{"same", "changed", "missing", "extra", "metadata", "nested metadata", "aggregate", "wrong identity", "missing identity", "empty"} {
		t.Run(mode, func(t *testing.T) {
			candidate, rebuilt := t.TempDir(), t.TempDir()
			a, b := filepath.Join(candidate, "codexsdk", defaultBaselineRel), filepath.Join(rebuilt, "schema")
			writeFile(t, filepath.Join(a, "v2/Item.json"), `{"type":"object"}`)
			writeFile(t, filepath.Join(b, "v2/Item.json"), `{"type":"object"}`)
			sha := strings.Repeat("a", 40)
			writeFile(t, filepath.Join(a, "baseline_metadata.json"), `{"source_commit":"`+sha+`"}`)
			writeFile(t, filepath.Join(rebuilt, "common.rs.source_sha"), sha)
			switch mode {
			case "changed":
				writeFile(t, filepath.Join(a, "v2/Item.json"), `{"type":"string"}`)
			case "missing":
				os.Remove(filepath.Join(a, "v2/Item.json"))
			case "extra":
				writeFile(t, filepath.Join(a, "v2/Extra.json"), `{}`)
			case "metadata":
				writeFile(t, filepath.Join(a, "coverage_matrix.json"), `{}`)
			case "nested metadata":
				writeFile(t, filepath.Join(a, "v2/baseline_metadata.json"), `{}`)
			case "aggregate":
				writeFile(t, filepath.Join(a, "codex_app_server_protocol.schemas.json"), `{}`)
			case "wrong identity":
				writeFile(t, filepath.Join(rebuilt, "common.rs.source_sha"), strings.Repeat("b", 40))
			case "missing identity":
				os.Remove(filepath.Join(rebuilt, "common.rs.source_sha"))
			case "empty":
				os.Remove(filepath.Join(a, "v2/Item.json"))
				os.Remove(filepath.Join(b, "v2/Item.json"))
			}
			err := verifyOfficialSchema(candidate, rebuilt)
			if (err == nil) != (mode == "same" || mode == "metadata") {
				t.Fatalf("%s: %v", mode, err)
			}
		})
	}
}
