package schemaoracle

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func request(schema string) Request {
	return Request{Schemas: map[string][]byte{"target.json": []byte(schema)}, Bindings: map[string]string{"target.json": "Payload"}, Package: "fixture", CloseObjects: true}
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
			source, e := Generate(request(tt.schema))
			if e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			for name, body := range map[string]string{"go.mod": "module fixture\ngo 1.26.8\n", "payload.go": candidate, "oracle_test.go": string(source)} {
				if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
					t.Fatal(e)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", ".")
			cmd.Dir = dir
			out, e := cmd.CombinedOutput()
			requireOracleOutcome(t, out, e, tt.wantPass)
		})
	}
}

func TestOpenUnionClassifiesMutationsWithWholeSchema(t *testing.T) {
	req := request(`{"definitions":{"Error":{"anyOf":[{"type":"object","properties":{"known":{"type":"integer"}},"required":["known"],"additionalProperties":false},{"type":["object","string"]}]}},"type":"null"}`)
	req.Bindings = map[string]string{"target.json#/definitions/Error": "Payload"}
	got, e := Build(req)
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
		ts, e := Build(r)
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
		if _, e := Generate(request(schema)); e == nil {
			t.Fatalf("accepted unsupported %s", schema)
		}
	}
	r := request(`{"type":"null"}`)
	r.Bindings["target.json"] = ""
	if _, e := Generate(r); e == nil {
		t.Fatal("missing binding accepted")
	}
}
func TestDistinctFieldsAndDeterminism(t *testing.T) {
	r := request(`{"type":"object","properties":{"first":{"type":"string"},"second":{"type":"string"}},"required":["first","second"]}`)
	a, e := Generate(r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Generate(r)
	if e != nil {
		t.Fatal(e)
	}
	if string(a) != string(b) {
		t.Fatal("nondeterministic oracle")
	}
	ts, _ := Build(r)
	var obj map[string]any
	_ = json.Unmarshal([]byte(ts[0].Probes[0].JSON), &obj)
	if obj["first"] == obj["second"] {
		t.Fatal("fields indistinguishable")
	}
}

// Execute only locally authored fixtures. A candidate's vacuous own test cannot
// neutralize the independently frozen corpus. No archived candidate is run.
func TestFrozenRuntimeDetectsLostFieldsAndWeakValidation(t *testing.T) {
	r := request(`{"type":"object","properties":{"first":{"type":"string"},"second":{"type":"string"}},"required":["first","second"]}`)
	test, e := Generate(r)
	if e != nil {
		t.Fatal(e)
	}
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
			dir := t.TempDir()
			for name, b := range map[string][]byte{"go.mod": []byte("module fixture\ngo 1.26.8\n"), "payload.go": []byte(tt.code), "oracle_test.go": test, "weak_test.go": []byte("package fixture;import \"testing\";func TestCandidateClaim(t *testing.T){}")} {
				if e := os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", ".")
			cmd.Dir = dir
			out, e := cmd.CombinedOutput()
			requireOracleOutcome(t, out, e, tt.pass)
		})
	}
}

