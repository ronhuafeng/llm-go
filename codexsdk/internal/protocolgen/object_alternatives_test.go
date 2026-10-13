package protocolgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGeneratedObjectAlternativesPreserveWholeUnion(t *testing.T) {
	schema := mustParseSchema(t, `{"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["image"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]}]}`)
	generated, err := GenerateProtocolTypes(ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, SchemaPath: "Input.json", TypeName: "Input", Schema: schema, GeneratedRoot: true, WireMessageRoles: WireMessageRoleActionBearingMessage}}})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := os.MkdirTemp(filepath.Join(repo, "codexsdk"), "protocol-alternatives-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pkg) })
	files := map[string][]byte{"protocol_types.gen.go": generated, "roundtrip_test.go": []byte(`package protocolv2
import("encoding/json";"reflect";"testing")
func TestWholeUnion(t *testing.T){
 for _, wire:=range []string{
  "{\"type\":\"image\",\"url\":\"\"}",
  "{\"type\":\"image\",\"fileId\":\"\"}",
  "{\"type\":\"image\",\"url\":\"ok\",\"fileId\":\"id\"}",
  "{\"type\":\"image\",\"url\":\"ok\",\"fileId\":3}",
  "{\"type\":\"image\",\"url\":false,\"fileId\":\"id\"}",
  "{\"type\":\"image\",\"url\":null,\"fileId\":\"id\"}",
 }{var value Input;if err:=json.Unmarshal([]byte(wire),&value);err!=nil{t.Fatalf("%s: %v",wire,err)};out,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};var got,want any;_ = json.Unmarshal(out,&got);_ = json.Unmarshal([]byte(wire),&want);if !reflect.DeepEqual(got,want){t.Fatalf("%s became %s",wire,out)}}
 for _,wire:=range []string{"{\"type\":\"image\"}","{\"type\":\"image\",\"url\":3,\"fileId\":false}","{\"type\":\"image\",\"url\":\"ok\",\"extra\":true}"}{var value Input;if err:=json.Unmarshal([]byte(wire),&value);err==nil{t.Fatalf("accepted %s",wire)}}
 for _,url:=range []string{"ok"}{out,err:=json.Marshal(NewInputImage(InputImage{URL:url}));if err!=nil{t.Fatal(err)};var fields map[string]any;_ = json.Unmarshal(out,&fields);if fields["url"]!=url{t.Fatalf("string convenience: %s",out)}}
}
`)}
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
	cmd := exec.Command("go", "test", "./codexsdk/"+filepath.Base(pkg))
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated anyOf fixture: %v\n%s", err, out)
	}
}
