package protocolgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGeneratedStringAliasEnforcesSourceLengthInCodePoints(t *testing.T) {
	schema := mustParseSchema(t, `{"definitions":{"Label":{"type":"string","minLength":2}}}`)
	generated, err := GenerateProtocolTypes(ProtocolTypePlan{Types: []TypePlan{{SchemaPath: "Alias.json", Schema: schema, GeneratedDefinitions: map[string]bool{"Label": true}}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"go.mod":   []byte("module aliasfixture\ngo 1.26.8\n"),
		"types.go": generated,
		"types_test.go": []byte(`package protocolv2
import("encoding/json";"testing")
func TestSourceLength(t *testing.T){for _,raw:=range []string{"null","\"\"","\"é\"","false"}{var value Label;if json.Unmarshal([]byte(raw),&value)==nil{t.Fatalf("accepted %s",raw)}};for _,s:=range []string{"ab","未来"}{var value Label;raw,_:=json.Marshal(s);if err:=json.Unmarshal(raw,&value);err!=nil{t.Fatal(err)};if _,err:=json.Marshal(value);err!=nil{t.Fatal(err)}};if _,err:=json.Marshal(Label("é"));err==nil{t.Fatal("encoded short Unicode label")}}`),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("source alias constraints: %v\n%s", err, out)
	}
}
