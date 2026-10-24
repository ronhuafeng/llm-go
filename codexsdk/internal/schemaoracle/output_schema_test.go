package schemaoracle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
			targets, err := collectTargets(r)
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
	targets, err := collectTargets(r)
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
		if _, err := collectTargets(r); err == nil {
			t.Fatalf("source constraint or private keyword accepted: %s", field)
		}
	}
}

func TestOutputSchemaOverlayKeepsOtherAggregateBranchesUnconstrained(t *testing.T) {
	schema := `{"definitions":{"TurnStartParams":` + outputSchemaRecord + `},"oneOf":[{"type":"object","properties":{"method":{"const":"turn/start"},"params":{"$ref":"#/definitions/TurnStartParams"}},"required":["method","params"]},{"type":"object","properties":{"method":{"const":"other"},"params":{"type":"object","properties":{"outputSchema":{}}}},"required":["method","params"]}]}`
	r := Request{Schemas: map[string][]byte{"ClientRequest.json": []byte(schema)}, Bindings: map[string]string{"ClientRequest.json": "Request"}, CloseObjects: true}
	targets, err := collectTargets(r)
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

// A minimal counterexample keeps the independently classified semantic rule
// separate from the real selected-type integration below.
func TestOutputSchemaRuntimeDefects(t *testing.T) {
	const schema = `{"type":"object","properties":{"outputSchema":{"description":"Optional JSON Schema"}}}`
	r := request(schema)
	r.Schemas = map[string][]byte{"v2/TurnStartParams.json": []byte(schema)}
	r.Bindings = map[string]string{"v2/TurnStartParams.json": "Payload"}
	const code = `package fixture
 import("encoding/json";"fmt";p "github.com/ronhuafeng/llm-go/codexsdk/protocolv2")
 type Payload struct{OutputSchema json.RawMessage ` + "`json:\"outputSchema,omitempty\"`" + `}
 func(v *Payload)UnmarshalJSON(b []byte)error{var fields map[string]json.RawMessage;if json.Unmarshal(b,&fields)!=nil||fields==nil||len(fields)>1{return fmt.Errorf("fields")};for key,raw:=range fields{if key!="outputSchema"{return fmt.Errorf("unknown")};CHECK;v.OutputSchema=raw};return nil}
 `
	checks := map[string]string{"good": `if _,err:=p.OutputSchemaFromJSON(raw);err!=nil{return err}`, "permissive": `_ = p.OutputSchema{} `, "object_only": `if len(raw)==0||raw[0]!='{'{return fmt.Errorf("object")};if _,err:=p.OutputSchemaFromJSON(raw);err!=nil{return err}`, "shape_unchecked": `if string(raw)!="true"&&string(raw)!="false"&&(len(raw)==0||raw[0]!='{'){return fmt.Errorf("shape")};_ = p.OutputSchema{}`}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) { runOracleFixture(t, r, strings.Replace(code, "CHECK", check, 1), name == "good") })
	}
}

func TestCurrentSelectedTypesAgainstIndependentSchemas(t *testing.T) {
	schemas := map[string][]byte{}
	bindings := map[string]string{}
	for _, path := range []string{"v2/TurnStartParams.json", "v2/ThreadRealtimeItemStartedNotification.json", "v2/McpResourceReadResponse.json", "McpServerElicitationRequestParams.json"} {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		raw, err := os.ReadFile(filepath.Join("../protocolschema/appserver/v2", path))
		if err != nil {
			t.Fatal(err)
		}
		schemas[path] = raw
		bindings[path] = name
	}
	r := Request{Schemas: schemas, Bindings: bindings, CloseObjects: true, ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2"}
	runCurrentTypes(t, r)
}

func runCurrentTypes(t *testing.T, r Request) {
	t.Helper()
	dir := t.TempDir()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	mod := "module fixture\ngo 1.26.8\nrequire github.com/ronhuafeng/llm-go v0.0.0\nreplace github.com/ronhuafeng/llm-go => " + strconv.Quote(root) + "\n"
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0600); err != nil {
		t.Fatal(err)
	}
	runCompiledFixture(t, r, dir, true)
}
