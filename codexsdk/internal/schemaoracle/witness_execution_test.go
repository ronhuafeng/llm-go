package schemaoracle

import (
	"strings"
	"testing"
)

const itemWitnessSchema = `{"oneOf":[{"type":"object","properties":{"method":{"type":"string","enum":["fixture/item"]},"params":{"type":"object","properties":{"item":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}},"required":["item"]}},"required":["method","params"]}]}`

const itemWitnessDecoder = `package fixture
import("encoding/json";"fmt")
type Payload map[string]any
func(p *Payload)UnmarshalJSON(b []byte)error{var root map[string]any;if json.Unmarshal(b,&root)!=nil||len(root)!=2||root["method"]!="fixture/item"{return fmt.Errorf("envelope")};params,ok:=root["params"].(map[string]any);if !ok||len(params)!=1{return fmt.Errorf("params")};item,present:=params["item"];if !present{return fmt.Errorf("item")};CHECK_ITEM;*p=root;return nil}
`

func TestIndependentWitnessFailuresExecuteTogether(t *testing.T) {
	r := request(itemWitnessSchema)
	goodCheck := `object,ok:=item.(map[string]any);if !ok||len(object)!=1{return fmt.Errorf("item object")};if _,ok:=object["id"].(string);!ok{return fmt.Errorf("id")}`
	good := strings.Replace(itemWitnessDecoder, "CHECK_ITEM", goodCheck, 1)
	t.Run("valid_decoder", func(t *testing.T) { runOracleFixture(t, r, good, true) })
	// Three independent decoder defects in the same contract: each must be
	// reported by a single compiled generated test process, not successive runs.
	bad := strings.Replace(itemWitnessDecoder, "CHECK_ITEM", `raw,_:=json.Marshal(item);if string(raw)!="null"&&string(raw)!="false"&&string(raw)!="{}"{`+goodCheck+`}`, 1)
	out := runOracleFixture(t, r, bad, false)
	for _, value := range []string{"null", "false", "{}"} {
		witness := "value=" + map[string]string{"null": "null", "false": "false", "{}": "<object members=0>"}[value]
		// The native assertion must carry the exact frozen input as well as its
		// source diagnostic, so an independently replayed challenge can identify
		// the failed witness without trusting decoder-authored output.
		input := `{"method":"fixture/item","params":{"item":` + value + `}}`
		if !strings.Contains(string(out), "schema-invalid input accepted: "+input+": ") || !strings.Contains(string(out), witness) {
			t.Errorf("missing executed witness %s in one process:\n%s", witness, out)
		}
	}
	// Count the distinct failed probe tests, so echoed fixture data or a
	// contract-level summary cannot stand in for executed independent evidence.
	failures := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "--- FAIL:") && strings.Contains(line, "/probe_") {
			failures++
		}
	}
	if failures != 3 {
		t.Fatalf("executed failed probes=%d, want 3:\n%s", failures, out)
	}
}

func TestFieldFailureDoesNotSuppressWireWitnesses(t *testing.T) {
	r := request(`{"type":"object","properties":{"item":{"type":"string"}},"required":["item"]}`)
	// Wrong declared field set and an independent permissive decoder must both
	// be observed. A field-layout failure is not grounds to skip runtime probes.
	bad := `package fixture
import "encoding/json"
type Payload struct{Extra string ` + "`json:\"extra\"`" + `;Raw json.RawMessage ` + "`json:\"-\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{p.Raw=append(p.Raw[:0],b...);return nil}
func(p Payload)MarshalJSON()([]byte,error){return p.Raw,nil}
`
	out := runOracleFixture(t, r, bad, false)
	if !strings.Contains(string(out), "schema fields=[item] Go JSON fields=[extra]") || !strings.Contains(string(out), "schema-invalid input accepted:") {
		t.Fatalf("field failure suppressed runtime evidence:\n%s", out)
	}
}
