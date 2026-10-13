package schemaoracle

import (
	"encoding/json"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"strings"
	"testing"
)

// The source shape is the unmodified rust-v0.162.1 image object product.
const imageAlternativesSchema = `{"type":"object","properties":{"type":{"type":"string","enum":["image"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]}`

func TestImageAlternativesWholeUnionClassification(t *testing.T) {
	var original, prepared any
	if err := json.Unmarshal([]byte(imageAlternativesSchema), &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(imageAlternativesSchema), &prepared); err != nil {
		t.Fatal(err)
	}
	if err := prepareReachable(prepared, prepared, "#", true, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	compile := func(doc any) *validator.Schema {
		c := validator.NewCompiler()
		if err := c.AddResource("https://schema-oracle.invalid/test.json", doc); err != nil {
			t.Fatal(err)
		}
		v, err := c.Compile("https://schema-oracle.invalid/test.json")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	source, overlay := compile(original), compile(prepared)
	cases := map[string]bool{
		`{"type":"image","url":"ok"}`:                true,
		`{"type":"image","fileId":"id"}`:             true,
		`{"type":"image","url":"ok","fileId":"id"}`:  true,
		`{"type":"image","url":"ok","fileId":3}`:     true,
		`{"type":"image","url":false,"fileId":"id"}`: true,
		`{"type":"image","url":null,"fileId":"id"}`:  true,
		`{"type":"image","url":3,"fileId":false}`:    false,
		`{"type":"image"}`:                           false,
		`{"type":"image","url":"ok","extra":true}`:   false,
	}
	for input, want := range cases {
		var value any
		if err := json.Unmarshal([]byte(input), &value); err != nil {
			t.Fatal(err)
		}
		if got := overlay.Validate(value) == nil; got != want {
			t.Errorf("overlay %s = %v", input, got)
		}
		sourceWant := want || strings.Contains(input, `"extra"`)
		if got := source.Validate(value) == nil; got != sourceWant {
			t.Errorf("source %s = %v", input, got)
		}
	}
}

func TestImageAlternativesOracleAcceptsEvolutionAndRejectsNarrowing(t *testing.T) {
	good := `package fixture
import("encoding/json";"fmt")
type Payload map[string]json.RawMessage
func(p *Payload) UnmarshalJSON(data []byte)error{var fields map[string]json.RawMessage;if err:=json.Unmarshal(data,&fields);err!=nil{return err};if fields==nil{return fmt.Errorf("object required")};for key:=range fields{if key!="type"&&key!="url"&&key!="fileId"{return fmt.Errorf("unknown")}};var kind string;if err:=json.Unmarshal(fields["type"],&kind);err!=nil||kind!="image"{return fmt.Errorf("type")};valid:=func(raw json.RawMessage)bool{var s string;return len(raw)>0&&string(raw)!="null"&&json.Unmarshal(raw,&s)==nil};if !(valid(fields["url"]) || valid(fields["fileId"])) {return fmt.Errorf("branches")};*p=fields;return nil}
`
	t.Run("legitimate_evolution", func(t *testing.T) { runOracleFixture(t, request(imageAlternativesSchema), good, true) })
	defect := strings.Replace(good, `valid(fields["url"]) || valid(fields["fileId"])`, `valid(fields["url"]) && valid(fields["fileId"])`, 1)
	t.Run("branch_intersection_defect", func(t *testing.T) { runOracleFixture(t, request(imageAlternativesSchema), defect, false) })
	narrowed := strings.Replace(good, `if !(valid(fields["url"]) || valid(fields["fileId"]))`, `if (fields["url"]!=nil && !valid(fields["url"])) || (fields["fileId"]!=nil && !valid(fields["fileId"])) || !(valid(fields["url"]) || valid(fields["fileId"]))`, 1)
	t.Run("cross_branch_type_narrowing_defect", func(t *testing.T) { runOracleFixture(t, request(imageAlternativesSchema), narrowed, false) })

}
