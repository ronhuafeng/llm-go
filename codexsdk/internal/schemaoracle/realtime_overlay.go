package schemaoracle

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// docs/protocol-sync.md and public tests own opaque realtime JSONValue fields
// and non-empty command argv. Enclosing records remain strict and required.
// Do not infer this exception from a candidate Go representation.
// Apply widening before source witness construction, narrowing afterward.
func applyPublicRepresentationOverlays(doc, root any, fragment, name string, narrow bool) error {
	seen := map[string]bool{}
	applied := map[string]bool{}
	var visit func(any, string) error
	visit = func(node any, location string) error {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		if narrow && !applied[location] && (location == "#/definitions/CommandExecParams" || location == "#/$defs/CommandExecParams" || (location == "#" && path.Base(name) == "CommandExecParams.json")) {
			props, _ := m["properties"].(map[string]any)
			command, _ := props["command"].(map[string]any)
			items, _ := command["items"].(map[string]any)
			description, _ := command["description"].(string)
			if command["type"] != "array" || items["type"] != "string" {
				return fmt.Errorf("unsupported CommandExecParams.command overlay source")
			}
			for key := range items {
				if key != "type" {
					return fmt.Errorf("unsupported CommandExecParams.command item keyword %s", key)
				}
			}
			for key := range command {
				if key != "type" && key != "items" && key != "description" && key != "minItems" {
					return fmt.Errorf("unsupported CommandExecParams.command keyword %s", key)
				}
			}
			if minimum, exists := command["minItems"]; exists {
				if fmt.Sprint(minimum) != "1" {
					return fmt.Errorf("unsupported CommandExecParams.command minItems")
				}
			} else if !strings.Contains(description, "Empty arrays are rejected.") {
				return fmt.Errorf("unsupported CommandExecParams.command description")
			}
			command["minItems"] = json.Number("1")
			applied[location] = true
		}
		for _, record := range []string{"ThreadRealtimeItemStartedNotification", "ThreadRealtimeItemCompletedNotification"} {
			if !narrow && !applied[location] && (location == "#/definitions/"+record || location == "#/$defs/"+record || (location == "#" && path.Base(name) == record+".json")) {
				props, _ := m["properties"].(map[string]any)
				field, _ := props["item"].(map[string]any)
				if field == nil || field["$ref"] != "#/definitions/ThreadRealtimeItem" {
					return fmt.Errorf("unsupported %s.item overlay source", record)
				}
				for key := range field {
					if key != "$ref" && key != "description" {
						return fmt.Errorf("unsupported %s.item keyword %s", record, key)
					}
				}
				props["item"] = map[string]any{}
				applied[location] = true
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
		for _, key := range []string{"oneOf", "anyOf", "allOf"} {
			branches, _ := m[key].([]any)
			for i, branch := range branches {
				if err := visit(branch, fmt.Sprintf("%s/%s/%d", location, key, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(root, fragment)
}
