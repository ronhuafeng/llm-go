package codexsdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

// jsonRPCEnvelope carries both the admitted strict wire shape and the values
// that must be routed. Selecting the kind here prevents transport and routing
// from independently interpreting a frame.
type jsonRPCEnvelope struct {
	kind    jsonRPCEnvelopeKind
	message map[string]any
}

type jsonRPCEnvelopeKind uint8

const (
	jsonRPCRequest jsonRPCEnvelopeKind = iota + 1
	jsonRPCNotification
	jsonRPCResponse
	jsonRPCError
)

// decodeJSONRPCEnvelope preserves the SDK's accepted strict envelope contract.
// Existing field validators keep the accepted wire rules; unknown metadata
// stays observable and exact JSON numbers survive.
func decodeJSONRPCEnvelope(data []byte) (jsonRPCEnvelope, error) {
	fields, err := decodeJSONRPCObjectFields(data, "JSONRPCMessage")
	if err != nil {
		return jsonRPCEnvelope{}, err
	}
	hasID := fields["id"] != nil
	hasMethod := fields["method"] != nil
	hasResult := fields["result"] != nil
	hasError := fields["error"] != nil

	var kind jsonRPCEnvelopeKind
	switch {
	case hasID && hasMethod && !hasResult && !hasError:
		kind = jsonRPCRequest
	case !hasID && hasMethod && !hasResult && !hasError:
		kind = jsonRPCNotification
	case hasID && !hasMethod && hasResult && !hasError:
		kind = jsonRPCResponse
	case hasID && !hasMethod && !hasResult && hasError:
		kind = jsonRPCError
	default:
		return jsonRPCEnvelope{}, fmt.Errorf("decode JSONRPCMessage: expected exactly one JSON-RPC envelope shape")
	}

	switch kind {
	case jsonRPCRequest:
		err = validateJSONRPCRequest(fields)
	case jsonRPCNotification:
		err = validateJSONRPCNotification(fields)
	case jsonRPCResponse:
		err = validateJSONRPCResponse(fields)
	case jsonRPCError:
		err = validateJSONRPCError(fields)
	}
	if err != nil {
		return jsonRPCEnvelope{}, err
	}

	// Preserve the original object, including unknown fields, present-null,
	// and json.Number. Decode its members instead of parsing the entire frame
	// again; the preceding pass already checked unique top-level keys.
	message := make(map[string]any, len(fields))
	for name, raw := range fields {
		var value any
		if err := unmarshalJSONPreserveNumbers(raw, &value); err != nil {
			return jsonRPCEnvelope{}, fmt.Errorf("decode JSONRPCMessage: invalid object field")
		}
		message[name] = value
	}
	return jsonRPCEnvelope{kind: kind, message: message}, nil
}

// Retain this validation entry point for existing accepted-base regressions.
// Production dispatch uses the same decoder and its selected kind.
func validateJSONRPCEnvelope(data []byte) error {
	_, err := decodeJSONRPCEnvelope(data)
	return err
}

func validateJSONRPCRequest(fields map[string]json.RawMessage) error {
	if err := validateJSONRPCRequestID(fields["id"], "JSONRPCRequest.id"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.request: %w", err)
	}
	if err := validateRequiredJSONString(fields["method"], "JSONRPCRequest.method"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.request: %w", err)
	}
	if err := validateOptionalJSONValue(fields, "params", "JSONRPCRequest.params"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.request: %w", err)
	}
	if raw := fields["trace"]; raw != nil {
		var trace protocolv2.Nullable[protocolv2.W3cTraceContext]
		if err := json.Unmarshal(raw, &trace); err != nil {
			return fmt.Errorf("decode JSONRPCMessage.request: decode JSONRPCRequest.trace: invalid value")
		}
	}
	return nil
}

func validateJSONRPCNotification(fields map[string]json.RawMessage) error {
	if err := validateRequiredJSONString(fields["method"], "JSONRPCNotification.method"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.notification: %w", err)
	}
	if err := validateOptionalJSONValue(fields, "params", "JSONRPCNotification.params"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.notification: %w", err)
	}
	return nil
}

func validateJSONRPCResponse(fields map[string]json.RawMessage) error {
	if err := validateJSONRPCRequestID(fields["id"], "JSONRPCResponse.id"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.response: %w", err)
	}
	if err := validateRequiredJSONValue(fields["result"], "JSONRPCResponse.result"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.response: %w", err)
	}
	return nil
}

func validateJSONRPCError(fields map[string]json.RawMessage) error {
	if err := validateJSONRPCRequestID(fields["id"], "JSONRPCError.id"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.error: %w", err)
	}
	errorFields, err := decodeJSONRPCObjectFields(fields["error"], "JSONRPCErrorError")
	if err != nil {
		return fmt.Errorf("decode JSONRPCMessage.error: %w", err)
	}
	if err := validateRequiredJSONInteger(errorFields["code"], "JSONRPCErrorError.code"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.error: %w", err)
	}
	if err := validateRequiredJSONString(errorFields["message"], "JSONRPCErrorError.message"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.error: %w", err)
	}
	if err := validateOptionalJSONValue(errorFields, "data", "JSONRPCErrorError.data"); err != nil {
		return fmt.Errorf("decode JSONRPCMessage.error: %w", err)
	}
	return nil
}

func decodeJSONRPCObjectFields(data []byte, path string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("decode %s: expected object", path)
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("decode %s: expected object key", path)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("decode %s: duplicate object key", path)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode %s: invalid object field: %w", path, err)
		}
		fields[key] = raw
	}
	token, err = decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '}' {
		return nil, fmt.Errorf("decode %s: expected end object", path)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("decode %s: unexpected trailing data", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return fields, nil
}

func validateRequiredJSONString(raw json.RawMessage, path string) error {
	if raw == nil {
		return fmt.Errorf("decode %s: missing required field", path)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode %s: expected string", path)
	}
	return nil
}

func validateRequiredJSONInteger(raw json.RawMessage, path string) error {
	if raw == nil {
		return fmt.Errorf("decode %s: missing required field", path)
	}
	return validateJSONInteger(raw, path)
}

func validateJSONRPCRequestID(raw json.RawMessage, path string) error {
	if raw == nil {
		return fmt.Errorf("decode %s: missing required field", path)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("decode %s: null is not allowed", path)
	}
	// Let the generated upstream RequestId union own the accepted ID kinds.
	// Error wording stays stable for the SDK's existing transport contract.
	var id protocolv2.RequestId
	if err := json.Unmarshal(raw, &id); err != nil || !id.IsValid() {
		return fmt.Errorf("decode %s: expected integer", path)
	}
	return nil
}

func validateJSONInteger(raw json.RawMessage, path string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return fmt.Errorf("decode %s: expected integer", path)
	}
	if strings.ContainsAny(number.String(), ".eE") {
		return fmt.Errorf("decode %s: expected integer", path)
	}
	if _, err := strconv.ParseInt(number.String(), 10, 64); err != nil {
		return fmt.Errorf("decode %s: expected integer", path)
	}
	return nil
}

func validateRequiredJSONValue(raw json.RawMessage, path string) error {
	if raw == nil {
		return fmt.Errorf("decode %s: missing required field", path)
	}
	var value protocolv2.JSONValue
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode %s: invalid JSON value", path)
	}
	return nil
}

func validateOptionalJSONValue(fields map[string]json.RawMessage, name string, path string) error {
	raw := fields[name]
	if raw == nil {
		return nil
	}
	var value protocolv2.JSONValue
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode %s: invalid JSON value", path)
	}
	return nil
}

func unmarshalJSONPreserveNumbers(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(dest)
}
