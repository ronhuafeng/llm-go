package schemaoracle

import (
	"encoding/json"
	"fmt"
	"path"
)

// These are existing public SDK contracts, independently specified in
// docs/protocol-sync.md and protocolv2's command/realtime owner tests.
// Source locations select them; candidate Go declarations cannot opt in.
func applySDKSchemaOverlays(doc, root any, fragment, name string) (bool, error) {
	changed := false
	seen := map[string]bool{}
	var visit func(any, string) error
	visit = func(node any, location string) error {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		record := ""
		if location == "#" {
			record = path.Base(name)
		}
		for _, candidate := range []string{"CommandExecParams", "ThreadRealtimeItemStartedNotification", "ThreadRealtimeItemCompletedNotification"} {
			if location == "#/definitions/"+candidate || location == "#/$defs/"+candidate {
				record = candidate + ".json"
			}
		}
		props, _ := m["properties"].(map[string]any)
		switch record {
		case "CommandExecParams.json":
			if raw, exists := props["command"]; exists {
				field, ok := raw.(map[string]any)
				if !ok || field["type"] != "array" {
					return fmt.Errorf("unsupported CommandExecParams.command shape")
				}
				items, ok := field["items"].(map[string]any)
				if !ok || items["type"] != "string" || len(items) != 1 {
					return fmt.Errorf("unsupported CommandExecParams.command items")
				}
				for key := range field {
					switch key {
					case "type", "items", "description", "title", "default", "minItems":
					default:
						return fmt.Errorf("unsupported CommandExecParams.command keyword %s", key)
					}
				}
				if minimum, exists := field["minItems"]; exists {
					v, ok := number(minimum)
					if !ok || (v != 0 && v != 1) {
						return fmt.Errorf("unsupported CommandExecParams.command minItems")
					}
				}
				field["minItems"] = json.Number("1")
				changed = true
			}
		case "ThreadRealtimeItemStartedNotification.json", "ThreadRealtimeItemCompletedNotification.json":
			if raw, exists := props["item"]; exists {
				field, ok := raw.(map[string]any)
				if !ok || field["$ref"] != "#/definitions/ThreadRealtimeItem" {
					return fmt.Errorf("unsupported opaque realtime item shape")
				}
				for key := range field {
					switch key {
					case "$ref", "description", "title":
					default:
						return fmt.Errorf("unsupported opaque realtime item keyword %s", key)
					}
				}
				props["item"] = map[string]any{}
				changed = true
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
			branches, _ := m[key].([]any)
			for i, branch := range branches {
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
