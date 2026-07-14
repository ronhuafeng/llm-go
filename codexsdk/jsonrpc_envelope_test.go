package codexsdk

import (
	"strings"
	"testing"
)

func TestValidateJSONRPCEnvelopeAcceptsCanonicalShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{
			name: "request",
			line: `{"id":"go-sdk-1","method":"turn/start","params":{"threadId":"thread-1"},"trace":null}`,
		},
		{
			name: "notification",
			line: `{"method":"initialized"}`,
		},
		{
			name: "response",
			line: `{"id":"go-sdk-1","result":{"ok":true}}`,
		},
		{
			name: "error",
			line: `{"id":42,"error":{"code":-32603,"data":null,"message":"boom"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateJSONRPCEnvelope([]byte(tc.line)); err != nil {
				t.Fatalf("validateJSONRPCEnvelope rejected %s: %v", tc.name, err)
			}
		})
	}
}

func TestValidateJSONRPCEnvelopeAcceptsExtraMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{
			name: "request",
			line: `{"id":"go-sdk-1","method":"turn/start","params":{"threadId":"thread-1"},"trace":null,"meta":{"source":"test"}}`,
		},
		{
			name: "notification",
			line: `{"method":"initialized","meta":{"source":"test"}}`,
		},
		{
			name: "response",
			line: `{"id":"go-sdk-1","result":{"ok":true},"meta":{"source":"test"}}`,
		},
		{
			name: "error",
			line: `{"id":42,"error":{"code":-32603,"data":null,"message":"boom","meta":{"source":"test"}},"meta":{"source":"test"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateJSONRPCEnvelope([]byte(tc.line)); err != nil {
				t.Fatalf("validateJSONRPCEnvelope rejected extra %s metadata: %v", tc.name, err)
			}
		})
	}
}

func TestValidateJSONRPCEnvelopeRejectsMalformedProtocol(t *testing.T) {
	for _, tc := range []struct {
		name    string
		line    string
		wantErr string
	}{
		{
			name:    "ambiguous request response",
			line:    `{"id":"go-sdk-1","method":"turn/start","result":{}}`,
			wantErr: "decode JSONRPCMessage: expected exactly one JSON-RPC envelope shape",
		},
		{
			name:    "duplicate top-level key",
			line:    `{"id":"go-sdk-1","id":"go-sdk-2","result":{"ok":true}}`,
			wantErr: `decode JSONRPCMessage: duplicate object key "id"`,
		},
		{
			name:    "error missing nested code",
			line:    `{"id":1,"error":{"message":"boom"}}`,
			wantErr: "decode JSONRPCMessage.error: decode JSONRPCErrorError.code: missing required field",
		},
		{
			name:    "request trace wrong shape",
			line:    `{"id":"go-sdk-1","method":"turn/start","trace":42}`,
			wantErr: "decode JSONRPCMessage.request: decode JSONRPCRequest.trace",
		},
		{
			name:    "request id null",
			line:    `{"id":null,"method":"turn/start"}`,
			wantErr: "decode JSONRPCMessage.request: decode JSONRPCRequest.id: null is not allowed",
		},
		{
			name:    "response nested duplicate",
			line:    `{"id":"go-sdk-1","result":{"ok":true,"ok":false}}`,
			wantErr: `decode JSONRPCMessage.response: decode JSONRPCResponse.result: duplicate object key "ok"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJSONRPCEnvelope([]byte(tc.line))
			if err == nil {
				t.Fatalf("validateJSONRPCEnvelope accepted malformed %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected %s error: %v", tc.name, err)
			}
		})
	}
}
