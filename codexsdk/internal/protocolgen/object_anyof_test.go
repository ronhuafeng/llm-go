package protocolgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

func TestGeneratedObjectAnyOfAgainstIndependentOracle(t *testing.T) {
	const schema = `{"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["image"]},"detail":{"type":"string"}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]},{"type":"object","properties":{"type":{"type":"string","enum":["bounded"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"count":{"type":"integer","format":"int64","minimum":2}},"required":["count"]},{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}},"required":["items"]}]}]}`
	plan := ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, GeneratedRoot: true, TypeName: "Input", SchemaPath: "Input.json", Schema: mustParseSchema(t, schema)}}}
	runGeneratedUnionOracle(t, schema, "Input", plan)
}

func runGeneratedUnionOracle(t *testing.T, schema, name string, plan ProtocolTypePlan) {
	t.Helper()
	if err := ApplyWireMessageRoles(&plan, Manifest{Entries: []ManifestEntry{{Kind: "request", Direction: "client_to_server", Method: "input/read", SourceSchema: name + ".json"}}}); err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := schemaoracle.Generate(schemaoracle.Request{Schemas: map[string][]byte{name + ".json": []byte(schema)}, Bindings: map[string]string{name + ".json": name}, Package: "protocolv2", CloseObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := os.MkdirTemp(filepath.Join(repo, "codexsdk"), "protocol-anyof-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pkg) })
	files := map[string][]byte{"protocol_types.gen.go": generated, "oracle_test.go": oracle}
	for _, name := range []string{"nullable.go", "json_value.go"} {
		raw, err := os.ReadFile(filepath.Join(repo, "codexsdk", "protocolv2", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = raw
	}
	for name, raw := range files {
		if err := os.WriteFile(filepath.Join(pkg, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./codexsdk/"+filepath.Base(pkg))
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated object anyOf: %v\n%s", err, out)
	}
}

func TestObjectAnyOfRejectsUnrepresentedConstraints(t *testing.T) {
	for _, branch := range []string{
		`{"type":"object","properties":{"url":{"type":"string","pattern":"x"}},"required":["url"]}`,
		`{"type":"object","properties":{"url":{"type":"string"}},"required":["missing"]}`,
		`{"type":"object","properties":{"url":{"type":"string"}},"additionalProperties":false}`,
	} {
		schema := mustParseSchema(t, `{"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["image"]}},"required":["type"],"anyOf":[`+branch+`]}]}`)
		_, err := GenerateProtocolTypes(ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, GeneratedRoot: true, TypeName: "Input", SchemaPath: "Input.json", Schema: schema}}})
		if err == nil {
			t.Fatalf("unrepresented branch accepted: %s", branch)
		}
	}
}
