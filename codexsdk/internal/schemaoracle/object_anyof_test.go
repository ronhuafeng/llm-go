package schemaoracle

import (
	"encoding/json"
	"strings"
	"testing"
)

// The object product in rust-v0.162.1 UserInput.image: branch fields are open
// in the other branch. The SDK closes the field set of the complete record.
const imageObjectProduct = `{"type":"object","properties":{"type":{"type":"string","enum":["image"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]}`

const imageObjectDecoder = `package fixture
import("encoding/json";"fmt")
type Payload map[string]json.RawMessage
func(p *Payload)UnmarshalJSON(b []byte)error{
 var fields map[string]json.RawMessage
 if err:=json.Unmarshal(b,&fields);err!=nil{return err};if fields==nil{return fmt.Errorf("object")}
 var typ string;if json.Unmarshal(fields["type"],&typ)!=nil||typ!="image"{return fmt.Errorf("type")}
 for key:=range fields{if key!="type"&&key!="url"&&key!="fileId"{return fmt.Errorf("unknown")}}
 isString:=func(raw json.RawMessage)bool{var v any;if json.Unmarshal(raw,&v)!=nil{return false};_,ok:=v.(string);return ok}
 if !isString(fields["url"])&&!isString(fields["fileId"]){return fmt.Errorf("anyOf")}
 *p=fields;return nil
}
`

func TestObjectAnyOfOraclePreservesBranchOpenness(t *testing.T) {
	for _, close := range []bool{false, true} {
		r := request(imageObjectProduct)
		r.CloseObjects = close
		targets, err := Build(r)
		if err != nil {
			t.Fatal(err)
		}
		mixed, both, single := false, false, false
		for _, probe := range targets[0].Probes {
			var fields map[string]any
			if json.Unmarshal([]byte(probe.JSON), &fields) != nil || !probe.Valid {
				continue
			}
			url, u := fields["url"]
			file, f := fields["fileId"]
			_, us := url.(string)
			_, fs := file.(string)
			single = single || (u != f)
			both = both || (us && fs)
			mixed = mixed || (u && f && us != fs)
		}
		if !single || !both || !mixed {
			t.Fatalf("close=%v: single=%v both=%v mixed=%v", close, single, both, mixed)
		}
	}
}

func TestObjectAnyOfOracleExecutesEvolutionAndDefectFixtures(t *testing.T) {
	r := request(imageObjectProduct)
	t.Run("whole_union", func(t *testing.T) { runOracleFixture(t, r, imageObjectDecoder, true) })
	t.Run("intersection_rejects_valid_mixed_types", func(t *testing.T) {
		bad := strings.Replace(imageObjectDecoder, `if !isString(fields["url"])&&!isString(fields["fileId"])`, `if (fields["url"]!=nil&&!isString(fields["url"]))||(fields["fileId"]!=nil&&!isString(fields["fileId"]))||(fields["url"]==nil&&fields["fileId"]==nil)`, 1)
		runOracleFixture(t, r, bad, false)
	})
	t.Run("no_branch_validation", func(t *testing.T) {
		bad := strings.Replace(imageObjectDecoder, `if !isString(fields["url"])&&!isString(fields["fileId"])`, `if false&&isString(fields["url"])`, 1)
		runOracleFixture(t, r, bad, false)
	})
	t.Run("lossy_branch_field", func(t *testing.T) {
		bad := strings.Replace(imageObjectDecoder, `*p=fields`, `delete(fields,"fileId");*p=fields`, 1)
		runOracleFixture(t, r, bad, false)
	})
}
