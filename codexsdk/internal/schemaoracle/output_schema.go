package schemaoracle

import (
	"encoding/json"
	"fmt"
	"path"

	native "github.com/google/jsonschema-go/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

const outputSchemaKeyword = "__oracle_output_schema__"

// The public overlay is owned by protocolv2/output_schema.go:17-30,61-73
// and output_schema_test.go:9-79, not by the upstream unconstrained JSON field.
// Identify the record by its source path/definition, never its Go binding or
// an arbitrary property named outputSchema. Audit source before adding the
// private keyword, so upstream cannot opt itself into this overlay.
func applyOutputSchemaOverlay(doc, root any, fragment, name string) (bool, error) {
	changed := false
	apply := func(record any) error {
		r, _ := record.(map[string]any)
		props, _ := r["properties"].(map[string]any)
		field, exists := props["outputSchema"]
		if !exists {
			return nil
		}
		// Retain every source constraint. Only the accepted unconstrained
		// representation is covered; constrained evolution remains fail-closed.
		if b, ok := field.(bool); ok && b {
			field = map[string]any{}
			props["outputSchema"] = field
		}
		f, ok := field.(map[string]any)
		if !ok {
			return fmt.Errorf("unsupported constrained TurnStartParams.outputSchema")
		}
		if f[outputSchemaKeyword] == true {
			return nil // A recursive reference can revisit the same audited record.
		}
		for key := range f {
			switch key {
			case "title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly":
			default:
				return fmt.Errorf("unsupported TurnStartParams.outputSchema keyword %s", key)
			}
		}
		f[outputSchemaKeyword] = true
		changed = true
		return nil
	}
	seen := map[string]bool{}
	var visit func(any, string) error
	visit = func(node any, location string) error {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		if location == "#/definitions/TurnStartParams" || location == "#/$defs/TurnStartParams" || (location == "#" && path.Base(name) == "TurnStartParams.json") {
			if err := apply(m); err != nil {
				return err
			}
		}
		if ref, ok := m["$ref"].(string); ok && !seen[ref] {
			seen[ref] = true
			target, err := pointer(doc, ref)
			if err != nil {
				return err
			}
			if err := visit(target, ref); err != nil {
				return err
			}
		}
		props, _ := m["properties"].(map[string]any)
		for _, key := range keys(props) {
			if err := visit(props[key], location+"/properties/"+key); err != nil {
				return err
			}
		}
		for _, key := range []string{"items", "additionalProperties"} {
			if err := visit(m[key], location+"/"+key); err != nil {
				return err
			}
		}
		for _, key := range []string{"anyOf", "oneOf", "allOf"} {
			alternatives, _ := m[key].([]any)
			for i, branch := range alternatives {
				if err := visit(branch, fmt.Sprintf("%s/%s/%d", location, key, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := visit(root, fragment)
	return changed, err
}

func outputSchemaVocabulary() *validator.Vocabulary {
	return &validator.Vocabulary{
		URL: "https://schema-oracle.invalid/output-schema",
		Compile: func(_ *validator.CompilerContext, obj map[string]any) (validator.SchemaExt, error) {
			if obj[outputSchemaKeyword] == true {
				return outputSchemaAssertion{}, nil
			}
			return nil, nil
		},
	}
}

type outputSchemaAssertion struct{}

func (outputSchemaAssertion) Validate(ctx *validator.ValidatorContext, value any) {
	if err := validateOutputSchemaDocument(value); err != nil {
		ctx.AddError(&kind.Format{Got: value, Want: "valid JSON Schema document", Err: err})
	}
}

func validateOutputSchemaDocument(value any) error {
	if _, ok := value.(bool); ok {
		return nil
	}
	if _, ok := value.(map[string]any); !ok {
		return fmt.Errorf("output schema must be an object or boolean")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var schema native.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	_, err = schema.Resolve(nil)
	return err
}

func outputSchemaWitnesses() []any {
	// Valid and invalid native documents are all valid upstream JSON values.
	// The independent assertion classifies them, including nested schema shape
	// and regex errors. Start with a valid object for enclosing-record witnesses.
	raw := []string{
		`{}`, `true`, `false`, `{"type":"object","properties":{"answer":{"type":"integer"}},"required":["answer"],"additionalProperties":false}`,
		`{"pattern":"^[a-z]+$"}`, `{"$defs":{"answer":{"type":"integer"}},"$ref":"#/$defs/answer"}`,
		`null`, `[]`, `"schema"`, `1`, `{"type":1}`, `{"pattern":"["}`, `{"$defs":{},"definitions":{}}`, `{"properties":{"answer":{"type":1}}}`,
	}
	values := make([]any, 0, len(raw))
	for _, input := range raw {
		var value any
		// These fixed literals contain only exactly representable integers.
		if err := json.Unmarshal([]byte(input), &value); err != nil {
			panic(err)
		}
		values = append(values, value)
	}
	return values
}
