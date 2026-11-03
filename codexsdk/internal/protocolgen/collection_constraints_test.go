package protocolgen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedCollectionAndAliasContracts(t *testing.T) {
	const schema = `{"title":"Payload","type":"object","definitions":{"Effort":{"type":"string","minLength":2}},"required":["names","flags","effort","nullable"],"properties":{"names":{"type":"array","items":{"type":"string"}},"flags":{"type":"object","additionalProperties":{"type":"boolean"}},"effort":{"$ref":"#/definitions/Effort"},"nullable":{"type":"object","additionalProperties":{"type":["string","null"]}},"arbitrary":{"type":"array","items":true},"nested":{"type":"object","additionalProperties":{"type":"array","items":{"type":"string"}}}}}`
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Payload.json"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	matrix := CoverageMatrix{Types: []CoverageType{{Schema: "Payload.json", Type: "Payload", Stability: "stable"}}}
	for _, name := range []string{"names", "flags", "effort", "nullable", "arbitrary", "nested"} {
		matrix.Fields = append(matrix.Fields, CoverageField{Field: name, Schema: "Payload.json", Path: "Payload.json#/properties/" + name, Required: name != "arbitrary" && name != "nested"})
	}
	manifest := Manifest{Entries: []ManifestEntry{{Kind: "request", Direction: "client_to_server", Method: "payload/read", SourceSchema: "Payload.json"}}}
	plan, err := BuildProtocolTypePlanFromFacts(root, matrix, manifest)
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedContracts(t, "Payload", plan, []string{
		`{"names":[],"flags":{},"effort":"ok","nullable":{}}`,
		`{"names":["x"],"flags":{"ready":true},"effort":"high","nullable":{"one":null,"two":"x"},"arbitrary":[null,{},1],"nested":{"a":["x"]}}`,
	}, []string{
		`{"names":null,"flags":{},"effort":"ok","nullable":{}}`,
		`{"names":[1],"flags":{},"effort":"ok","nullable":{}}`,
		`{"names":[],"flags":{"x":1},"effort":"ok","nullable":{}}`,
		`{"names":[],"flags":{},"effort":"x","nullable":{}}`,
		`{"names":[],"flags":{},"effort":"ok","nullable":{"x":4}}`,
		`{"names":[],"flags":{},"effort":"ok"}`,
	})
}
