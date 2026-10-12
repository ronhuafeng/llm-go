package schemaoracle

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Accepted contract: protocolv2/protocol_types_test.go:3680-3701,3749-3762;
// internal/protocolgen/type_plan.go:1245-1254,1270-1285; and
// docs/protocol-sync.md:113-117. The same-version upstream command field says
// "Empty arrays are rejected." but omits minItems. Apply that reviewed local
// argv invariant only to its source record, preserving all other constraints.
func applyCommandArgvOverlay(doc, root any, fragment, name string) (bool, error) {
	changed := false
	apply := func(record map[string]any) error {
		props, _ := record["properties"].(map[string]any)
		raw, exists := props["command"]
		if !exists {
			return nil
		}
		field, ok := raw.(map[string]any)
		if !ok || field["type"] != "array" {
			return fmt.Errorf("unsupported CommandExecParams.command argv shape")
		}
		items, ok := field["items"].(map[string]any)
		if !ok || items["type"] != "string" {
			return fmt.Errorf("unsupported CommandExecParams.command item shape")
		}
		for key := range items {
			switch key {
			case "type", "title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly":
			default:
				return fmt.Errorf("unsupported CommandExecParams.command item keyword %s", key)
			}
		}
		for key := range field {
			switch key {
			case "type", "items", "minItems", "title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly":
			default:
				return fmt.Errorf("unsupported CommandExecParams.command keyword %s", key)
			}
		}
		if raw, exists := field["minItems"]; exists {
			if min, ok := number(raw); !ok || min != 1 {
				return fmt.Errorf("unsupported CommandExecParams.command minItems")
			}
			return nil
		}
		description, _ := field["description"].(string)
		if !strings.Contains(description, "Empty arrays are rejected.") {
			return fmt.Errorf("unsupported CommandExecParams.command without reviewed argv invariant")
		}
		field["minItems"] = json.Number("1")
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
		if location == "#/definitions/CommandExecParams" || location == "#/$defs/CommandExecParams" || (location == "#" && path.Base(name) == "CommandExecParams.json") {
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
