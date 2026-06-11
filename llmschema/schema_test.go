package llmschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaJSONForProjectsGoType(t *testing.T) {
	type verdict struct {
		Answer string `json:"answer" jsonschema:"final answer"`
		Score  int    `json:"score,omitempty"`
	}

	data, err := SchemaJSONFor[verdict]()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"answer"`) || !strings.Contains(string(data), `"final answer"`) {
		t.Fatalf("SchemaJSONFor output missing projected field facts: %s", data)
	}
}

func TestSchemaJSONForTreatsRawMessageAsArbitraryJSON(t *testing.T) {
	type output struct {
		Evidence json.RawMessage `json:"evidence,omitempty"`
	}

	data, err := SchemaJSONFor[output]()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"evidence":true`) {
		t.Fatalf("RawMessage should project as arbitrary JSON schema: %s", data)
	}
}

func TestDecodeStructuredOutput(t *testing.T) {
	type verdict struct {
		Passed bool `json:"passed"`
	}

	got, err := Decode[verdict]([]byte(`{"passed":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed {
		t.Fatalf("Decode passed = false, want true")
	}

	if _, err := Decode[verdict]([]byte(`{"passed":`)); err == nil {
		t.Fatal("Decode accepted invalid JSON")
	}
}
