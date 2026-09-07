package architecture

import (
	"strings"
	"testing"
)

func TestPublicAPIIsDerivedFromExportedSource(t *testing.T) {
	actual, err := ExportPublicAPI(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(actual, "type github.com/ronhuafeng/llm-go/llmcaller/codex.Caller") {
		t.Fatalf("derived public API omitted Caller:\n%s", actual)
	}
	second, err := ExportPublicAPI(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if actual != second {
		t.Fatal("derived public API is not deterministic")
	}
}
