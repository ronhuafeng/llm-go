package protocolv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// JSONKind names the explicit JSON AST case represented by JSONValue.
type JSONKind string

const (
	JSONKindInvalid JSONKind = ""
	JSONKindObject  JSONKind = "object"
	JSONKindArray   JSONKind = "array"
	JSONKindString  JSONKind = "string"
	JSONKindNumber  JSONKind = "number"
	JSONKindBool    JSONKind = "boolean"
	JSONKindNull    JSONKind = "null"
)

// JSONValue is an opaque, typed JSON AST value for protocol fields that are
// explicitly dynamic JSON. The zero value is invalid; JSONNull is JSON null.
type JSONValue struct {
	kind        JSONKind
	boolValue   bool
	stringValue string
	numberValue json.Number
	arrayValue  []JSONValue
	objectValue map[string]JSONValue
}

// JSONNull constructs a JSON null value.
func JSONNull() JSONValue {
	return JSONValue{kind: JSONKindNull}
}

// JSONBool constructs a JSON boolean value.
func JSONBool(value bool) JSONValue {
	return JSONValue{kind: JSONKindBool, boolValue: value}
}

// JSONString constructs a JSON string value.
func JSONString(value string) JSONValue {
	return JSONValue{kind: JSONKindString, stringValue: value}
}

// JSONNumber constructs a JSON number value, preserving the original token.
func JSONNumber(value json.Number) (JSONValue, error) {
	if err := validateJSONNumber(value); err != nil {
		return JSONValue{}, err
	}
	return JSONValue{kind: JSONKindNumber, numberValue: value}, nil
}

// JSONArray constructs a JSON array value. The input slice is copied.
func JSONArray(values []JSONValue) JSONValue {
	copied := make([]JSONValue, len(values))
	for index, value := range values {
		copied[index] = value.clone()
	}
	return JSONValue{kind: JSONKindArray, arrayValue: copied}
}

// JSONObject constructs a JSON object value. The input map is copied.
func JSONObject(values map[string]JSONValue) JSONValue {
	copied := make(map[string]JSONValue, len(values))
	for key, value := range values {
		copied[key] = value.clone()
	}
	return JSONValue{kind: JSONKindObject, objectValue: copied}
}

// ParseJSONValue parses a JSON value, preserving number tokens and rejecting
// duplicate object keys at every object depth.
func ParseJSONValue(data []byte) (JSONValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	value, err := parseJSONValue(decoder, "")
	if err != nil {
		return JSONValue{}, err
	}

	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return JSONValue{}, err
		}
		return JSONValue{}, fmt.Errorf("unexpected trailing JSON token %v", token)
	}

	return value, nil
}

// Kind returns the explicit JSON kind. The zero value returns JSONKindInvalid.
func (value JSONValue) Kind() JSONKind {
	return value.kind
}

// IsValid reports whether the value is one of the explicit JSON cases.
func (value JSONValue) IsValid() bool {
	return value.kind != JSONKindInvalid
}

// AsBool returns the boolean value when Kind is JSONKindBool.
func (value JSONValue) AsBool() (bool, bool) {
	if value.kind != JSONKindBool {
		return false, false
	}
	return value.boolValue, true
}

// AsString returns the string value when Kind is JSONKindString.
func (value JSONValue) AsString() (string, bool) {
	if value.kind != JSONKindString {
		return "", false
	}
	return value.stringValue, true
}

// AsNumber returns the number token when Kind is JSONKindNumber.
func (value JSONValue) AsNumber() (json.Number, bool) {
	if value.kind != JSONKindNumber {
		return "", false
	}
	return value.numberValue, true
}

// AsArray returns a copy of the array when Kind is JSONKindArray.
func (value JSONValue) AsArray() ([]JSONValue, bool) {
	if value.kind != JSONKindArray {
		return nil, false
	}
	copied := make([]JSONValue, len(value.arrayValue))
	for index, member := range value.arrayValue {
		copied[index] = member.clone()
	}
	return copied, true
}

// AsObject returns a copy of the object map when Kind is JSONKindObject.
func (value JSONValue) AsObject() (map[string]JSONValue, bool) {
	if value.kind != JSONKindObject {
		return nil, false
	}
	copied := make(map[string]JSONValue, len(value.objectValue))
	for key, member := range value.objectValue {
		copied[key] = member.clone()
	}
	return copied, true
}

// MarshalJSON marshals the value as JSON. Invalid values return an ordinary Go error.
func (value JSONValue) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	if err := value.writeJSON(&buffer, ""); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// UnmarshalJSON decodes a JSON AST value and rejects duplicate object keys.
func (value *JSONValue) UnmarshalJSON(data []byte) error {
	parsed, err := ParseJSONValue(data)
	if err != nil {
		return err
	}
	*value = parsed
	return nil
}

