package schemaoracle

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestWalkStopsAtConsumerFailure(t *testing.T) {
	sentinel := errors.New("consumer stopped")
	calls := 0
	err := Walk(request(`{"type":"object","properties":{"first":{"type":"string"},"second":{"type":"integer"}},"required":["first"]}`), func(target Target, p *Probe) error {
		calls++
		if p != nil {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) || calls != 2 {
		t.Fatalf("incremental consumer failure lost: %v calls=%d", err, calls)
	}
}

func TestFramingAndIncompleteExpectations(t *testing.T) {
	for _, wire := range []string{"{", "{}", "{} {}\n", "{\"unknown\":true}\n"} {
		if err := ReadFrame(bufio.NewReader(strings.NewReader(wire)), new(record)); err == nil {
			t.Fatalf("accepted incomplete/unknown frame %q", wire)
		}
	}
	// The framing limit applies even when the producer never emits a delimiter.
	if err := ReadFrame(bufio.NewReader(io.LimitReader(zeroReader{}, maxFrameBytes+1)), new(record)); err == nil || !strings.Contains(err.Error(), "framing bound") {
		t.Fatal(err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestRawInputNumberSpellingAndIndependentVerdicts(t *testing.T) {
	original := `{"large":123456789012345678901234567890,"exponent":1e1000,"negative":-0,"escaped":"<>&\u2028\"\\"}`
	var frame bytes.Buffer
	if err := writeFrame(&frame, decoderInput{Input: original}); err != nil {
		t.Fatal(err)
	}
	var q decoderInput
	if err := ReadFrame(bufio.NewReader(&frame), &q); err != nil || q.Input != original {
		t.Fatalf("input spelling changed: %v %s", err, q.Input)
	}
	req := Request{Schemas: map[string][]byte{"A.json": []byte(`{"type":"boolean"}`), "B.json": []byte(`{"const":true}`)}, Bindings: map[string]string{"A.json": "A", "B.json": "B"}}
	verdicts := map[string]bool{}
	if err := Walk(req, func(target Target, p *Probe) error {
		if p != nil && p.JSON == "false" {
			verdicts[target.Schema] = p.Valid
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !verdicts["A.json"] || verdicts["B.json"] {
		t.Fatal("per-target verdicts merged", verdicts)
	}
}

func TestRoundtripExactNumbersAndPresence(t *testing.T) {
	for _, tc := range []struct {
		a, b  string
		equal bool
	}{
		{`1e1000`, `10e999`, true}, {`-0`, `0.0`, true},
		{`9007199254740993`, `9007199254740992`, false},
		{`{"x":null}`, `{}`, false}, {`{"a":"one","b":"two"}`, `{"a":"two","b":"one"}`, false},
	} {
		if got := roundtrip(Target{}, tc.a, tc.b) == nil; got != tc.equal {
			t.Fatalf("%s -> %s accepted=%t", tc.a, tc.b, got)
		}
	}
}

func TestOpenRecordLayoutDoesNotReplaceWireEvidence(t *testing.T) {
	r := request(`{"type":"object","properties":{"known":{"type":"string"}},"required":["known"],"additionalProperties":{"type":"string"}}`)
	const code = `package fixture
import("encoding/json";"fmt")
type Payload struct{Known string;DynamicProperties map[string]string}
func(p *Payload)UnmarshalJSON(b []byte)error{var m map[string]any;if json.Unmarshal(b,&m)!=nil||m==nil{return fmt.Errorf("object")};if _,ok:=m["known"].(string);!ok{return fmt.Errorf("known")};p.DynamicProperties=map[string]string{};for k,v:=range m{s,ok:=v.(string);if !ok{return fmt.Errorf("value")};p.DynamicProperties[k]=s};return nil}
func(p Payload)MarshalJSON()([]byte,error){DROP;return json.Marshal(p.DynamicProperties)}
`
	t.Run("dynamic representation", func(t *testing.T) { runOracleFixture(t, r, strings.Replace(code, "DROP", "", 1), true) })
	t.Run("dynamic value lost", func(t *testing.T) {
		runOracleFixture(t, r, strings.Replace(code, "DROP", `delete(p.DynamicProperties,"__oracle_map_entry__")`, 1), false)
	})
}

func TestOversizedFailureEvidenceIsExplicitlyIncomplete(t *testing.T) {
	raw := `{"type":"string","const":"` + strings.Repeat("x", 8<<20) + `"}`
	out := runOracleFixture(t, request(raw), `package fixture
import "encoding/json"
type Payload struct{Raw json.RawMessage}
func(p *Payload)UnmarshalJSON(b []byte)error{p.Raw=append(p.Raw[:0],b...);return nil}
func(p Payload)MarshalJSON()([]byte,error){if len(p.Raw)>8{p.Raw[len(p.Raw)-2]='y'};return p.Raw,nil}
`, false)
	if !strings.Contains(string(out), "failure diagnostics incomplete") {
		t.Fatalf("oversized evidence did not fail closed: %.1000s", out)
	}
}
