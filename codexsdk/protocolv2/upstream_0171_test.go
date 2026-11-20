package protocolv2

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Expectations come from the unmodified schemas generated at rust-v0.171.0,
// ccf9be8cc458bd6b8336fe84d68d8893f2691a5e, plus the closed-record public overlay.
func TestAgentMessageDisplay0171SourceContract(t *testing.T) {
	source := compile0169Definition(t, "ThreadReadResponse.json", "AgentMessageDisplay")
	for _, tc := range []struct {
		wire           string
		source, public bool
	}{
		{`{"mode":"show","version":1,"trustedContacts":"check"}`, true, true},
		{`{"mode":"show","version":1,"trustedContacts":"skip","annotation":null}`, true, true},
		{`{"mode":"show","version":1,"trustedContacts":"skip","annotation":{"kind":"wellbeingHelpline","subject":"answer"}}`, true, true},
		{`{"mode":"hide","version":1,"replacement":{"kind":"wellbeingPromptHelpline"}}`, true, true},
		{`{"mode":"block","version":1,"reason":{"kind":"protection","protectionType":"row"}}`, true, true},
		{`{"mode":"block","version":1,"reason":{"kind":"cyber","trustedAccess":null}}`, true, true},
		{`{"mode":"show","version":1,"trustedContacts":"skip","future":true}`, true, false},
		{`{"mode":"show","version":2,"trustedContacts":"check"}`, false, false},
		{`{"mode":"show","version":0,"trustedContacts":"check"}`, false, false},
		{`{"mode":"show","version":"1","trustedContacts":"check"}`, false, false},
		{`{"mode":"show","version":1}`, false, false},
		{`{"mode":"hide","version":1}`, false, false},
		{`{"mode":"block","version":1,"reason":{"kind":"other"}}`, false, false},
		{`{"action":"show"}`, false, false},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			want := decode0167JSON(t, []byte(tc.wire))
			if valid := source.Validate(want) == nil; valid != tc.source {
				t.Fatalf("source validity=%v want=%v", valid, tc.source)
			}
			var value AgentMessageDisplay
			if err := json.Unmarshal([]byte(tc.wire), &value); (err == nil) != tc.public {
				t.Fatalf("public validity=%v want=%v: %v", err == nil, tc.public, err)
			}
			if tc.public {
				out, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(decode0167JSON(t, out), want) {
					t.Fatalf("wire changed: %s became %s", tc.wire, out)
				}
			}
		})
	}
	// Check encoding independently of decoding: invalid programmatic versions
	// cannot escape through the tagged union constructors.
	for _, version := range []uint8{0, 2, 255} {
		value := NewAgentMessageDisplayShow(AgentMessageDisplayShow{Version: version, TrustedContacts: TrustedContactsCheck})
		if raw, err := json.Marshal(value); err == nil {
			t.Fatalf("invalid version %d encoded: %s", version, raw)
		}
	}
}

func TestSearchCancellation0171SourceContract(t *testing.T) {
	source := compile0167Source(t, "ThreadSearchCancelParams.json")
	for _, tc := range []struct {
		wire  string
		valid bool
	}{
		{`{"requestId":9007199254740993}`, true},
		{`{"requestId":"search-1"}`, true},
		{`{}`, false}, {`{"requestId":null}`, false}, {`{"requestId":1.5}`, false},
	} {
		want := decode0167JSON(t, []byte(tc.wire))
		if valid := source.Validate(want) == nil; valid != tc.valid {
			t.Fatalf("source validity=%v want=%v: %s", valid, tc.valid, tc.wire)
		}
		var value ThreadSearchCancelParams
		if err := json.Unmarshal([]byte(tc.wire), &value); (err == nil) != tc.valid {
			t.Fatalf("public validity=%v want=%v: %v", err == nil, tc.valid, err)
		}
		if tc.valid {
			out, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decode0167JSON(t, out), want) {
				t.Fatalf("wire changed: %s became %s", tc.wire, out)
			}
		}
	}
}
