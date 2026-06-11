package llmschema

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// SchemaJSONFor projects a Go expected-output type into provider-neutral JSON Schema JSON.
func SchemaJSONFor[T any]() (json.RawMessage, error) {
	schema, err := schemaFor[T]()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal structured output schema: %w", err)
	}
	return json.RawMessage(data), nil
}

// Decode unmarshals provider structured output into the expected Go type.
func Decode[T any](data []byte) (T, error) {
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode structured output: %w", err)
	}
	return value, nil
}

// DecodeString unmarshals provider structured output text into the expected Go type.
func DecodeString[T any](text string) (T, error) {
	return Decode[T]([]byte(text))
}

func schemaFor[T any]() (*jsonschema.Schema, error) {
	return jsonschema.For[T](defaultForOptions())
}

func defaultForOptions() *jsonschema.ForOptions {
	return &jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeOf(json.RawMessage{}): {},
		},
	}
}
