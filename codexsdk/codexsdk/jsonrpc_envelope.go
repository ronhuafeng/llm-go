package codexsdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func validateJSONRPCEnvelope(data []byte) error {
	fields, err := decodeJSONRPCObjectFields(data, "JSONRPCMessage")
	if err != nil {
		return err
	}
	hasID := fields["id"] != nil
	hasMethod := fields["method"] != nil
	hasResult := fields["result"] != nil
	hasError := fields["error"] != nil

	matches := 0
	shape := ""
	switch {
	case hasID && hasMethod && !hasResult && !hasError:
		matches = 1
		shape = "request"
	case !hasID && hasMethod && !hasResult && !hasError:
		matches = 1
		shape = "notification"
	case hasID && !hasMethod && hasResult && !hasError:
		matches = 1
		shape = "response"
	case hasID && !hasMethod && !hasResult && hasError:
		matches = 1
		shape = "error"
	}
	if matches != 1 {
		return fmt.Errorf("decode JSONRPCMessage: expected exactly one JSON-RPC envelope shape")
	}

	switch shape {
	case "request":
		return validateJSONRPCRequest(fields)
	case "notification":
		return validateJSONRPCNotification(fields)
	case "response":
		return validateJSONRPCResponse(fields)
	case "error":
		return validateJSONRPCError(fields)
	default:
		return fmt.Errorf("decode JSONRPCMessage: expected exactly one JSON-RPC envelope shape")
	}
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
			return fmt.Errorf("decode JSONRPCMessage.request: decode JSONRPCRequest.trace: %w", err)
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
			return nil, fmt.Errorf("decode %s: duplicate object key %q", path, key)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode %s.%s: %w", path, key, err)
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
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return nil
	}
	return validateJSONInteger(raw, path)
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
		return fmt.Errorf("decode %s: %w", path, err)
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
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
