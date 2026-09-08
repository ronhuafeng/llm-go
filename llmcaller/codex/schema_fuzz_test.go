package codexcaller

import (
	"encoding/json"
	"errors"
	"testing"
)

func FuzzStrictOutputSchemaFromJSON(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"type":"object","properties":{"x":{"type":["string","null"]}}}`),
		[]byte(`{"type":"object","properties":{"x":{"type":"string"}}}`),
		[]byte(`{"$dynamicRef":"#node"}`),
		[]byte(`{"type":"object","type":"string"}`),
		[]byte(`{"$ref":"https://example.com/schema"}`),
		[]byte(`{"$schema":"http://json-schema.org/draft-07/schema#","items":[{"type":"object","properties":{"value":{"type":"string"}}}]}`),
		[]byte(`true`),
		[]byte(`[]`),
		[]byte(``),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		schema, err := StrictOutputSchemaFromJSON(raw)
		if err != nil {
			if errors.Is(err, ErrMissingSchemaJSON) {
				return
			}
			var policyErr *SchemaPolicyError
			if !errors.As(err, &policyErr) || policyErr.Kind == "" {
				t.Fatalf("error = %v, want SchemaPolicyError with kind", err)
			}
			return
		}
		encoded, encErr := json.Marshal(schema)
		if encErr != nil {
			t.Fatalf("accepted schema failed to marshal: %v", encErr)
		}
		if !json.Valid(encoded) {
			t.Fatalf("accepted schema is not valid JSON: %s", encoded)
		}
	})
}
