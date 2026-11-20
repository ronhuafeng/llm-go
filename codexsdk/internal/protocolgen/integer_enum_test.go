package protocolgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The version field is the shape introduced by rust-v0.171.0's
// AgentMessageDisplay. Check the same rule in records and tagged variants.
func TestGeneratedIntegerEnumContracts(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		schema := `{"type":"object","required":["version"],"properties":{"version":{"type":"integer","format":"uint8","minimum":0,"enum":[1]},"optional":{"type":"integer","enum":[-2,3]},"exact":{"type":"integer","format":"uint64","enum":[9007199254740993,18446744073709551615]}}}`
		accepted := []string{`{"version":1}`, `{"version":1,"optional":-2,"exact":9007199254740993}`, `{"version":1,"optional":3,"exact":18446744073709551615}`}
		rejected := []string{`{}`, `{"version":0}`, `{"version":2}`, `{"version":256}`, `{"version":-1}`, `{"version":"1"}`, `{"version":null}`, `{"version":1,"optional":0}`, `{"version":1,"exact":9007199254740992}`}
		var plan ProtocolTypePlan
		if tagged {
			schema = `{"oneOf":[{"type":"object","properties":{"mode":{"type":"string","enum":["show"]},"version":{"type":"integer","format":"uint8","minimum":0,"enum":[1]}},"required":["mode","version"]}]}`
			plan = ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, GeneratedRoot: true, TypeName: "Payload", SchemaPath: "Payload.json", Schema: mustParseSchema(t, schema)}}}
			accepted = []string{`{"mode":"show","version":1}`}
			rejected = []string{`{"mode":"show"}`, `{"mode":"show","version":0}`, `{"mode":"show","version":2}`, `{"mode":"show","version":"1"}`, `{"mode":"show","version":null}`}
		} else {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "Payload.json"), []byte(schema), 0600); err != nil {
				t.Fatal(err)
			}
			matrix := CoverageMatrix{Types: []CoverageType{{Schema: "Payload.json", Type: "Payload", Stability: "stable"}}}
			for _, name := range []string{"version", "optional", "exact"} {
				matrix.Fields = append(matrix.Fields, CoverageField{Field: name, Schema: "Payload.json", Path: "Payload.json#/properties/" + name, Required: name == "version"})
			}
			var err error
			plan, err = BuildProtocolTypePlanFromFacts(root, matrix, Manifest{Entries: []ManifestEntry{{Kind: "request", Direction: "client_to_server", Method: "payload/read", SourceSchema: "Payload.json"}}})
			if err != nil {
				t.Fatal(err)
			}
		}
		runGeneratedContracts(t, "Payload", plan, accepted, rejected)
	}
}

func TestIntegerEnumsFailClosedWhenUnrepresentable(t *testing.T) {
	for _, schema := range []string{
		`{"type":"integer","enum":[1,"1"]}`,
		`{"type":"integer","enum":[]}`,
		`{"type":"integer","enum":[null]}`,
		`{"type":"integer","enum":[1.5]}`,
		`{"type":["integer","null"],"enum":[1]}`,
		`{"type":"boolean","enum":[true]}`,
		`{"type":"integer","format":"uint8","enum":[256]}`,
		`{"type":"integer","format":"uint64","enum":[18446744073709551616]}`,
		`{"type":"array","items":{"type":"integer","enum":[1]}}`,
	} {
		var field Schema
		err := json.Unmarshal([]byte(schema), &field)
		var plan FieldPlan
		if err == nil {
			plan, err = planField(CoverageField{Field: "value", Path: "Payload.json#/properties/value", Schema: "Payload.json"}, &field)
		}
		if err == nil && plan.Kind != FieldPlanConstrainedDeferred && plan.Kind != FieldPlanUnionDeferred {
			t.Fatalf("unrepresentable enum was accepted: %s", schema)
		}
	}
}
