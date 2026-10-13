package protocolv2

import (
	"encoding/json"
	"testing"
)

func TestCollectionMembersRetainSourceNullability(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		valid  string
		target any
	}{
		{`{"experimentalApi":true,"optOutNotificationMethods":[null]}`, `{"experimentalApi":true,"optOutNotificationMethods":["turn/started"]}`, &InitializeCapabilities{}},
		{`{"enablement":{"flag":null}}`, `{"enablement":{"flag":true}}`, &ExperimentalFeatureEnablementSetParams{}},
		{`{"threadId":"t","outputModality":"audio","codexResponseHandoffChannelPrefixes":{"channel":null}}`, `{"threadId":"t","outputModality":"audio","codexResponseHandoffChannelPrefixes":{"channel":["prefix"]}}`, &ThreadRealtimeStartParams{}},
	} {
		if err := json.Unmarshal([]byte(tc.valid), tc.target); err != nil {
			t.Fatalf("rejected valid collection: %s: %v", tc.valid, err)
		}
		if err := json.Unmarshal([]byte(tc.raw), tc.target); err == nil {
			t.Fatalf("accepted non-nullable member: %s", tc.raw)
		}
	}
	for _, raw := range []string{`null`, `""`, `false`, `3`} {
		var value ReasoningEffort
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			t.Fatalf("accepted effort %s", raw)
		}
	}
	for _, raw := range []string{`"high"`, `"未来"`} {
		var value ReasoningEffort
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if _, err := json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := json.Marshal(ReasoningEffort("")); err == nil {
		t.Fatal("encoded effort below source minLength")
	}
	var opaque map[string]JSONValue
	if err := wireMapDecoder(decodeWireValue[JSONValue])([]byte(`{"value":null}`), &opaque, wireDecodeClosed); err != nil {
		t.Fatal(err)
	}
}
