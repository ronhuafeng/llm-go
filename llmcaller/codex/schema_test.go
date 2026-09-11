package codexcaller

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
)

func TestStrictOutputSchemaPreservesAcceptedContracts(t *testing.T) {
	t.Run("required scalar", func(t *testing.T) {
		type output struct {
			Name string `json:"name"`
		}
		assertSchemaJSONValueEqual(t, schemaFor[output](t))
	})

	t.Run("required nullable property", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","required":["note"],"properties":{"note":{"type":["string","null"]}}}`)
		assertSchemaJSONValueEqual(t, raw)
	})

	t.Run("required local ref", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","required":["note"],"properties":{"note":{"$ref":"#/$defs/note"}},"$defs":{"note":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`)
		assertSchemaJSONValueEqual(t, raw)
	})

	t.Run("unknown keyword value", func(t *testing.T) {
		raw := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","required":["name"],"properties":{"name":{"type":"string","x-note":{"level":2}}}}`)
		schema, err := StrictOutputSchemaFromJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"x-note":{"level":2}`) {
			t.Fatalf("unknown keyword changed: %s", encoded)
		}
	})

	t.Run("boolean schemas", func(t *testing.T) {
		assertSchemaJSONValueEqual(t, json.RawMessage(`true`))
		assertSchemaJSONValueEqual(t, json.RawMessage(`false`))
	})
}

func TestStrictOutputSchemaRejectsOptionalPropertiesWithoutMutation(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		path string
	}{
		{
			name: "nullable optional",
			raw:  json.RawMessage(`{"type":"object","properties":{"x":{"type":["string","null"]}}}`),
			path: "/properties/x",
		},
		{
			name: "nonnullable optional",
			raw:  json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
			path: "/properties/x",
		},
		{
			name: "anyOf including null",
			raw:  json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`),
			path: "/properties/x",
		},
		{
			name: "enum including null",
			raw:  json.RawMessage(`{"type":"object","properties":{"x":{"enum":[null,"x"]}}}`),
			path: "/properties/x",
		},
		{
			name: "optional local ref",
			raw:  json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"#/$defs/value"}},"$defs":{"value":{"type":["string","null"]}}}`),
			path: "/properties/x",
		},
		{
			name: "nested optional",
			raw:  json.RawMessage(`{"type":"object","required":["child"],"properties":{"child":{"type":"object","properties":{"note":{"type":["string","null"]}}}}}`),
			path: "/properties/child/properties/note",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertSchemaError(t, test.raw, "optional_property_unsupported", test.path)
		})
	}
}

func TestOptionalGoShapesAreNotUsedAsSemanticEquivalenceProof(t *testing.T) {
	t.Run("pointer", func(t *testing.T) {
		type output struct {
			Name string  `json:"name"`
			Note *string `json:"note,omitempty"`
		}
		assertSchemaError(t, schemaFor[output](t), "optional_property_unsupported", "/properties/note")
	})

	t.Run("slice", func(t *testing.T) {
		type output struct {
			Items []string `json:"items,omitempty"`
		}
		assertSchemaError(t, schemaFor[output](t), "optional_property_unsupported", "/properties/items")
	})

	t.Run("raw message", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","properties":{"payload":{"type":["object","array","string","number","boolean","null"]}}}`)
		assertSchemaError(t, raw, "optional_property_unsupported", "/properties/payload")
	})
}

func TestStrictOutputSchemaRejectsUnsupportedRepresentationFeatures(t *testing.T) {
	assertSchemaErrorKind(t, json.RawMessage(`{"$schema":"https://json-schema.org/draft/9999/schema","type":"object"}`), "invalid_schema")
	assertSchemaErrorKind(t, json.RawMessage(`{"$defs":{"node":{"$ref":"#/$defs/node"}},"$ref":"#/$defs/node"}`), "cyclic_ref")
	assertSchemaErrorKind(t, json.RawMessage(`{"$ref":"https://example.test/schema"}`), "external_ref")
	assertSchemaErrorKind(t, json.RawMessage(`{"$ref":"#/$defs/missing"}`), "unresolvable_ref")
	assertSchemaErrorKind(t, json.RawMessage(`{"$dynamicRef":"#node"}`), "unsupported_dynamic_ref")
	assertSchemaErrorKind(t, json.RawMessage(`{"$vocabulary":{"https://example.test/vocab":true},"type":"object"}`), "unsupported_vocabulary")
}

func TestStrictOutputSchemaRejectsDuplicateKeysAndPreservesPointerPath(t *testing.T) {
	assertSchemaErrorKind(t, json.RawMessage(`{"type":"object","type":"string"}`), "invalid_json")
	_, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"type":"object","properties":{"a/b~c":{"type":"string"}}}`))
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != "optional_property_unsupported" || policyErr.Path != "/properties/a~1b~0c" {
		t.Fatalf("error = %#v", policyErr)
	}
}

func TestCallerRejectsUnrepresentableSchemaBeforeRunnerInvocation(t *testing.T) {
	runner := &fakeRunner{}
	caller, err := New(applicationOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.CallDetailed(context.Background(), llmadapter.Request{
		Prompt:       "must not run",
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":["string","null"]}}}`),
	})
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != "optional_property_unsupported" || policyErr.Path != "/properties/x" {
		t.Fatalf("error = %#v", policyErr)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d", len(runner.requests))
	}
}

func assertSchemaJSONValueEqual(t *testing.T, raw json.RawMessage) {
	t.Helper()
	schema, err := StrictOutputSchemaFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := schema.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("schema JSON value changed:\nbefore: %s\nafter:  %s", raw, encoded)
	}
}

func schemaFor[T any](t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := llmschema.SchemaJSONFor[T]()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertSchemaErrorKind(t *testing.T, raw json.RawMessage, kind string) {
	t.Helper()
	_, err := StrictOutputSchemaFromJSON(raw)
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != kind {
		t.Fatalf("error = %v, want SchemaPolicyError kind %s", err, kind)
	}
}

func assertSchemaError(t *testing.T, raw json.RawMessage, kind, path string) {
	t.Helper()
	_, err := StrictOutputSchemaFromJSON(raw)
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != kind || policyErr.Path != path {
		t.Fatalf("error = %#v, want SchemaPolicyError kind %s at %q", policyErr, kind, path)
	}

	runner := &fakeRunner{}
	caller, err := New(applicationOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.CallDetailed(context.Background(), llmadapter.Request{Prompt: "must not run", OutputSchema: raw})
	policyErr = nil
	if !errors.As(err, &policyErr) || policyErr.Kind != kind || policyErr.Path != path {
		t.Fatalf("Caller error = %#v, want SchemaPolicyError kind %s at %q", policyErr, kind, path)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d requests after fail-closed schema error", len(runner.requests))
	}
}
