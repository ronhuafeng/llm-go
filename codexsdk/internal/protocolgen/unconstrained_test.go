package protocolgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGeneratedUnconstrainedValuesRoundTrip(t *testing.T) {
	root := t.TempDir()
	schema := `{"title":"ArbitraryPayload","type":"object","required":["requiredValue"],"properties":{"requiredValue":true,"emptyValue":{},"annotatedValue":{"description":"opaque upstream value"},"defaultValue":{"title":"Anything","default":null}}}`
	if err := os.WriteFile(filepath.Join(root, "ArbitraryPayload.json"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	matrix := CoverageMatrix{Types: []CoverageType{{Schema: "ArbitraryPayload.json", Type: "ArbitraryPayload", Stability: "stable"}}}
	for _, name := range []string{"requiredValue", "emptyValue", "annotatedValue", "defaultValue"} {
		matrix.Fields = append(matrix.Fields, CoverageField{Field: name, Schema: "ArbitraryPayload.json", Path: "ArbitraryPayload.json#/properties/" + name, Required: name == "requiredValue"})
	}
	manifest := Manifest{Entries: []ManifestEntry{{Kind: "request", Direction: "client_to_server", Method: "sample/read", SourceSchema: "ArbitraryPayload.json"}}}
	plan, err := BuildProtocolTypePlanFromFacts(root, matrix, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyWireMessageRoles(&plan, manifest); err != nil {
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
	pkg, err := os.MkdirTemp(filepath.Join(repo, "codexsdk"), "protocol-unconstrained-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(pkg) })
	files := map[string][]byte{"protocol_types.gen.go": generated, "roundtrip_test.go": []byte(`package protocolv2
import("encoding/json";"reflect";"testing")
func TestWireValues(t *testing.T){
 for _,raw:=range []string{"null","{}","[]","123","true","\"text\"","{\"nested\":[1,null,false]}"}{
  for _,present:=range []bool{false,true}{
   wire:="{\"requiredValue\":"+raw
   if present {wire+=",\"emptyValue\":"+raw+",\"annotatedValue\":"+raw+",\"defaultValue\":"+raw};wire+="}"
   var value ArbitraryPayload
   if err:=json.Unmarshal([]byte(wire),&value);err!=nil{t.Fatal(err)}
   if (value.EmptyValue!=nil)!=present || (value.AnnotatedValue!=nil)!=present || (value.DefaultValue!=nil)!=present{t.Fatalf("presence lost: %s",wire)}
   out,err:=json.Marshal(value);if err!=nil{t.Fatal(err)}
   var got,want any
   if err:=json.Unmarshal(out,&got);err!=nil{t.Fatal(err)}
   if err:=json.Unmarshal([]byte(wire),&want);err!=nil{t.Fatal(err)}
   if !reflect.DeepEqual(got,want){t.Fatalf("%s became %s",wire,out)}
  }
 }
 var value ArbitraryPayload
 if err:=json.Unmarshal([]byte("{}"),&value);err==nil{t.Fatal("missing required value accepted")}
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
		t.Fatalf("generated wire fixture: %v\n%s", err, out)
	}
}
