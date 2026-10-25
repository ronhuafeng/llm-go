package codexsdk

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The official App Server serializes typed Request, Notification, Response and
// Error variants; normal output must travel through the same decoder as the
// SDK's existing strict envelope checks.
func TestJSONRPCEnvelopeDecodedOnceForDispatch(t *testing.T) {
	cases := []struct {
		name string
		line string
		kind jsonRPCEnvelopeKind
		want map[string]any
	}{
		{"request", `{"id":9007199254740993,"method":"item/tool/call","params":{"arguments":{"n":9007199254740993},"callId":"one"},"trace":null,"future":{"a":1}}`, jsonRPCRequest,
			map[string]any{"id": json.Number("9007199254740993"), "method": "item/tool/call", "params": map[string]any{"arguments": map[string]any{"n": json.Number("9007199254740993")}, "callId": "one"}, "trace": nil, "future": map[string]any{"a": json.Number("1")}}},
		{"notification", `{"method":"turn/completed","params":{"turn":{"id":"t"}},"meta":{"version":2},"optional":null}`, jsonRPCNotification,
			map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "t"}}, "meta": map[string]any{"version": json.Number("2")}, "optional": nil}},
		{"response", `{"id":"go-sdk-1","result":{"ok":true},"meta":"extra"}`, jsonRPCResponse,
			map[string]any{"id": "go-sdk-1", "result": map[string]any{"ok": true}, "meta": "extra"}},
		{"error", `{"id":42,"error":{"code":-32603,"message":"failed","data":{"num":9007199254740993}},"meta":false}`, jsonRPCError,
			map[string]any{"id": json.Number("42"), "error": map[string]any{"code": json.Number("-32603"), "message": "failed", "data": map[string]any{"num": json.Number("9007199254740993")}}, "meta": false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := decodeJSONRPCEnvelope([]byte(tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if frame.kind != tc.kind || !reflect.DeepEqual(frame.message, tc.want) {
				t.Fatalf("kind=%v message=%#v; want kind=%v message=%#v", frame.kind, frame.message, tc.kind, tc.want)
			}
			if err := validateJSONRPCEnvelope([]byte(tc.line)); err != nil {
				t.Fatalf("acceptance path differs: %v", err)
			}
		})
	}
}

func TestJSONRPCEnvelopeRejectsBeforeRouting(t *testing.T) {
	for _, tt := range []struct{ name, line, reason string }{
		{"ambiguous", `{"id":1,"method":"item/tool/call","result":{}}`, "expected exactly one JSON-RPC envelope shape"},
		{"invalid-id", `{"id":null,"method":"item/tool/call"}`, "null is not allowed"},
		{"invalid-trace", `{"id":1,"method":"item/tool/call","trace":42}`, "JSONRPCRequest.trace"},
		{"duplicate-key", `{"id":1,"id":2,"result":{}}`, "duplicate object key"},
		{"duplicate-result", `{"id":1,"result":{"v":1,"v":2}}`, "invalid JSON value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := decodeJSONRPCEnvelope([]byte(tt.line))
			if err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("frame=%#v error=%v; expected %s", frame, err, tt.reason)
			}
			if validErr := validateJSONRPCEnvelope([]byte(tt.line)); validErr == nil || validErr.Error() != err.Error() {
				t.Fatalf("different error from validation path: %v vs %v", validErr, err)
			}
		})
	}
}