func TestCompoundObjectAndRecursiveBaseCases(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"type":{"const":"image"},"detail":{"type":["string","null"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]}`,
		`{"$ref":"#/definitions/Node","definitions":{"Node":{"type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"#/definitions/Node"}}},"required":["name","children"]}}}`,
	} {
		if _, e := Build(request(schema)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := Build(request(`{"$ref":"#/definitions/Node","definitions":{"Node":{"type":"object","properties":{"child":{"$ref":"#/definitions/Node"}},"required":["child"]}}}`)); e == nil {
		t.Fatal("uninhabited required recursive shape accepted")
	}
}

func TestExternalProbeCannotBeSuppressedByCandidateTestMain(t *testing.T) {
	r := request(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)
	r.Package = "schemaoracleprobe"
	r.ImportPath = "fixture/subject"
	source, e := Generate(r)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	for _, p := range []string{"subject", "probe"} {
		if e := os.Mkdir(filepath.Join(dir, p), 0700); e != nil {
			t.Fatal(e)
		}
	}
	for name, body := range map[string]string{"go.mod": "module fixture\ngo 1.26.8\n", "subject/payload.go": "package subject;type Payload struct { Value string `json:\"value\"`;Stale string `json:\"removed,omitempty\"` }", "subject/payload_test.go": "package subject;import(\"testing\";\"os\");func TestMain(m *testing.M){os.Exit(0)}", "probe/oracle_test.go": string(source)} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./probe")
	cmd.Dir = dir
	out, e := cmd.CombinedOutput()
	if e == nil || !strings.Contains(string(out), "Go JSON fields=[removed value]") {
		t.Fatalf("external frozen field proof suppressed or failed incorrectly: %v\n%s", e, out)
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
	targets, e := Build(request(string(raw)))
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
	targets, e := Build(r)
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

func TestCompressedCorpusPreservesEveryProbe(t *testing.T) {
	r := request(`{"type":"object","properties":{"first":{"type":"string"},"second":{"anyOf":[{"type":"integer"},{"type":"null"}]}},"required":["first"]}`)
	targets, e := Build(r)
	if e != nil {
		t.Fatal(e)
	}
	source, e := Generate(r)
	if e != nil {
		t.Fatal(e)
	}
	line := strings.SplitN(strings.SplitN(string(source), "const frozenCorpus = ", 2)[1], "\n", 2)[0]
	encoded, e := strconv.Unquote(line)
	if e != nil {
		t.Fatal(e)
	}
	compressed, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := gzip.NewReader(bytes.NewReader(compressed))
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	var corpus [][]Probe
	if e := json.NewDecoder(reader).Decode(&corpus); e != nil {
		t.Fatal(e)
	}
	if len(corpus) != len(targets) || !reflect.DeepEqual(corpus[0], targets[0].Probes) {
		t.Fatal("compression changed frozen expectations")
	}
}

func TestCompoundRootPreservesLocalDefinitionResources(t *testing.T) {
	r := request(`{"$schema":"http://json-schema.org/draft-07/schema#","definitions":{"Payload":{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}},"properties":{"emittedAt":{"type":"integer"}},"oneOf":[{"type":"object","properties":{"method":{"const":"event"},"params":{"$ref":"#/definitions/Payload"}},"required":["method","params"]}]}`)
	if _, err := Build(r); err != nil {
		t.Fatal(err)
	}
}

func runOracleFixture(t *testing.T, r Request, implementation string, wantPass bool) []byte {
	t.Helper()
	source, err := Generate(r)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture\ngo 1.26.8\n", "payload.go": implementation, "oracle_test.go": string(source)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	requireOracleOutcome(t, output, err, wantPass)
	return output
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
		if _, err := Build(request(schema)); err != nil {
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
	targets, err := Build(r)
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
	targets, err := Build(request(string(raw)))
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
		_, err := Generate(req)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("during=%t: cancelled construction returned %v", during, err)
		}
	}
}

func TestGeneratedCasesStayInsideOneContractTest(t *testing.T) {
	req := request(`{"type":"object","properties":{"one":{"type":"string"},"two":{"type":"boolean"}},"required":["one"]}`)
	targets, err := Build(req)
	if err != nil || len(targets[0].Probes) < 10 {
		t.Fatalf("corpus %v", err)
	}
	source, err := Generate(req)
	if err != nil {
		t.Fatal(err)
	}
	// Keep a single trusted parent per source contract, but execute each
	// witness independently. Text counts cannot establish actual execution.
	implementation := `package fixture
import("bytes";"encoding/json";"fmt")
type Payload struct{One string ` + "`json:\"one\"`" + `;Two *bool ` + "`json:\"two,omitempty\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var root map[string]json.RawMessage;if json.Unmarshal(b,&root)!=nil||root["one"]==nil{return fmt.Errorf("one required")};for key,value:=range root{if key!="one"&&key!="two"||bytes.Equal(value,[]byte("null")){return fmt.Errorf("field")}};type plain Payload;return json.Unmarshal(b,(*plain)(p))}
`
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fixture\ngo 1.26.8\n", "payload.go": implementation, "oracle_test.go": string(source)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-json", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	requireOracleOutcome(t, out, err, true)
	ran, passed := map[string]int{}, map[string]int{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event struct{ Action, Test string }
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		switch event.Action {
		case "run":
			ran[event.Test]++
		case "pass":
			if event.Test != "" {
				passed[event.Test]++
			}
		}
	}
	parent := "TestTrustedSchemaOracle/target.json"
	names := []string{"TestTrustedSchemaOracle", parent, parent + "/fields"}
	for i := range targets[0].Probes {
		names = append(names, parent+"/probe_"+strconv.Itoa(i))
	}
	if len(ran) != len(names) || len(passed) != len(names) {
		t.Fatalf("executed tests=%d passed=%d expected=%d", len(ran), len(passed), len(names))
	}
	for _, name := range names {
		if ran[name] != 1 || passed[name] != 1 {
			t.Errorf("%s executed=%d passed=%d, want once each", name, ran[name], passed[name])
		}
	}
}

func TestStreamingCorpusMatchesOriginalBytes(t *testing.T) {
	targets := []Target{{Probes: []Probe{{JSON: `{"text":"<quoted>\\\""}`, Valid: true}, {JSON: `null`, Valid: false}}}, {Probes: nil}, {Probes: []Probe{}}}
	compressed, err := compressCorpus(context.Background(), targets)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	original := make([][]Probe, len(targets))
	for i, target := range targets {
		original[i] = target.Probes
	}
	want, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("stream changed corpus: %s != %s", raw, want)
	}
}

func requireOracleOutcome(t *testing.T, output []byte, err error, wantPass bool) {
	t.Helper()
	if (err == nil) != wantPass {
		t.Fatalf("wantPass=%t err=%v\n%s", wantPass, err, output)
	}
	if !wantPass && !bytes.Contains(output, []byte("--- FAIL: TestTrustedSchemaOracle")) {
		t.Fatalf("negative fixture did not reach a contract assertion: %v\n%s", err, output)
	}
}
