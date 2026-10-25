package codexsdk

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

type jsonRPCVariant uint8

const (
	jsonRPCRequest jsonRPCVariant = iota + 1
	jsonRPCNotification
	jsonRPCResponse
	jsonRPCError
)

// payload contains only the selected variant's semantic fields. observation is
// the original notification object, never request or response authority.
type jsonRPCEnvelope struct {
	variant     jsonRPCVariant
	payload     map[string]any
	observation map[string]any
}

// decodeJSONRPCEnvelope follows rpc.rs's untagged variant order within the
// SDK's object-only, duplicate-free, lossless JSON domain. It does not admit a
// method, a typed payload, or an application decision.
func decodeJSONRPCEnvelope(data []byte) (jsonRPCEnvelope, error) {
	invalid := errors.New("decode JSONRPCMessage: invalid envelope")
	value, err := protocolv2.ParseJSONValue(data)
	if err != nil || value.Kind() != protocolv2.JSONKindObject {
		return jsonRPCEnvelope{}, invalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return jsonRPCEnvelope{}, invalid
	}
	var id protocolv2.RequestId
	validID := fields["id"] != nil && !jsonRPCNegativeZero(fields["id"]) && json.Unmarshal(fields["id"], &id) == nil
	var method string
	validMethod := fields["method"] != nil && !bytes.Equal(bytes.TrimSpace(fields["method"]), []byte("null")) && json.Unmarshal(fields["method"], &method) == nil
	var variant jsonRPCVariant
	var names []string
	switch {
	case validID && validMethod && validJSONRPCTrace(fields["trace"]):
		variant, names = jsonRPCRequest, []string{"id", "method", "params"}
	case validMethod:
		variant, names = jsonRPCNotification, []string{"method", "params"}
	case validID && fields["result"] != nil:
		variant, names = jsonRPCResponse, []string{"id", "result"}
	case validID && validJSONRPCError(fields["error"]):
		variant, names = jsonRPCError, []string{"id", "error"}
	default:
		return jsonRPCEnvelope{}, invalid
	}
	var original map[string]any
	if err := unmarshalJSONPreserveNumbers(data, &original); err != nil {
		return jsonRPCEnvelope{}, invalid
	}
	payload := make(map[string]any, len(names))
	for _, name := range names {
		if v, present := original[name]; present {
			payload[name] = v
		}
	}
	if variant == jsonRPCError {
		// Unknown error members are ignored by Rust too.
		source := payload["error"].(map[string]any)
		projected := map[string]any{"code": source["code"], "message": source["message"]}
		if v, present := source["data"]; present {
			projected["data"] = v
		}
		payload["error"] = projected
	}
	envelope := jsonRPCEnvelope{variant: variant, payload: payload}
	if variant == jsonRPCNotification {
		envelope.observation = original
	}
	return envelope, nil
}

func validJSONRPCTrace(raw json.RawMessage) bool {
	if raw == nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	// Select exact Rust field names before encoding/json's struct decoder,
	// which otherwise also matches case-insensitive spellings.
	for name := range fields {
		if name != "traceparent" && name != "tracestate" {
			delete(fields, name)
		}
	}
	projected, err := json.Marshal(fields)
	if err != nil {
		return false
	}
	// Bypass the standalone closed-record decoder only at this Rust envelope
	// field. Generated field types own optional string/null decoding. Nested
	// action payloads retain their generated strict decoder.
	type traceFields protocolv2.W3cTraceContext
	var trace traceFields
	return json.Unmarshal(projected, &trace) == nil
}

func validJSONRPCError(raw json.RawMessage) bool {
	// These two required fields have no generated public envelope type.
	var fields map[string]json.RawMessage
	if raw == nil || json.Unmarshal(raw, &fields) != nil {
		return false
	}
	code, message := fields["code"], fields["message"]
	if code == nil || message == nil || bytes.Equal(bytes.TrimSpace(code), []byte("null")) || bytes.Equal(bytes.TrimSpace(message), []byte("null")) {
		return false
	}
	var number int64
	var text string
	return !jsonRPCNegativeZero(code) && json.Unmarshal(code, &number) == nil && json.Unmarshal(message, &text) == nil
}

// serde_json presents -0 as a float to untagged matching. This token-level
// runtime distinction is not expressible by the schema's int64 type. Keep the
// generated RequestId decoder as the field-type authority.
func jsonRPCNegativeZero(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("-0"))
}

func unmarshalJSONPreserveNumbers(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(dest)
}
