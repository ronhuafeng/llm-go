package protocolgen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedCollectionAndAliasConstraintsAgainstSourceOracle(t *testing.T) {
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
	runGeneratedUnionOracle(t, schema, "Payload", plan)
}
