package protocolv2

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutputSchemaFromJSONAcceptsNativeJSONSchema(t *testing.T) {
	valid, err := OutputSchemaFromJSON([]byte(`{"type":"object","properties":{"name":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !valid.IsValid() {
		t.Fatal("schema should be valid")
	}

	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"properties":{"name":{"type":"string"}},"type":"object"}` {
		t.Fatalf("marshal = %s", data)
	}

	booleanSchema, err := OutputSchemaFromJSON([]byte(`false`))
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(booleanSchema)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `false` {
		t.Fatalf("boolean schema marshal = %s", data)
	}

	for _, input := range []string{`null`, `[]`, `"schema"`, `1`} {
		if _, err := OutputSchemaFromJSON([]byte(input)); err == nil {
			t.Fatalf("OutputSchemaFromJSON(%s) should reject non-schema root", input)
		}
	}
}

func TestOutputSchemaRejectsDuplicateKeys(t *testing.T) {
	_, err := OutputSchemaFromJSON([]byte(`{"properties":{"name":{},"name":{"type":"string"}}}`))
	if err == nil {
		t.Fatal("expected duplicate key error")
	}
	if !strings.Contains(err.Error(), "/properties") || !strings.Contains(err.Error(), "name") {
		t.Fatalf("error %q should include duplicate path and key", err)
	}
}

func TestOutputSchemaRejectsInvalidJSONSchemaShape(t *testing.T) {
	for _, input := range []string{
		`{"type":1}`,
		`{"pattern":"["}`,
		`{"$defs":{},"definitions":{}}`,
	} {
		if _, err := OutputSchemaFromJSON([]byte(input)); err == nil {
			t.Fatalf("OutputSchemaFromJSON(%s) should reject invalid JSON Schema", input)
		}
	}
}

func TestOutputSchemaUnmarshalUsesTypedParser(t *testing.T) {
	var schema OutputSchema
	if err := json.Unmarshal([]byte(`{"type":"object"}`), &schema); err != nil {
		t.Fatal(err)
	}
	if !schema.IsValid() {
		t.Fatal("unmarshaled schema should be valid")
	}

	if err := json.Unmarshal([]byte(`{"properties":{"x":{},"x":false}}`), &schema); err == nil {
		t.Fatal("expected duplicate key rejection through UnmarshalJSON")
	}
}

func TestOutputSchemaZeroValueInvalid(t *testing.T) {
	var schema OutputSchema
	if schema.IsValid() {
		t.Fatal("zero value should be invalid")
	}
	if _, err := json.Marshal(schema); err == nil {
		t.Fatal("zero value MarshalJSON should fail")
	}
}

func TestOutputSchemaJSONValueAccessor(t *testing.T) {
	schema, err := OutputSchemaFromJSON([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	value := schema.JSONValue()
	if value.Kind() != JSONKindObject {
		t.Fatalf("schema JSONValue kind = %q, want object", value.Kind())
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"a":1,"b":2}` {
		t.Fatalf("JSONValue marshal = %s", data)
	}
}
