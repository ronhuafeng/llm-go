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
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type nullAwareString struct {
	SawNull bool
}

func (value *nullAwareString) UnmarshalJSON(data []byte) error {
	value.SawNull = string(data) == "null"
	return nil
}

func TestStrictOutputSchemaCompatibilityMatrix(t *testing.T) {
	t.Run("required-scalar-preserved", func(t *testing.T) {
		type output struct {
			Name string `json:"name"`
		}
		assertSchemaJSONValueEqual(t, schemaFor[output](t))
	})
	t.Run("optional-pointer-currently-promoted", func(t *testing.T) {
		type output struct {
			Name string  `json:"name"`
			Note *string `json:"note,omitempty"`
		}
		schema, err := StrictOutputSchemaFromJSON(schemaFor[output](t))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"required":["name","note"]`) {
			t.Fatalf("schema = %s", encoded)
		}
	})
	t.Run("optional-scalar-fails-closed", func(t *testing.T) {
		type output struct {
			Name  string `json:"name"`
			Score int    `json:"score,omitempty"`
		}
		assertSchemaError(t, schemaFor[output](t), "optional_non_nullable", "/properties/score")
	})
	t.Run("nested-optional-pointer-currently-promoted", func(t *testing.T) {
		type child struct {
			Note *string `json:"note,omitempty"`
		}
		type output struct {
			Child child `json:"child"`
		}
		if _, err := StrictOutputSchemaFromJSON(schemaFor[output](t)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("presence-sensitive-decoding-remains-distinct", func(t *testing.T) {
		type rawOutput struct {
			Payload json.RawMessage `json:"payload,omitempty"`
		}
		var absentRaw, nullRaw rawOutput
		if err := json.Unmarshal([]byte(`{}`), &absentRaw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(`{"payload":null}`), &nullRaw); err != nil {
			t.Fatal(err)
		}
		if absentRaw.Payload != nil || string(nullRaw.Payload) != "null" {
			t.Fatalf("RawMessage distinction changed: absent=%q null=%q", absentRaw.Payload, nullRaw.Payload)
		}

		type customOutput struct {
			Value nullAwareString `json:"value,omitempty"`
		}
		var absentCustom, nullCustom customOutput
		if err := json.Unmarshal([]byte(`{}`), &absentCustom); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(`{"value":null}`), &nullCustom); err != nil {
			t.Fatal(err)
		}
		if absentCustom.Value.SawNull || !nullCustom.Value.SawNull {
			t.Fatal("custom unmarshaler did not preserve absence/null distinction")
		}
	})
	t.Run("local-ref-preserved", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","properties":{"note":{"$ref":"#/$defs/note"}},"$defs":{"note":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`)
		schema, err := StrictOutputSchemaFromJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"$ref":"#/$defs/note"`) ||
			!strings.Contains(string(encoded), `"required":["note"]`) {
			t.Fatalf("schema = %s", encoded)
		}
	})
	t.Run("drafts-and-unknown-keywords", func(t *testing.T) {
		raw := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","required":["name"],"properties":{"name":{"type":"string","x-note":{"level":2}}}}`)
		schema, err := StrictOutputSchemaFromJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := schema.MarshalJSON()
		if !strings.Contains(string(encoded), `"x-note":{"level":2}`) {
			t.Fatalf("unknown keyword changed: %s", encoded)
		}
		assertSchemaErrorKind(t, json.RawMessage(`{"$schema":"https://json-schema.org/draft/9999/schema","type":"object"}`), "invalid_schema")
	})
	t.Run("reference-failures", func(t *testing.T) {
		assertSchemaErrorKind(t, json.RawMessage(`{"$defs":{"node":{"$ref":"#/$defs/node"}},"$ref":"#/$defs/node"}`), "cyclic_ref")
		assertSchemaErrorKind(t, json.RawMessage(`{"$ref":"https://example.test/schema"}`), "external_ref")
		assertSchemaErrorKind(t, json.RawMessage(`{"$ref":"#/$defs/missing"}`), "unresolvable_ref")
		assertSchemaErrorKind(t, json.RawMessage(`{"$dynamicRef":"#node"}`), "unsupported_dynamic_ref")
	})
}

func TestStrictOutputSchemaUsesJSONSchemaSemanticsForNullAdmission(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		wantKind string
	}{
		{
			name:     "ref rejects null",
			schema:   `{"type":"object","properties":{"x":{"$ref":"#/$defs/nonNull","type":["string","null"]}},"$defs":{"nonNull":{"type":"string"}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:   "anyOf admits null",
			schema: `{"type":"object","properties":{"x":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`,
		},
		{
			name:     "allOf rejects null",
			schema:   `{"type":"object","properties":{"x":{"allOf":[{"type":["string","null"]},{"type":"string"}]}}}`,
			wantKind: "optional_non_nullable",
		},
		{
			name:   "enum admits null",
			schema: `{"type":"object","properties":{"x":{"enum":[null,"x"]}}}`,
		},
		{
			name:     "not rejects null",
			schema:   `{"type":"object","properties":{"x":{"not":{"const":null}}}}`,
			wantKind: "optional_non_nullable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := StrictOutputSchemaFromJSON(json.RawMessage(test.schema))
			if test.wantKind != "" {
				var policyErr *SchemaPolicyError
				if !errors.As(err, &policyErr) || policyErr.Kind != test.wantKind || policyErr.Path != "/properties/x" {
					t.Fatalf("error = %#v, want %s at /properties/x", policyErr, test.wantKind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := schema.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), `"required":["x"]`) {
				t.Fatalf("schema = %s", encoded)
			}
		})
	}
}

func TestStrictOutputSchemaDecisionMatchesDirectValidator(t *testing.T) {
	for _, propertySchema := range []string{
		`{"type":"null"}`,
		`{"type":"string"}`,
		`{"anyOf":[{"type":"string"},{"enum":[null]}]}`,
		`{"not":{"enum":[null]}}`,
	} {
		raw := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"x":` + propertySchema + `}}`)
		var document any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			t.Fatal(err)
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("https://test.invalid/schema.json", document); err != nil {
			t.Fatal(err)
		}
		property, err := compiler.Compile("https://test.invalid/schema.json#/properties/x")
		if err != nil {
			t.Fatal(err)
		}
		wantPromotion := property.Validate(nil) == nil
		_, transformErr := StrictOutputSchemaFromJSON(raw)
		if (transformErr == nil) != wantPromotion {
			t.Errorf("property %s: accepted=%v null=%v error=%v", propertySchema, transformErr == nil, wantPromotion, transformErr)
		}
	}
}

func TestStrictOutputSchemaRejectsDuplicateKeysAndPreservesPointerPath(t *testing.T) {
	assertSchemaErrorKind(t, json.RawMessage(`{"type":"object","type":"string"}`), "invalid_json")
	_, err := StrictOutputSchemaFromJSON(json.RawMessage(`{"type":"object","properties":{"a/b~c":{"type":"string"}}}`))
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Path != "/properties/a~1b~0c" {
		t.Fatalf("error = %#v", policyErr)
	}
}

func TestCallerRejectsSchemaBeforeRunnerInvocation(t *testing.T) {
	runner := &fakeRunner{}
	caller, err := New(applicationOptions(runner))
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.CallDetailed(context.Background(), llmadapter.Request{
		Prompt:       "must not run",
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":["null",1]}}}`),
	})
	var policyErr *SchemaPolicyError
	if !errors.As(err, &policyErr) || policyErr.Kind != "nullable_analysis" || policyErr.Path != "/properties/x" {
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
