package schemaoracle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const outputSchemaRecord = `{"type":"object","properties":{"outputSchema":{"description":"Optional JSON Schema"},"other":{},"threadId":{"type":"string"}},"required":["threadId"]}`

func TestOutputSchemaOverlaySourceLocations(t *testing.T) {
	for _, location := range []string{"root", "definition", "$defs", "aggregate"} {
		t.Run(location, func(t *testing.T) {
			name, schema, fragment := "v2/TurnStartParams.json", outputSchemaRecord, ""
			switch location {
			case "definition", "$defs":
				key := "definitions"
				if location == "$defs" {
					key = "$defs"
				}
				name, fragment = "ClientRequest.json", "#/"+key+"/TurnStartParams"
				schema = `{"` + key + `":{"TurnStartParams":` + outputSchemaRecord + `}}`
			case "aggregate":
				name = "ClientRequest.json"
				schema = `{"definitions":{"TurnStartParams":` + outputSchemaRecord + `},"type":"object","properties":{"params":{"$ref":"#/definitions/TurnStartParams"}},"required":["params"]}`
			}
			r := Request{Schemas: map[string][]byte{name: []byte(schema)}, Bindings: map[string]string{name + fragment: "ArbitraryBinding"}, CloseObjects: true}
			targets, err := Build(r)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, probe := range targets[0].Probes {
				var object map[string]any
				if json.Unmarshal([]byte(probe.JSON), &object) != nil || object == nil {
					continue
				}
				if location == "aggregate" {
					object, _ = object["params"].(map[string]any)
				}
				if document, exists := object["outputSchema"]; exists {
					raw, _ := json.Marshal(document)
					want := validateOutputSchemaDocument(document) == nil
					if probe.Valid && !want {
						t.Fatalf("invalid document accepted: %s", probe.JSON)
					}
					if probe.Valid == want {
						seen[string(raw)] = true
					}
				}
			}
			for _, input := range []string{`{}`, `true`, `false`, `null`, `[]`, `"schema"`, `1`, `{"type":1}`, `{"pattern":"["}`, `{"$defs":{},"definitions":{}}`, `{"properties":{"answer":{"type":1}}}`} {
				if !seen[input] {
					t.Errorf("missing classified document %s", input)
				}
			}
		})
	}
}

func TestOutputSchemaOverlayDoesNotGeneralizeFieldOrBindingNames(t *testing.T) {
	r := Request{Schemas: map[string][]byte{"Tool.json": []byte(outputSchemaRecord)}, Bindings: map[string]string{"Tool.json": "TurnStartParams"}, CloseObjects: true}
	targets, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range targets[0].Probes {
		if p.Valid && strings.Contains(p.JSON, `"outputSchema":null`) {
			found = true
		}
	}
	if !found {
		t.Fatal("unrelated unconstrained JSON narrowed by a field or Go name")
	}
	for _, field := range []string{`{"type":"string"}`, `false`, `{"__oracle_output_schema__":true}`} {
		r = Request{Schemas: map[string][]byte{"TurnStartParams.json": []byte(strings.Replace(outputSchemaRecord, `{"description":"Optional JSON Schema"}`, field, 1))}, Bindings: map[string]string{"TurnStartParams.json": "Payload"}, CloseObjects: true}
		if _, err := Build(r); err == nil {
			t.Fatalf("source constraint or private keyword accepted: %s", field)
		}
	}
}

func TestOutputSchemaOverlayKeepsOtherAggregateBranchesUnconstrained(t *testing.T) {
	schema := `{"definitions":{"TurnStartParams":` + outputSchemaRecord + `},"oneOf":[{"type":"object","properties":{"method":{"const":"turn/start"},"params":{"$ref":"#/definitions/TurnStartParams"}},"required":["method","params"]},{"type":"object","properties":{"method":{"const":"other"},"params":{"type":"object","properties":{"outputSchema":{}}}},"required":["method","params"]}]}`
	r := Request{Schemas: map[string][]byte{"ClientRequest.json": []byte(schema)}, Bindings: map[string]string{"ClientRequest.json": "Request"}, CloseObjects: true}
	targets, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, probe := range targets[0].Probes {
		if probe.JSON == `{"method":"other","params":{"outputSchema":null}}` {
			if !probe.Valid {
				t.Fatal("overlay narrowed the unrelated aggregate branch")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing unrelated branch witness")
	}
}

// Execute the real accepted field graph rather than duplicating a tiny wrapper.
// Contract locations: protocolv2/output_schema.go:17-30,61-73;
// protocolv2/output_schema_test.go:9-79; v2/TurnStartParams.json:872-874.
func TestAcceptedOutputSchemaRuntimeContract(t *testing.T) {
	raw, err := os.ReadFile("../protocolschema/appserver/v2/v2/TurnStartParams.json")
	if err != nil {
		t.Fatal(err)
	}
	r := Request{Schemas: map[string][]byte{"v2/TurnStartParams.json": raw}, Bindings: map[string]string{"v2/TurnStartParams.json": "TurnStartParams"}, Package: "verifierfixture", CloseObjects: true}
	source, err := Generate(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		fixture string
		pass    bool
	}{{"good", true}, {"permissive", false}, {"object_only", false}, {"lossy", false}, {"shape_unchecked", false}} {
		t.Run(tt.fixture, func(t *testing.T) {
			dir := t.TempDir()
			for name, from := range map[string]string{"go.mod": "../../../go.mod", "go.sum": "../../../go.sum", "fixture.go": "testdata/verifier-proof/output_schema_" + tt.fixture + ".go"} {
				body, err := os.ReadFile(from)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "oracle_test.go"), source, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "-count=1", ".")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			requireOracleOutcome(t, out, err, tt.pass)
		})
	}
}
