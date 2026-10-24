package schemaoracle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func request(schema string) Request {
	return Request{Schemas: map[string][]byte{"target.json": []byte(schema)}, Bindings: map[string]string{"target.json": "Payload"}, CloseObjects: true}
}

func TestLegalFieldRemovalAndAddition(t *testing.T) {
	old := `{"type":"object","properties":{"retiredFlag":{"type":"boolean"},"stableFlag":{"type":"boolean"}},"required":["retiredFlag","stableFlag"]}`
	current := `{"type":"object","properties":{"currentFlag":{"type":"boolean"},"stableFlag":{"type":"boolean"}},"required":["currentFlag","stableFlag"]}`
	candidate := `package fixture
import("bytes";"encoding/json";"fmt")
type Payload struct{ Current bool ` + "`json:\"currentFlag\"`" + `; Stable bool ` + "`json:\"stableFlag\"`" + ` }
func(p *Payload) UnmarshalJSON(b []byte)error {var m map[string]json.RawMessage;if err:=json.Unmarshal(b,&m);err!=nil{return err};if len(m)!=2||m["currentFlag"]==nil||m["stableFlag"]==nil||bytes.Equal(m["currentFlag"],[]byte("null"))||bytes.Equal(m["stableFlag"],[]byte("null")){return fmt.Errorf("invalid fields")};type plain Payload;return json.Unmarshal(b,(*plain)(p))}
`
	for _, tt := range []struct {
		name, schema string
		wantPass     bool
	}{{"stale_oracle_rejects_legal_candidate", old, false}, {"candidate_schema_oracle_accepts_legal_candidate", current, true}} {
		t.Run(tt.name, func(t *testing.T) {
			_, e := collectTargets(request(tt.schema))
			if e != nil {
				t.Fatal(e)
			}
			runOracleFixture(t, request(tt.schema), candidate, tt.wantPass)
		})
	}
}

