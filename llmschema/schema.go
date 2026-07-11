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

// Decode validates and unmarshals provider structured output into the expected Go type.
func Decode[T any](data []byte) (T, error) {
	var value T
	instance, err := decodeJSON(data)
	if err != nil {
		return value, fmt.Errorf("decode structured output: %w", err)
	}
	schemaJSON, err := SchemaJSONFor[T]()
	if err != nil {
		return value, err
	}
	if err := validate(schemaJSON, instance); err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode structured output: %w", err)
	}
	return value, nil
}

// DecodeString validates and unmarshals provider structured output text.
//
// Deprecated: use Decode.
func DecodeString[T any](text string) (T, error) {
	return Decode[T]([]byte(text))
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

func validate(schemaJSON json.RawMessage, instance any) error {
	var schemaDocument any
	decoder := json.NewDecoder(bytes.NewReader(schemaJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&schemaDocument); err != nil {
		return fmt.Errorf("decode generated schema: %w", err)
	}
	compiler := validator.NewCompiler()
	const schemaURL = "https://llmkit.local/output-schema.json"
	if err := compiler.AddResource(schemaURL, schemaDocument); err != nil {
		return fmt.Errorf("register generated schema: %w", err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return fmt.Errorf("compile generated schema: %w", err)
	}
	if err := compiled.Validate(instance); err != nil {
		var validationErr *validator.ValidationError
		if !errors.As(err, &validationErr) {
			return fmt.Errorf("validate structured output: %w", err)
		}
		violations := collectViolations(validationErr)
		return &SchemaValidationError{Violations: violations}
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
