package protocolgen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGeneratedObjectAnyOfContracts(t *testing.T) {
	const schema = `{"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["image"]},"detail":{"type":"string"}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]},{"type":"object","properties":{"type":{"type":"string","enum":["bounded"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"count":{"type":"integer","format":"int64","minimum":2}},"required":["count"]},{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}},"required":["items"]}]}]}`
	plan := ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, GeneratedRoot: true, TypeName: "Input", SchemaPath: "Input.json", Schema: mustParseSchema(t, schema)}}}
	runGeneratedContracts(t, "Input", plan, []string{
		`{"type":"image","url":"https://example.test","detail":"small"}`,
		`{"type":"image","fileId":"f"}`,
		`{"type":"image","fileId":"f","url":"u"}`,
		`{"type":"image","fileId":"f","url":null}`,
		`{"type":"bounded","count":2}`,
		`{"type":"bounded","items":[]}`,
		`{"type":"bounded","items":["x"],"count":0}`,
	}, []string{
		`{"type":"image"}`, `{"type":"image","fileId":4}`, `{"type":"image","url":"u","unknown":1}`,
		`{"type":"bounded","count":1}`, `{"type":"bounded","items":[1]}`, `{"type":"other"}`,
	})
}

// Compile the generator output and exercise native public JSON entrypoints.
// Expectations are explicit behavior contracts, not another schema interpreter.
func runGeneratedContracts(t *testing.T, name string, plan ProtocolTypePlan, accepted, rejected []string) {
	t.Helper()
	if err := ApplyWireMessageRoles(&plan, Manifest{Entries: []ManifestEntry{{Kind: "request", Direction: "client_to_server", Method: "fixture/read", SourceSchema: name + ".json"}}}); err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProtocolTypes(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := os.MkdirTemp(filepath.Join(repo, "codexsdk"), "protocol-contract-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pkg) })
	source := fmt.Sprintf(`package protocolv2
import("encoding/json";"reflect";"strings";"testing")
func TestContract(t *testing.T) {
 for _, wire := range %#v {
  var value %s
  if err := json.Unmarshal([]byte(wire), &value); err != nil {t.Fatalf("accept %%s: %%v",wire,err)}
  encoded, err := json.Marshal(value); if err != nil {t.Fatal(err)}
  var original, roundtrip any
  decode := json.NewDecoder(strings.NewReader(wire)); decode.UseNumber()
  if err := decode.Decode(&original); err != nil {t.Fatal(err)}
  decode = json.NewDecoder(strings.NewReader(string(encoded))); decode.UseNumber()
  if err := decode.Decode(&roundtrip); err != nil {t.Fatal(err)}
  if !reflect.DeepEqual(original,roundtrip) {t.Fatalf("wire value lost: %%s became %%s",wire,encoded)}
 }
 for _, wire := range %#v {
  var value %s
  if err := json.Unmarshal([]byte(wire), &value); err == nil {t.Fatalf("invalid value accepted: %%s",wire)}
 }
}
`, accepted, name, rejected, name)
	files := map[string][]byte{"protocol_types.gen.go": generated, "contract_test.go": []byte(source)}
	for _, file := range []string{"nullable.go", "json_value.go"} {
		data, err := os.ReadFile(filepath.Join(repo, "codexsdk/protocolv2", file))
		if err != nil {
			t.Fatal(err)
		}
		files[file] = data
	}
	for file, data := range files {
		if err := os.WriteFile(filepath.Join(pkg, file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./codexsdk/"+filepath.Base(pkg))
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated contracts: %v\n%s", err, out)
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
