package llmschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// Violation identifies one failed JSON Schema constraint.
type Violation struct {
	Path    string
	Keyword string
	Message string
}

// SchemaValidationError reports stable structured-output violations.
type SchemaValidationError struct {
	Violations []Violation
}

func (e *SchemaValidationError) Error() string {
	if e == nil || len(e.Violations) == 0 {
		return "structured output failed schema validation"
	}
	return fmt.Sprintf("structured output failed schema validation: %s at %s", e.Violations[0].Keyword, e.Violations[0].Path)
}

// ErrUncompiledContract reports a Contract that was never compiled.
var ErrUncompiledContract = errors.New("llmschema: contract is not compiled")

// Contract is one compiled provider-neutral structured-output type. It owns
// the exact schema JSON used for a request and the compiled validator used
// to decode matching responses. The zero value is uncompiled: SchemaJSON
// returns nil and Decode returns ErrUncompiledContract. It does not guess
// or regenerate a schema.
type Contract[T any] struct {
	schema   json.RawMessage
	compiled *validator.Schema
}

// Compile projects T into provider-neutral JSON Schema and compiles the
// structural validator once.
func Compile[T any]() (Contract[T], error) {
	schema, err := SchemaJSONFor[T]()
	if err != nil {
		return Contract[T]{}, err
	}
	compiled, err := compileSchema(schema)
	if err != nil {
		return Contract[T]{}, err
	}
	return Contract[T]{
		schema:   append(json.RawMessage(nil), schema...),
		compiled: compiled,
	}, nil
}

// Compiled reports whether Compile populated this contract.
func (c Contract[T]) Compiled() bool {
	return c.compiled != nil
}

// SchemaJSON returns a copy of the owned provider-neutral schema. The zero
// contract returns nil and does not project a schema.
func (c Contract[T]) SchemaJSON() json.RawMessage {
	if c.compiled == nil || len(c.schema) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), c.schema...)
}

// Decode validates data against the owned compiled validator and unmarshals
// it into T. The zero contract returns ErrUncompiledContract.
func (c Contract[T]) Decode(data []byte) (T, error) {
	var value T
	if c.compiled == nil {
		return value, ErrUncompiledContract
	}
	instance, err := decodeJSON(data)
	if err != nil {
		return value, fmt.Errorf("decode structured output: %w", err)
	}
	if err := validateCompiled(c.compiled, instance); err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode structured output: %w", err)
	}
	return value, nil
}

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

// Decode compiles the contract for T and decodes one value. Prefer Compile
// when the same type is decoded or requested more than once.
func Decode[T any](data []byte) (T, error) {
	contract, err := Compile[T]()
	if err != nil {
		var zero T
		return zero, err
	}
	return contract.Decode(data)
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err == nil {
		return nil, errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return instance, nil
}

func compileSchema(schemaJSON json.RawMessage) (*validator.Schema, error) {
	var schemaDocument any
	decoder := json.NewDecoder(bytes.NewReader(schemaJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&schemaDocument); err != nil {
		return nil, fmt.Errorf("decode generated schema: %w", err)
	}
	compiler := validator.NewCompiler()
	const schemaURL = "https://llmkit.local/output-schema.json"
	if err := compiler.AddResource(schemaURL, schemaDocument); err != nil {
		return nil, fmt.Errorf("register generated schema: %w", err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile generated schema: %w", err)
	}
	return compiled, nil
}

func validateCompiled(compiled *validator.Schema, instance any) error {
	if err := compiled.Validate(instance); err != nil {
		var validationErr *validator.ValidationError
		if !errors.As(err, &validationErr) {
			return fmt.Errorf("validate structured output: %w", err)
		}
		return &SchemaValidationError{Violations: collectViolations(validationErr)}
	}
	return nil
}

func collectViolations(root *validator.ValidationError) []Violation {
	var violations []Violation
	var visit func(*validator.ValidationError)
	visit = func(current *validator.ValidationError) {
		if len(current.Causes) > 0 {
			for _, cause := range current.Causes {
				visit(cause)
			}
			return
		}
		keywordPath := current.ErrorKind.KeywordPath()
		keyword := "schema"
		if len(keywordPath) > 0 {
			keyword = keywordPath[len(keywordPath)-1]
		}
		violations = append(violations, Violation{
			Path:    jsonPointer(current.InstanceLocation),
			Keyword: keyword,
			Message: keyword + " constraint failed",
		})
	}
	visit(root)
	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		if violations[i].Keyword != violations[j].Keyword {
			return violations[i].Keyword < violations[j].Keyword
		}
		return violations[i].Message < violations[j].Message
	})
	return violations
}

func jsonPointer(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	var pointer strings.Builder
	for _, token := range tokens {
		pointer.WriteByte('/')
		token = strings.ReplaceAll(token, "~", "~0")
		token = strings.ReplaceAll(token, "/", "~1")
		pointer.WriteString(token)
	}
	return pointer.String()
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
