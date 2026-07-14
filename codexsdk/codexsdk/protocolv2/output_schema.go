package protocolv2

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// OutputSchema is a typed wrapper around a JSON Schema value.
type OutputSchema struct {
	value JSONValue
}

// OutputSchemaFromJSON parses and validates a native JSON Schema value.
func OutputSchemaFromJSON(data []byte) (OutputSchema, error) {
	value, err := ParseJSONValue(data)
	if err != nil {
		return OutputSchema{}, err
	}
	if value.Kind() != JSONKindObject && value.Kind() != JSONKindBool {
		return OutputSchema{}, errors.New("output schema must be a top-level JSON object or boolean schema")
	}
	if value.Kind() == JSONKindObject {
		if err := validateOutputSchemaValue(value); err != nil {
			return OutputSchema{}, err
		}
	}
	return OutputSchema{value: value}, nil
}

// IsValid reports whether the schema contains a JSON Schema object or boolean schema.
func (schema OutputSchema) IsValid() bool {
	return schema.value.Kind() == JSONKindObject || schema.value.Kind() == JSONKindBool
}

// JSONValue returns the schema as a typed JSON value.
func (schema OutputSchema) JSONValue() JSONValue {
	return schema.value.clone()
}

// MarshalJSON marshals the schema value. The zero value returns an ordinary Go error.
func (schema OutputSchema) MarshalJSON() ([]byte, error) {
	if !schema.IsValid() {
		return nil, errors.New("invalid OutputSchema")
	}
	return json.Marshal(schema.value)
}

// UnmarshalJSON decodes a native JSON Schema object or boolean schema.
func (schema *OutputSchema) UnmarshalJSON(data []byte) error {
	parsed, err := OutputSchemaFromJSON(data)
	if err != nil {
		return err
	}
	*schema = parsed
	return nil
}

func validateOutputSchemaValue(value JSONValue) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("invalid output schema: %w", err)
	}
	if _, err := schema.Resolve(nil); err != nil {
		return fmt.Errorf("invalid output schema: %w", err)
	}
	return nil
}
