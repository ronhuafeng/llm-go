package schemaoracle

import (
	"bytes"
	"testing"
)

func TestExpectedInputCannotBeMutatedByCandidateDecoder(t *testing.T) {
	r := request(`{"type":"string","const":"abc"}`)
	output := runOracleFixture(t, r, `package fixture
 import("encoding/json";"fmt")
 type Payload string
 func(p *Payload)UnmarshalJSON(b []byte)error{if string(b)!=fmt.Sprintf("%q","abc"){return fmt.Errorf("invalid")};b[1]='z';type plain Payload;return json.Unmarshal(b,(*plain)(p))}
 `, false)
	if !bytes.Contains(output, []byte("wire value lost")) {
		t.Fatalf("did not reject input mutation: %s", output)
	}
}
