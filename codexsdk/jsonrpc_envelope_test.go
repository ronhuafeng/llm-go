package codexsdk

import (
	"encoding/json"
	"strings"
	"testing"
)

// Cases grounded in the bounded source-derived Rust probe described in
// docs/protocol-sync.md. SDK JSON policies are tested separately below.
func TestDecodeJSONRPCEnvelopeOrderedMatch(t *testing.T) {
	for _, tc := range []struct {
		line    string
		variant jsonRPCVariant
	}{
		{`{"id":1,"method":"m"}`, jsonRPCRequest},
		{`{"method":"m"}`, jsonRPCNotification},
		{`{"id":1,"result":null}`, jsonRPCResponse},
		{`{"id":1,"error":{"code":-1,"message":"e"}}`, jsonRPCError},
		{`{"id":1,"method":"m","result":2,"error":{}}`, jsonRPCRequest},
		{`{"id":1,"result":2,"error":{"code":-1,"message":"e"}}`, jsonRPCResponse},
		{`{"id":1,"result":null,"error":false}`, jsonRPCResponse},
		{`{"id":null,"method":"m"}`, jsonRPCNotification},
		{`{"id":{},"method":"m"}`, jsonRPCNotification},
		{`{"id":1e0,"method":"m"}`, jsonRPCNotification},
		{`{"id":-0,"method":"m"}`, jsonRPCNotification},
		{`{"id":9223372036854775808,"method":"m"}`, jsonRPCNotification},
		{`{"id":1,"method":"m","trace":{"traceparent":2}}`, jsonRPCNotification},
		{`{"id":1,"method":"m","trace":42}`, jsonRPCNotification},
		{`{"id":1,"method":null,"result":3}`, jsonRPCResponse},
		{`{"id":1,"method":2,"error":{"code":-1,"message":"e"}}`, jsonRPCError},
		{`{"method":"m","params":null}`, jsonRPCNotification},
		{`{"method":"m","params":[1]}`, jsonRPCNotification},
		{`{"id":1,"method":"m","trace":{"unknown":1,"Traceparent":42}}`, jsonRPCRequest},
		{`{"id":1,"method":"m","trace":{"traceparent":null,"tracestate":"s","extra":false}}`, jsonRPCRequest},
		{`{"id":9223372036854775807,"result":null}`, jsonRPCResponse},
		{`{"id":-9223372036854775808,"result":null}`, jsonRPCResponse},
		{`{"id":1,"result":1e100}`, jsonRPCResponse},
		{`{"id":1,"error":{"code":-1,"message":"e","extra":true}}`, jsonRPCError},
	} {
		t.Run(tc.line, func(t *testing.T) {
			got, err := decodeJSONRPCEnvelope([]byte(tc.line))
			if err != nil || got.variant != tc.variant {
				t.Fatalf("variant=%v err=%v, want %v", got.variant, err, tc.variant)
			}
			if tc.variant == jsonRPCNotification {
				if _, exists := got.payload["id"]; exists {
					t.Fatal("ignored ID became semantic authority")
				}
			}
			if tc.variant == jsonRPCResponse && got.payload["error"] != nil {
				t.Fatal("ignored error became semantic authority")
			}
		})
	}
}

func TestDecodeJSONRPCEnvelopeNoMatchingBranch(t *testing.T) {
	for _, line := range []string{
		`{}`, `{"id":1}`, `{"id":1,"method":null}`, `{"id":null,"result":null}`,
		`{"id":9223372036854775808,"result":null}`, `{"id":-9223372036854775809,"result":null}`,
		`{"id":1e0,"result":null}`, `{"id":-0,"result":null}`, `{"id":1.0,"result":null}`,
		`{"id":1,"error":{"message":"e"}}`, `{"id":1,"error":{"CODE":1,"MESSAGE":"e"}}`, `{"id":1,"error":{"code":null,"message":"e"}}`,
		`{"id":1,"error":{"code":1,"message":null}}`, `{"id":1,"error":{"code":1.0,"message":"e"}}`, `{"id":1,"error":{"code":-0,"message":"e"}}`,
	} {
		if _, err := decodeJSONRPCEnvelope([]byte(line)); err == nil {
			t.Fatalf("accepted %s", line)
		}
	}
}

func TestDecodeJSONRPCEnvelopeRetainsSDKJSONPolicies(t *testing.T) {
	// Serde accepts some struct sequences and duplicate keys. The SDK retains
	// object structs and rejects duplicate keys even in ignored envelope fields.
	for _, line := range []string{
		`[1,"m",null,null]`, `["m",null]`, `[1,2]`, `[{"code":-1,"message":"e"},1]`,
		`{"id":1,"error":[-1,null,"e"]}`,
		`{"id":1,"id":2,"method":"m"}`,
		`{"id":1,"result":{"stdout_secret":1,"stdout_secret":2}}`,
		`{"method":"m","ignored":{"stdout_secret":1,"stdout_secret":2}}`,
		`{"method":"m","trace":{"traceparent":"a","traceparent":"b"}}`,
		`{"stdout_secret":`, `{"id":1,"result":{}} {}`,
	} {
		_, err := decodeJSONRPCEnvelope([]byte(line))
		if err == nil || !strings.HasPrefix(err.Error(), "decode JSONRPCMessage:") || strings.Contains(err.Error(), "stdout_secret") {
			t.Fatalf("unsafe acceptance/diagnostic: %v", err)
		}
	}
	// Sequence trace structs stay outside the SDK's object-struct policy. They
	// fail Request matching but may fall back to Notification.
	envelope, err := decodeJSONRPCEnvelope([]byte(`{"id":1,"method":"m","trace":[null,null]}`))
	if err != nil || envelope.variant != jsonRPCNotification {
		t.Fatalf("trace sequence: %+v %v", envelope, err)
	}
}

func ingestTestJSONRPC(t *testing.T, client *Client, value map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ingestJSONRPCLine(raw); err != nil {
		t.Fatal(err)
	}
}