func (value JSONValue) clone() JSONValue {
	switch value.kind {
	case JSONKindArray:
		copied := make([]JSONValue, len(value.arrayValue))
		for index, member := range value.arrayValue {
			copied[index] = member.clone()
		}
		value.arrayValue = copied
	case JSONKindObject:
		copied := make(map[string]JSONValue, len(value.objectValue))
		for key, member := range value.objectValue {
			copied[key] = member.clone()
		}
		value.objectValue = copied
	}
	return value
}

func validateJSONNumber(value json.Number) error {
	token := value.String()
	if token == "" {
		return errors.New("invalid JSON number: empty token")
	}
	if strings.TrimSpace(token) != token {
		return fmt.Errorf("invalid JSON number %q: whitespace is not part of a JSON number token", token)
	}

	decoder := json.NewDecoder(strings.NewReader(token))
	decoder.UseNumber()
	var parsed json.Number
	if err := decoder.Decode(&parsed); err != nil {
		return fmt.Errorf("invalid JSON number %q: %w", token, err)
	}
	if parsed.String() != token {
		return fmt.Errorf("invalid JSON number %q", token)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid JSON number %q: trailing JSON value", token)
		}
		return fmt.Errorf("invalid JSON number %q: %w", token, err)
	}
	return nil
}

func parseJSONValue(decoder *json.Decoder, path string) (JSONValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return JSONValue{}, err
	}

	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			return parseJSONObject(decoder, path)
		case '[':
			return parseJSONArray(decoder, path)
		default:
			return JSONValue{}, fmt.Errorf("unexpected JSON delimiter %q at %s", typed, pointerForError(path))
		}
	case nil:
		return JSONNull(), nil
	case bool:
		return JSONBool(typed), nil
	case string:
		return JSONString(typed), nil
	case json.Number:
		return JSONNumber(typed)
	default:
		return JSONValue{}, fmt.Errorf("unexpected JSON token %v at %s", token, pointerForError(path))
	}
}

func parseJSONObject(decoder *json.Decoder, path string) (JSONValue, error) {
	seen := map[string]struct{}{}
	values := map[string]JSONValue{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return JSONValue{}, err
		}
		key, ok := token.(string)
		if !ok {
			return JSONValue{}, fmt.Errorf("expected object key at %s, got %v", pointerForError(path), token)
		}
		if _, exists := seen[key]; exists {
			return JSONValue{}, fmt.Errorf("duplicate object key %q at %s", key, pointerForError(path))
		}
		seen[key] = struct{}{}

		value, err := parseJSONValue(decoder, joinPointer(path, key))
		if err != nil {
			return JSONValue{}, err
		}
		values[key] = value
	}

	if token, err := decoder.Token(); err != nil {
		return JSONValue{}, err
	} else if token != json.Delim('}') {
		return JSONValue{}, fmt.Errorf("expected object close at %s, got %v", pointerForError(path), token)
	}

	return JSONObject(values), nil
}

func parseJSONArray(decoder *json.Decoder, path string) (JSONValue, error) {
	values := []JSONValue{}
	for index := 0; decoder.More(); index++ {
		value, err := parseJSONValue(decoder, joinPointer(path, strconv.Itoa(index)))
		if err != nil {
			return JSONValue{}, err
		}
		values = append(values, value)
	}

	if token, err := decoder.Token(); err != nil {
		return JSONValue{}, err
	} else if token != json.Delim(']') {
		return JSONValue{}, fmt.Errorf("expected array close at %s, got %v", pointerForError(path), token)
	}

	return JSONArray(values), nil
}

func (value JSONValue) writeJSON(buffer *bytes.Buffer, path string) error {
	switch value.kind {
	case JSONKindNull:
		buffer.WriteString("null")
	case JSONKindBool:
		if value.boolValue {
			buffer.WriteString("true")
		} else {
			buffer.WriteString("false")
		}
	case JSONKindString:
		encoded, err := json.Marshal(value.stringValue)
		if err != nil {
			return err
		}
		buffer.Write(encoded)
	case JSONKindNumber:
		if err := validateJSONNumber(value.numberValue); err != nil {
			return fmt.Errorf("invalid JSON number at %s: %w", pointerForError(path), err)
		}
		buffer.WriteString(value.numberValue.String())
	case JSONKindArray:
		buffer.WriteByte('[')
		for index, member := range value.arrayValue {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := member.writeJSON(buffer, joinPointer(path, strconv.Itoa(index))); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case JSONKindObject:
		keys := make([]string, 0, len(value.objectValue))
		for key := range value.objectValue {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buffer.Write(encodedKey)
			buffer.WriteByte(':')
			if err := value.objectValue[key].writeJSON(buffer, joinPointer(path, key)); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	default:
		return fmt.Errorf("invalid JSONValue at %s", pointerForError(path))
	}
	return nil
}

func joinPointer(path, token string) string {
	escaped := strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
	if path == "" {
		return "/" + escaped
	}
	return path + "/" + escaped
}

func pointerForError(path string) string {
	if path == "" {
		return "/"
	}
	return path
}
