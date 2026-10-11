package schemaoracle

import (
	"encoding/json"
	"fmt"

	native "github.com/google/jsonschema-go/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/message"
)

const outputSchemaKeyword = "x-llm-go-output-schema"

// The accepted public contract is OutputSchemaFromJSON in
// protocolv2/output_schema.go:16-30, with regressions in output_schema_test.go
// and protocol_types_test.go:1633. Raw upstream has only a description here.
// Audit source before inserting this private keyword; upstream cannot opt in.
// Go bindings and schema titles never select the overlay.
func prepareOutputSchema(doc, node any, document, path string, seen map[string]bool) error {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	if path == "#/definitions/TurnStartParams" || path == "#/$defs/TurnStartParams" || (path == "#" && document == "v2/TurnStartParams.json") {
		if properties, ok := m["properties"].(map[string]any); ok {
			if value, exists := properties["outputSchema"]; exists {
				field, ok := value.(map[string]any)
				if !ok || len(field) != 1 || field["description"] == nil {
					return fmt.Errorf("%s/properties/outputSchema: accepted overlay requires description-only source", path)
				}
				field[outputSchemaKeyword] = true
			}
		}
	}
	if ref, ok := m["$ref"].(string); ok && !seen[ref] {
		seen[ref] = true
		target, err := pointer(doc, ref)
		if err != nil {
			return err
		}
		if err := prepareOutputSchema(doc, target, document, ref, seen); err != nil {
			return err
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for _, key := range keys(props) {
			if err := prepareOutputSchema(doc, props[key], document, path+"/properties/"+key, seen); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if err := prepareOutputSchema(doc, m[key], document, path+"/"+key, seen); err != nil {
			return err
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if branches, ok := m[key].([]any); ok {
			for i, branch := range branches {
				if err := prepareOutputSchema(doc, branch, document, fmt.Sprintf("%s/%s/%d", path, key, i), seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func outputSchemaVocabulary() *validator.Vocabulary {
	return &validator.Vocabulary{
		URL: "urn:llm-go:output-schema",
		Compile: func(_ *validator.CompilerContext, node map[string]any) (validator.SchemaExt, error) {
			if node[outputSchemaKeyword] == true {
				return outputSchemaContract{}, nil
			}
			return nil, nil
		},
	}
}

type outputSchemaContract struct{}

func (outputSchemaContract) Validate(ctx *validator.ValidatorContext, value any) {
	if _, ok := value.(bool); ok {
		return
	}
	if _, ok := value.(map[string]any); !ok {
		ctx.AddError(outputSchemaError{"expected JSON Schema object or boolean"})
		return
	}
	// Use the accepted native-schema dependency, never the candidate's wrapper,
	// generator, reflection or tests. Resolve(nil) cannot load remote schemas.
	raw, err := json.Marshal(value)
	var schema native.Schema
	if err == nil {
		err = json.Unmarshal(raw, &schema)
	}
	if err == nil {
		_, err = schema.Resolve(nil)
	}
	if err != nil {
		ctx.AddError(outputSchemaError{err.Error()})
	}
}

type outputSchemaError struct{ reason string }

func (outputSchemaError) KeywordPath() []string { return []string{outputSchemaKeyword} }
func (e outputSchemaError) LocalizedString(p *message.Printer) string {
	return p.Sprintf("invalid OutputSchema: %s", e.reason)
}

func outputSchemaWitnesses() []any {
	return []any{
		map[string]any{}, true, false,
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required":   []any{"answer"},
		},
	}
}
