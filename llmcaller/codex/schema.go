package codexcaller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaPolicyError identifies a stable schema-admission kind and JSON pointer.
type SchemaPolicyError struct {
	Path string
	Kind string
	Err  error
}

func (e *SchemaPolicyError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("llmcaller/codex: schema policy %s at %s: %v", e.Kind, e.Path, e.Err)
}

func (e *SchemaPolicyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// StrictOutputSchemaFromJSON admits a caller-owned JSON Schema only when the
// Codex representation can carry it without changing the accepted instance
// language. Unsupported representation requirements fail closed; this function
// never makes an optional property required or otherwise narrows the contract.
func StrictOutputSchemaFromJSON(raw json.RawMessage) (protocolv2.OutputSchema, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return protocolv2.OutputSchema{}, ErrMissingSchemaJSON
	}
	parsed, err := protocolv2.ParseJSONValue(raw)
	if err != nil {
		return protocolv2.OutputSchema{}, &SchemaPolicyError{Path: "", Kind: "invalid_json", Err: err}
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		return protocolv2.OutputSchema{}, &SchemaPolicyError{Path: "", Kind: "invalid_json", Err: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return protocolv2.OutputSchema{}, &SchemaPolicyError{Path: "", Kind: "invalid_json", Err: err}
	}
	draft, err := supportedSchemaDraft(root)
	if err != nil {
		return protocolv2.OutputSchema{}, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(draft)
	compiler.UseLoader(rejectSchemaResourceLoader{})
	const schemaResourceURL = "https://llmcaller.invalid/output-schema.json"
	if err := compiler.AddResource(schemaResourceURL, root); err != nil {
		return protocolv2.OutputSchema{}, &SchemaPolicyError{Path: "", Kind: "invalid_schema", Err: err}
	}
	admission := schemaAdmission{root: root}
	if err := admission.walk(root, "", nil); err != nil {
		return protocolv2.OutputSchema{}, err
	}
	if _, err := compiler.Compile(schemaResourceURL); err != nil {
		return protocolv2.OutputSchema{}, &SchemaPolicyError{Path: "", Kind: "invalid_schema", Err: err}
	}
	out, err := json.Marshal(root)
	if err != nil {
		return protocolv2.OutputSchema{}, &SchemaPolicyError{Path: "", Kind: "marshal", Err: err}
	}
	schema, err := protocolv2.OutputSchemaFromJSON(out)
	if err != nil {
		return protocolv2.OutputSchema{}, &SchemaPolicyError{Path: "", Kind: "invalid_schema", Err: err}
	}
	return schema, nil
}

type schemaAdmission struct {
	root any
}

type rejectSchemaResourceLoader struct{}

func (rejectSchemaResourceLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resource %q is unsupported", url)
}

func (a schemaAdmission) walk(value any, path string, refs map[string]bool) error {
	object, ok := value.(map[string]any)
	if !ok {
		if _, boolean := value.(bool); boolean {
			return nil
		}
		return &SchemaPolicyError{Path: path, Kind: "invalid_subschema", Err: errors.New("subschema must be an object or boolean")}
	}
	if refValue, exists := object["$ref"]; exists {
		ref, ok := refValue.(string)
		if !ok {
			return &SchemaPolicyError{Path: path + "/$ref", Kind: "invalid_ref", Err: errors.New("$ref must be a string")}
		}
		if !strings.HasPrefix(ref, "#") {
			return &SchemaPolicyError{Path: path + "/$ref", Kind: "external_ref", Err: fmt.Errorf("external reference %q is unsupported", ref)}
		}
		if refs[ref] {
			return &SchemaPolicyError{Path: path + "/$ref", Kind: "cyclic_ref", Err: fmt.Errorf("cyclic reference %q", ref)}
		}
		resolved, err := resolveLocalRef(a.root, ref)
		if err != nil {
			return &SchemaPolicyError{Path: path + "/$ref", Kind: "unresolvable_ref", Err: err}
		}
		nextRefs := copyRefSet(refs)
		nextRefs[ref] = true
		if err := a.walk(resolved, refPath(ref), nextRefs); err != nil {
			return err
		}
	}
	if _, exists := object["$dynamicRef"]; exists {
		return &SchemaPolicyError{Path: path + "/$dynamicRef", Kind: "unsupported_dynamic_ref", Err: errors.New("dynamic references are unsupported")}
	}
	if _, exists := object["$vocabulary"]; exists {
		return &SchemaPolicyError{Path: path + "/$vocabulary", Kind: "unsupported_vocabulary", Err: errors.New("custom vocabulary declarations are unsupported")}
	}
	if properties, exists := objectMap(object["properties"]); exists {
		required, err := requiredSet(object["required"], path)
		if err != nil {
			return err
		}
		for name, property := range properties {
			propertyPath := path + "/properties/" + escapePointer(name)
			if !required[name] {
				return &SchemaPolicyError{
					Path: propertyPath,
					Kind: "optional_property_unsupported",
					Err:  errors.New("Codex representation cannot require this property without narrowing the caller contract"),
				}
			}
			if err := a.walk(property, propertyPath, refs); err != nil {
				return err
			}
		}
	} else if _, present := object["properties"]; present {
		return &SchemaPolicyError{Path: path + "/properties", Kind: "invalid_properties", Err: errors.New("properties must be an object")}
	}
	for _, key := range []string{"additionalProperties", "unevaluatedProperties", "propertyNames", "additionalItems", "contains", "unevaluatedItems", "not", "if", "then", "else", "contentSchema"} {
		if child, exists := object[key]; exists {
			if err := a.walk(child, path+"/"+escapePointer(key), refs); err != nil {
				return err
			}
		}
	}
	if items, exists := object["items"]; exists {
		if list, ok := items.([]any); ok {
			for index, child := range list {
				if err := a.walk(child, fmt.Sprintf("%s/items/%d", path, index), refs); err != nil {
					return err
				}
			}
		} else if err := a.walk(items, path+"/items", refs); err != nil {
			return err
		}
	}
	for _, key := range []string{"properties", "patternProperties", "dependentSchemas", "$defs", "definitions"} {
		children, exists := objectMap(object[key])
		if !exists {
			continue
		}
		for name, child := range children {
			if key == "properties" {
				continue
			}
			if err := a.walk(child, path+"/"+escapePointer(key)+"/"+escapePointer(name), refs); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		if children, exists := object[key]; exists {
			list, ok := children.([]any)
			if !ok {
				return &SchemaPolicyError{Path: path + "/" + key, Kind: "invalid_subschemas", Err: errors.New("keyword must be an array")}
			}
			for index, child := range list {
				if err := a.walk(child, fmt.Sprintf("%s/%s/%d", path, key, index), refs); err != nil {
					return err
				}
			}
		}
	}
	if dependencies, exists := objectMap(object["dependencies"]); exists {
		for name, dependency := range dependencies {
			if _, propertyList := dependency.([]any); propertyList {
				continue
			}
			if err := a.walk(dependency, path+"/dependencies/"+escapePointer(name), refs); err != nil {
				return err
			}
		}
	} else if _, present := object["dependencies"]; present {
		return &SchemaPolicyError{Path: path + "/dependencies", Kind: "invalid_dependencies", Err: errors.New("dependencies must be an object")}
	}
	return nil
}

func requiredSet(value any, path string) (map[string]bool, error) {
	set := map[string]bool{}
	if value == nil {
		return set, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, &SchemaPolicyError{Path: path + "/required", Kind: "invalid_required", Err: errors.New("required must be an array")}
	}
	for _, item := range list {
		name, ok := item.(string)
		if !ok {
			return nil, &SchemaPolicyError{Path: path + "/required", Kind: "invalid_required", Err: errors.New("required entries must be strings")}
		}
		set[name] = true
	}
	return set, nil
}

func resolveLocalRef(root any, ref string) (any, error) {
	if ref == "#" {
		return root, nil
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("unsupported local reference %q", ref)
	}
	current := root
	for _, encoded := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reference %q traverses a non-object", ref)
		}
		current, ok = object[token]
		if !ok {
			return nil, fmt.Errorf("reference %q does not exist", ref)
		}
	}
	return current, nil
}

func objectMap(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func supportedSchemaDraft(root any) (*jsonschema.Draft, error) {
	object, ok := root.(map[string]any)
	if !ok {
		return jsonschema.Draft2020, nil
	}
	value, exists := object["$schema"]
	if !exists {
		return jsonschema.Draft2020, nil
	}
	identifier, ok := value.(string)
	if !ok {
		return nil, &SchemaPolicyError{Path: "", Kind: "invalid_schema", Err: errors.New("$schema must be a string")}
	}
	switch identifier {
	case "http://json-schema.org/draft-07/schema#":
		return jsonschema.Draft7, nil
	case "https://json-schema.org/draft/2020-12/schema":
		return jsonschema.Draft2020, nil
	default:
		return nil, &SchemaPolicyError{Path: "", Kind: "invalid_schema", Err: fmt.Errorf("unsupported $schema %q", identifier)}
	}
}

func copyRefSet(refs map[string]bool) map[string]bool {
	copied := make(map[string]bool, len(refs)+1)
	for ref, present := range refs {
		copied[ref] = present
	}
	return copied
}

func escapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

func refPath(ref string) string {
	if ref == "#" {
		return ""
	}
	return strings.TrimPrefix(ref, "#")
}