func TestOpenUnionClassifiesMutationsWithWholeSchema(t *testing.T) {
	req := request(`{"definitions":{"Error":{"anyOf":[{"type":"object","properties":{"known":{"type":"integer"}},"required":["known"],"additionalProperties":false},{"type":["object","string"]}]}},"type":"null"}`)
	req.Bindings = map[string]string{"target.json#/definitions/Error": "Payload"}
	got, e := collectTargets(req)
	if e != nil {
		t.Fatal(e)
	}
	var sawMalformedKnown, sawUnknown, sawInvalid bool
	for _, p := range got[0].Probes {
		if p.JSON == `{"known":null}` && p.Valid {
			sawMalformedKnown = true
		}
		if strings.Contains(p.JSON, "oracle_future") && p.Valid {
			sawUnknown = true
		}
		if p.JSON == "false" && !p.Valid {
			sawInvalid = true
		}
	}
	if !sawMalformedKnown || !sawUnknown || !sawInvalid {
		t.Fatalf("missing coverage malformed=%v unknown=%v invalid=%v", sawMalformedKnown, sawUnknown, sawInvalid)
	}
}
func TestClosedObjectOverlayIsExplicit(t *testing.T) {
	r := request(`{"type":"object","properties":{"value":{"type":"string"}}}`)
	for _, closed := range []bool{false, true} {
		r.CloseObjects = closed
		ts, e := collectTargets(r)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, p := range ts[0].Probes {
			if strings.Contains(p.JSON, "__oracle_unknown_field__") {
				found = true
				if p.Valid == closed {
					t.Fatalf("closed=%v classified unknown as valid=%v", closed, p.Valid)
				}
			}
		}
		if !found {
			t.Fatal("missing unknown-field probe")
		}
	}
}
func TestBindingAndUnsupportedFailClosed(t *testing.T) {
	for _, schema := range []string{`{"$ref":"https://example.com/network"}`, `{"type":"string","pattern":"a+"}`, `{"allOf":[{"type":"object"},{"type":"object"}]}`, `{"$id":"https://example.com/base","type":"null"}`} {
		if _, e := collectStream(request(schema)); e == nil {
			t.Fatalf("accepted unsupported %s", schema)
		}
	}
	r := request(`{"type":"null"}`)
	r.Bindings["target.json"] = ""
	if _, e := collectStream(r); e == nil {
		t.Fatal("missing binding accepted")
	}
}
func TestDistinctFieldsAndDeterminism(t *testing.T) {
	r := request(`{"type":"object","properties":{"first":{"type":"string"},"second":{"type":"string"}},"required":["first","second"]}`)
	a, e := collectStream(r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := collectStream(r)
	if e != nil {
		t.Fatal(e)
	}
	if string(a) != string(b) {
		t.Fatal("nondeterministic oracle")
	}
	ts, _ := collectTargets(r)
	var obj map[string]any
	_ = json.Unmarshal([]byte(ts[0].Probes[0].JSON), &obj)
	if obj["first"] == obj["second"] {
		t.Fatal("fields indistinguishable")
	}
}

// Execute only locally authored fixtures. A candidate's vacuous own test cannot
// neutralize independently derived obligations. No archived candidate is run.
func TestRuntimeDetectsLostFieldsAndWeakValidation(t *testing.T) {
	r := request(`{"type":"object","properties":{"first":{"type":"string"},"second":{"type":"string"}},"required":["first","second"]}`)
	good := `package fixture
import("bytes";"encoding/json";"fmt")
type Payload struct{ First string ` + "`json:\"first\"`" + `;Second string ` + "`json:\"second\"`" + ` }
func(p *Payload) UnmarshalJSON(b []byte)error {var m map[string]json.RawMessage;if err:=json.Unmarshal(b,&m);err!=nil{return err};if len(m)!=2||m["first"]==nil||m["second"]==nil||bytes.Equal(m["first"],[]byte("null"))||bytes.Equal(m["second"],[]byte("null")){return fmt.Errorf("invalid fields")};type plain Payload;return json.Unmarshal(b,(*plain)(p))}
`
	for _, tt := range []struct {
		name, code string
		pass       bool
	}{{"valid", good, true}, {"drop_added_field", strings.Replace(good, "type plain Payload;return json.Unmarshal(b,(*plain)(p))", "type plain Payload;err:=json.Unmarshal(b,(*plain)(p));p.Second=\"\";return err", 1), false}, {"weak_decoder", `package fixture;type Payload map[string]any`, false}} {
		t.Run(tt.name, func(t *testing.T) {
			runOracleFixture(t, r, tt.code, tt.pass)
		})
	}
}

func TestCompoundObjectAndRecursiveBaseCases(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"type":{"const":"image"},"detail":{"type":["string","null"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]}`,
		`{"$ref":"#/definitions/Node","definitions":{"Node":{"type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"#/definitions/Node"}}},"required":["name","children"]}}}`,
	} {
		if _, e := collectTargets(request(schema)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := collectTargets(request(`{"$ref":"#/definitions/Node","definitions":{"Node":{"type":"object","properties":{"child":{"$ref":"#/definitions/Node"}},"required":["child"]}}}`)); e == nil {
		t.Fatal("uninhabited required recursive shape accepted")
	}
}

func TestExternalProbeCannotBeSuppressedByCandidateTestMain(t *testing.T) {
	r := request(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)
	out := runOracleFixture(t, r, "package fixture;type Payload struct { Value string `json:\"value\"`;Stale string `json:\"removed,omitempty\"` }", false)
	if !strings.Contains(string(out), "Go JSON fields=[removed value]") {
		t.Fatal(string(out))
	}
}

func TestMutationsAreBoundedByFieldCoverageNotSiblingProducts(t *testing.T) {
	properties := map[string]any{}
	for i := 0; i < 60; i++ {
		properties[fmt.Sprintf("field%02d", i)] = map[string]any{"anyOf": []any{map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}}, map[string]any{"type": "null"}}}
	}
	raw, e := json.Marshal(map[string]any{"type": "object", "properties": properties})
	if e != nil {
		t.Fatal(e)
	}
	targets, e := collectTargets(request(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	if len(targets[0].Probes) > 2000 {
		t.Fatalf("redundant sibling cross-product: %d probes", len(targets[0].Probes))
	}
	for key := range properties {
		found := false
		for _, p := range targets[0].Probes {
			if !p.Valid && strings.Contains(p.JSON, `"`+key+`":-1`) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing type-corruption obligation for %s", key)
		}
	}
}

func TestNegativeCoverageKeepsSameShapeTaggedBranches(t *testing.T) {
	r := request(`{"oneOf":[{"type":"object","properties":{"tag":{"const":"first"},"value":{"type":"string"}},"required":["tag","value"]},{"type":"object","properties":{"tag":{"const":"second"},"value":{"type":"string"}},"required":["tag","value"]}]}`)
	targets, e := collectTargets(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, tag := range []string{"first", "second"} {
		found := false
		for _, p := range targets[0].Probes {
			if p.JSON == `{"tag":"`+tag+`","value":null}` && !p.Valid {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing branch-specific invalid-value coverage for %s", tag)
		}
	}
}

func TestCompoundRootPreservesLocalDefinitionResources(t *testing.T) {
	r := request(`{"$schema":"http://json-schema.org/draft-07/schema#","definitions":{"Payload":{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}},"properties":{"emittedAt":{"type":"integer"}},"oneOf":[{"type":"object","properties":{"method":{"const":"event"},"params":{"$ref":"#/definitions/Payload"}},"required":["method","params"]}]}`)
	if _, err := collectTargets(r); err != nil {
		t.Fatal(err)
	}
}

func TestDeepRequiredAndTypeMutationsExecuteAgainstWeakDecoder(t *testing.T) {
	r := request(`{"type":"object","properties":{"outer":{"type":"object","properties":{"inner":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}},"required":["inner"]}},"required":["outer"]}`)
	good := `package fixture
import("encoding/json";"fmt")
type Payload struct{Outer json.RawMessage ` + "`json:\"outer\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var root map[string]json.RawMessage;if json.Unmarshal(b,&root)!=nil||len(root)!=1||root["outer"]==nil{return fmt.Errorf("root")};var outer map[string]json.RawMessage;if json.Unmarshal(root["outer"],&outer)!=nil||len(outer)!=1||outer["inner"]==nil{return fmt.Errorf("outer")};var inner map[string]any;if json.Unmarshal(outer["inner"],&inner)!=nil{return fmt.Errorf("inner")};if _,ok:=inner["value"].(string);!ok||len(inner)!=1{return fmt.Errorf("value")};p.Outer=root["outer"];return nil}
`
	t.Run("correct", func(t *testing.T) { runOracleFixture(t, r, good, true) })
	t.Run("deep_validation_removed", func(t *testing.T) {
		bad := strings.Replace(good, `if _,ok:=inner["value"].(string);!ok||len(inner)!=1{return fmt.Errorf("value")};`, "", 1)
		runOracleFixture(t, r, bad, false)
	})
}

func TestExactNumericWitnessesAndSemanticRoundtrip(t *testing.T) {
	for _, schema := range []string{`{"type":"number","minimum":0.1,"maximum":0.2}`, `{"type":"integer","minimum":9007199254740993,"maximum":9007199254740993}`, `{"type":"integer","minimum":-2.7,"maximum":-1.2}`} {
		if _, err := collectTargets(request(schema)); err != nil {
			t.Fatal(err)
		}
	}
	runOracleFixture(t, request(`{"type":"number","minimum":1,"maximum":1}`), `package fixture
import("encoding/json";"fmt")
type Payload float64
func(p *Payload)UnmarshalJSON(b []byte)error{var v any;if err:=json.Unmarshal(b,&v);err!=nil{return err};n,ok:=v.(float64);if !ok||n!=1{return fmt.Errorf("invalid")};*p=1;return nil}
func(p Payload)MarshalJSON()([]byte,error){return []byte("1.0"),nil}
`, true)
}

func TestTypedAdditionalPropertiesHavePositiveAndNegativeEvidence(t *testing.T) {
	r := request(`{"type":"object","properties":{"known":{"type":"string"}},"required":["known"],"additionalProperties":{"type":"string"}}`)
	targets, err := collectTargets(r)
	if err != nil {
		t.Fatal(err)
	}
	positive, negative := false, false
	for _, probe := range targets[0].Probes {
		if strings.Contains(probe.JSON, "__oracle_map_entry__") {
			if probe.Valid {
				positive = true
			} else {
				negative = true
			}
		}
	}
	if !positive || !negative {
		t.Fatalf("typed additional evidence positive=%v negative=%v", positive, negative)
	}
	code := `package fixture
import("encoding/json";"fmt")
type Payload struct{Known string ` + "`json:\"known\"`" + `;Extras map[string]string ` + "`json:\"-\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var value map[string]any;if err:=json.Unmarshal(b,&value);err!=nil{return err};known,ok:=value["known"].(string);if !ok{return fmt.Errorf("known")};p.Known=known;p.Extras=map[string]string{};for k,v:=range value{s,ok:=v.(string);if !ok{return fmt.Errorf("extra")};if k!="known"{p.Extras[k]=s}};return nil}
func(p Payload)MarshalJSON()([]byte,error){m:=map[string]string{"known":p.Known};for k,v:=range p.Extras{m[k]=v};return json.Marshal(m)}
`
	t.Run("preserved", func(t *testing.T) { runOracleFixture(t, r, code, true) })
	t.Run("dropped_extra", func(t *testing.T) {
		runOracleFixture(t, r, strings.Replace(code, `for k,v:=range p.Extras{m[k]=v};`, "", 1), false)
	})
}

func TestOpenAndClosedUnionRuntimeFixtures(t *testing.T) {
	open := request(`{"anyOf":[{"type":"string","enum":["known"]},{"type":"object","properties":{"known":{"type":"string"}},"required":["known"],"additionalProperties":false},{"type":["object","string"]}]}`)
	closed := request(`{"oneOf":[{"type":"string","enum":["known"]},{"type":"object","properties":{"known":{"type":"string"}},"required":["known"],"additionalProperties":false}]}`)
	code := `package fixture
import("encoding/json";"fmt")
type Payload json.RawMessage
func(p *Payload)UnmarshalJSON(b []byte)error{var v any;if err:=json.Unmarshal(b,&v);err!=nil{return err};switch v.(type){case string,map[string]any:default:return fmt.Errorf("invalid")};CHECK;*p=append((*p)[:0],b...);return nil}
func(p Payload)MarshalJSON()([]byte,error){return []byte(p),nil}
`
	permissive := strings.Replace(code, "CHECK", "", 1)
	strict := strings.Replace(code, "CHECK", `switch x:=v.(type){case string:if x!="known"{return fmt.Errorf("enum")};case map[string]any:if _,ok:=x["known"].(string);!ok||len(x)!=1{return fmt.Errorf("known")}}`, 1)
	t.Run("open_correct", func(t *testing.T) { runOracleFixture(t, open, permissive, true) })
	t.Run("open_lossy", func(t *testing.T) {
		runOracleFixture(t, open, strings.Replace(permissive, "return []byte(p),nil", `var v any;_ = json.Unmarshal(p,&v);if _,ok:=v.(map[string]any);ok{return []byte("{}"),nil};return []byte(p),nil`, 1), false)
	})
	t.Run("closed_correct", func(t *testing.T) { runOracleFixture(t, closed, strict, true) })
	t.Run("closed_weakened", func(t *testing.T) { runOracleFixture(t, closed, permissive, false) })
}

func TestAggregateUnionKeepsEveryBoundedBranch(t *testing.T) {
	branches := []any{}
	for i := 0; i < 2100; i++ {
		branches = append(branches, map[string]any{"const": fmt.Sprintf("branch_%d", i)})
	}
	raw, _ := json.Marshal(map[string]any{"anyOf": branches})
	targets, err := collectTargets(request(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range targets[0].Probes {
		if p.Valid {
			count++
		}
	}
	if count != 2100 {
		t.Fatalf("union branches truncated: %d", count)
	}
}

type cancelAfterChecks struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelAfterChecks) Err() error {
	c.checks++
	if c.checks == 4 {
		c.cancel()
	}
	return c.Context.Err()
}
func TestConstructionHonorsExistingRepairCancellation(t *testing.T) {
	for _, during := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		req := request(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)
		if during {
			req.Context = &cancelAfterChecks{Context: ctx, cancel: cancel}
		} else {
			cancel()
			req.Context = ctx
		}
		_, err := collectStream(req)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("during=%t: cancelled construction returned %v", during, err)
		}
	}
}
