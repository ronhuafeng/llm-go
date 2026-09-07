package llmschema

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSchemaJSONForProjectsGoType(t *testing.T) {
	type verdict struct {
		Answer string `json:"answer" jsonschema:"final answer"`
		Score  int    `json:"score,omitempty"`
	}

	data, err := SchemaJSONFor[verdict]()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"answer"`) || !strings.Contains(string(data), `"final answer"`) {
		t.Fatalf("SchemaJSONFor output missing projected field facts: %s", data)
	}
}

func TestDecodeValidatesSchemaBeforeUnmarshal(t *testing.T) {
	type child struct {
		Name string `json:"name"`
	}
	type output struct {
		Child child  `json:"child"`
		Flags []bool `json:"flags"`
	}

	_, err := Decode[output]([]byte(`{"child":{"name":7},"flags":[true,"no"]}`))
	var validationErr *SchemaValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Decode error = %v, want SchemaValidationError", err)
	}
	if len(validationErr.Violations) == 0 {
		t.Fatal("SchemaValidationError has no violations")
	}
	want := Violation{Path: "/child/name", Keyword: "type", Message: "type constraint failed"}
	if !reflect.DeepEqual(validationErr.Violations[0], want) {
		t.Fatalf("first violation = %#v, want %#v", validationErr.Violations[0], want)
	}
	if strings.Contains(validationErr.Error(), "7") || strings.Contains(validationErr.Error(), `"no"`) {
		t.Fatalf("validation error retained raw model values: %v", validationErr)
	}
}

func TestDecodeEnforcesRequiredNullableScalarListAndMapConstraints(t *testing.T) {
	type output struct {
		Name   string         `json:"name"`
		Note   *string        `json:"note,omitempty"`
		Scores []int          `json:"scores"`
		Labels map[string]int `json:"labels"`
	}

	tests := []string{
		`{"note":null,"scores":[1],"labels":{"a":2}}`,
		`{"name":false,"scores":[1],"labels":{"a":2}}`,
		`{"name":"ok","scores":["bad"],"labels":{"a":2}}`,
		`{"name":"ok","scores":[1],"labels":{"a":"bad"}}`,
	}
	for _, input := range tests {
		_, err := Decode[output]([]byte(input))
		var validationErr *SchemaValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("Decode(%s) error = %v, want SchemaValidationError", input, err)
		}
	}

	if _, err := Decode[output]([]byte(`{"name":"ok","note":null,"scores":[1],"labels":{"a":2}}`)); err != nil {
		t.Fatalf("Decode rejected valid nullable output: %v", err)
	}
}

func TestDecodeRejectsTrailingJSONValue(t *testing.T) {
	if _, err := Decode[bool]([]byte(`true false`)); err == nil {
		t.Fatal("Decode accepted multiple JSON values")
	}
}

func TestDecodeEnforcesAdditionalPropertiesConstraint(t *testing.T) {
	type output struct {
		Name string `json:"name"`
	}
	_, err := Decode[output]([]byte(`{"name":"ok","unexpected":true}`))
	var validationErr *SchemaValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Decode error = %v, want SchemaValidationError", err)
	}
	found := false
	for _, violation := range validationErr.Violations {
		if violation.Keyword == "additionalProperties" {
			found = true
		}
	}
	if !found {
		t.Fatalf("violations = %#v, want additionalProperties", validationErr.Violations)
	}
}

func TestSchemaJSONForTreatsRawMessageAsArbitraryJSON(t *testing.T) {
	type output struct {
		Evidence json.RawMessage `json:"evidence,omitempty"`
	}

	data, err := SchemaJSONFor[output]()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"evidence":true`) {
		t.Fatalf("RawMessage should project as arbitrary JSON schema: %s", data)
	}
}

func TestCompileOwnsSchemaAndDecode(t *testing.T) {
	type verdict struct {
		Passed bool `json:"passed"`
	}
	contract, err := Compile[verdict]()
	if err != nil {
		t.Fatal(err)
	}
	if !contract.Compiled() {
		t.Fatal("compiled contract reports uncompiled")
	}
	schema := contract.SchemaJSON()
	projected, err := SchemaJSONFor[verdict]()
	if err != nil {
		t.Fatal(err)
	}
	if string(schema) != string(projected) {
		t.Fatalf("contract schema = %s, want %s", schema, projected)
	}
	schema[0] = 'x'
	if string(contract.SchemaJSON()) != string(projected) {
		t.Fatal("SchemaJSON returned the owned schema, not a copy")
	}
	got, err := contract.Decode([]byte(`{"passed":true}`))
	if err != nil || !got.Passed {
		t.Fatalf("Decode = %#v, %v", got, err)
	}
}

func TestZeroContractDoesNotGuessSchema(t *testing.T) {
	var contract Contract[bool]
	if contract.Compiled() || contract.SchemaJSON() != nil {
		t.Fatalf("zero contract leaked a schema: compiled=%t schema=%s", contract.Compiled(), contract.SchemaJSON())
	}
	_, err := contract.Decode([]byte(`true`))
	if !errors.Is(err, ErrUncompiledContract) {
		t.Fatalf("zero Decode error = %v, want ErrUncompiledContract", err)
	}
}

func TestContractDecodePreservesValidationViolations(t *testing.T) {
	type output struct {
		Name string `json:"name"`
	}
	contract, err := Compile[output]()
	if err != nil {
		t.Fatal(err)
	}
	_, err = contract.Decode([]byte(`{"name":"ok","unexpected":true}`))
	var validationErr *SchemaValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Decode error = %v, want SchemaValidationError", err)
	}
	found := false
	for _, violation := range validationErr.Violations {
		if violation.Keyword == "additionalProperties" {
			found = true
		}
	}
	if !found {
		t.Fatalf("violations = %#v, want additionalProperties", validationErr.Violations)
	}
}

func TestDecodeStructuredOutput(t *testing.T) {
	type verdict struct {
		Passed bool `json:"passed"`
	}

	got, err := Decode[verdict]([]byte(`{"passed":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed {
		t.Fatalf("Decode passed = false, want true")
	}

	if _, err := Decode[verdict]([]byte(`{"passed":`)); err == nil {
		t.Fatal("Decode accepted invalid JSON")
	}
}
