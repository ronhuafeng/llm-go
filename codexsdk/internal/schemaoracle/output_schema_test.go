package schemaoracle

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

const turnSchemaFixture = `{"type":"object","properties":{"outputSchema":{"description":"Optional JSON Schema"},"threadId":{"type":"string"}},"required":["threadId"]}`

func TestOutputSchemaContractClassifiesDocuments(t *testing.T) {
	c := validator.NewCompiler()
	c.UseLoader(denyLoader{})
	c.RegisterVocabulary(outputSchemaVocabulary())
	c.AssertVocabs()
	if err := c.AddResource("urn:test:output-schema", map[string]any{outputSchemaKeyword: true}); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("urn:test:output-schema")
	if err != nil {
		t.Fatal(err)
	}
	// Exact accepted expectations: protocolv2/output_schema_test.go:9-78.
	for _, tt := range []struct {
		input string
		valid bool
	}{
		{`true`, true}, {`false`, true}, {`{}`, true},
		{`{"type":"object","properties":{"answer":{"type":"string"}}}`, true},
		{`{"b":2,"a":1}`, true}, // Unknown schema keywords remain preserved.
		{`{"$defs":{"answer":{"type":"string"}},"$ref":"#/$defs/answer"}`, true},
		{`null`, false}, {`[]`, false}, {`"schema"`, false}, {`1`, false},
		{`{"type":1}`, false}, {`{"pattern":"["}`, false},
		{`{"$defs":{},"definitions":{}}`, false},
		{`{"$ref":"https://example.invalid/remote"}`, false},
	} {
		t.Run(tt.input, func(t *testing.T) {
			var value any
			d := json.NewDecoder(strings.NewReader(tt.input))
			d.UseNumber()
			if err := d.Decode(&value); err != nil {
				t.Fatal(err)
			}
			if got := schema.Validate(value) == nil; got != tt.valid {
				t.Fatalf("valid=%t want=%t", got, tt.valid)
			}
		})
	}
}

func TestOutputSchemaOverlayUsesSourcePosition(t *testing.T) {
	for _, tt := range []struct {
		name, document, schema, fragment string
		overlay                          bool
	}{
		{"standalone", "v2/TurnStartParams.json", turnSchemaFixture, "", true},
		{"definition", "ClientRequest.json", `{"definitions":{"TurnStartParams":` + turnSchemaFixture + `}}`, "#/definitions/TurnStartParams", true},
		{"aggregate", "ClientRequest.json", `{"definitions":{"TurnStartParams":` + turnSchemaFixture + `},"$ref":"#/definitions/TurnStartParams"}`, "", true},
		{"unrelated_field", "Tool.json", turnSchemaFixture, "", false},
		{"title_is_annotation", "Other.json", strings.Replace(turnSchemaFixture, `"type":"object"`, `"title":"TurnStartParams","type":"object"`, 1), "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Request{Schemas: map[string][]byte{tt.document: []byte(tt.schema)}, Bindings: map[string]string{tt.document + tt.fragment: "UnrelatedGoName"}, CloseObjects: true}
			targets, err := Build(r)
			if err != nil {
				t.Fatal(err)
			}
			var sawNull, sawOmitted, sawFalse, sawDocument bool
			for _, p := range targets[0].Probes {
				var fields map[string]any
				if json.Unmarshal([]byte(p.JSON), &fields) != nil || fields == nil {
					continue
				}
				if _, ok := fields["threadId"].(string); !ok || len(fields) > 2 {
					continue
				}
				value, exists := fields["outputSchema"]
				if !exists && p.Valid {
					sawOmitted = true
				}
				if exists && value == nil {
					sawNull = true
					if p.Valid == tt.overlay {
						t.Fatalf("null classification: %+v", p)
					}
				}
				if value == false && p.Valid {
					sawFalse = true
				}
				if object, ok := value.(map[string]any); ok && object["type"] == "object" && p.Valid {
					sawDocument = true
				}
			}
			if !sawNull || !sawOmitted || !sawFalse || (tt.overlay && !sawDocument) {
				t.Fatalf("coverage null=%t omitted=%t false=%t document=%t", sawNull, sawOmitted, sawFalse, sawDocument)
			}
		})
	}
	for _, schema := range []string{
		strings.Replace(turnSchemaFixture, `{"description":"Optional JSON Schema"}`, `{"type":"object"}`, 1),
		strings.Replace(turnSchemaFixture, `{"description":"Optional JSON Schema"}`, `{"description":"Optional JSON Schema","x-llm-go-output-schema":true}`, 1),
	} {
		_, err := Build(Request{Schemas: map[string][]byte{"v2/TurnStartParams.json": []byte(schema)}, Bindings: map[string]string{"v2/TurnStartParams.json": "TurnStartParams"}})
		if err == nil {
			t.Fatal("changed overlay source accepted")
		}
	}
}

func TestAcceptedTurnOutputSchemaBinding(t *testing.T) {
	raw, err := os.ReadFile("../protocolschema/appserver/v2/v2/TurnStartParams.json")
	if err != nil {
		t.Fatal(err)
	}
	r := Request{Schemas: map[string][]byte{"v2/TurnStartParams.json": raw}, Bindings: map[string]string{"v2/TurnStartParams.json": "TurnStartParams"}, Package: "verifierfixture", CloseObjects: true}
	for _, tt := range []struct {
		fixture string
		pass    bool
	}{{"output_schema_good.go", true}, {"output_schema_null.go", false}, {"output_schema_lossy.go", false}} {
		t.Run(tt.fixture, func(t *testing.T) {
			output := runOutputSchemaProofFixture(t, r, tt.fixture, tt.pass)
			if !tt.pass {
				failure := "schema-invalid input accepted:"
				if tt.fixture == "output_schema_lossy.go" {
					failure = "wire value lost: input="
				}
				if !bytes.Contains(output, []byte(failure)) || !bytes.Contains(output, []byte(`"outputSchema":`)) {
					t.Fatalf("unrelated mutant failure: %s", output)
				}
			}
		})
	}
	// Reproduce the old rule (raw unconstrained field) without changing source
	// bytes. This good binding must fail specifically on present null.
	r.Schemas = map[string][]byte{"raw.json": raw}
	r.Bindings = map[string]string{"raw.json": "TurnStartParams"}
	t.Run("raw_rule_false_rejection", func(t *testing.T) {
		output := runOutputSchemaProofFixture(t, r, "output_schema_good.go", false)
		if !bytes.Contains(output, []byte("schema-valid input rejected:")) || !bytes.Contains(output, []byte(`"outputSchema":null`)) {
			t.Fatalf("old rule did not falsely reject present null: %s", output)
		}
	})
}

func runOutputSchemaProofFixture(t *testing.T, r Request, fixture string, pass bool) []byte {
	t.Helper()
	source, err := Generate(r)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("testdata/verifier-proof", fixture))
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod = bytes.Replace(mod, []byte("module github.com/ronhuafeng/llm-go"), []byte("module fixture"), 1)
	mod = append(mod, []byte("\nrequire github.com/ronhuafeng/llm-go v0.0.0\nreplace github.com/ronhuafeng/llm-go => "+strconv.Quote(root)+"\n")...)
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"go.mod": mod, "go.sum": sum, "fixture.go": body, "oracle_test.go": source} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	requireOracleOutcome(t, output, err, pass)
	return output
}
