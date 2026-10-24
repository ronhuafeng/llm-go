package schemaoracle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// These public overlays predate this upgrade. Their owners are
// protocolgen/type_plan.go and protocolv2/protocol_types_test.go (nonempty
// CommandExecParams.command, JSONValue realtime notification items, and the
// exclusive closed ResourceContent records and opaque MCP elicitation properties).
// Change only the validation document: keep every source witness and mutation.
// ContractDocument applies overlays for validation and typed reachability.
func ContractDocument(doc any, fragment, name string) (any, bool, error) {
	if fragment == "" {
		fragment = "#"
	} else if !strings.HasPrefix(fragment, "#") {
		fragment = "#" + fragment
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, false, err
	}
	var contract any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&contract); err != nil {
		return nil, false, err
	}
	root, err := pointer(contract, fragment)
	if err != nil {
		return nil, false, err
	}
	changed := false
	seen := map[string]bool{}
	var visit func(any, string) error
	visit = func(node any, location string) error {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		isRecord := func(record string) bool {
			return location == "#/definitions/"+record || location == "#/$defs/"+record || location == "#" && path.Base(name) == record+".json"
		}
		props, _ := m["properties"].(map[string]any)
		if isRecord("McpElicitationSchema") {
			properties, _ := props["properties"].(map[string]any)
			values, _ := properties["additionalProperties"].(map[string]any)
			if properties["type"] != "object" || len(values) != 1 || values["$ref"] != "#/definitions/McpElicitationPrimitiveSchema" {
				return fmt.Errorf("unsupported elicitation properties overlay shape")
			}
			properties["additionalProperties"] = true
			changed = true
		}
		if isRecord("ResourceContent") {
			branches, _ := m["anyOf"].([]any)
			if len(branches) != 2 {
				return fmt.Errorf("unsupported ResourceContent overlay alternatives")
			}
			for i, key := range []string{"text", "blob"} {
				branch, _ := branches[i].(map[string]any)
				fields, _ := branch["properties"].(map[string]any)
				payload, _ := fields[key].(map[string]any)
				uri, _ := fields["uri"].(map[string]any)
				if branch["type"] != "object" || payload["type"] != "string" || uri["type"] != "string" {
					return fmt.Errorf("unsupported ResourceContent overlay branch")
				}
				branch["additionalProperties"] = false
			}
			changed = true
		}
		if isRecord("CommandExecParams") {
			command, _ := props["command"].(map[string]any)
			items, _ := command["items"].(map[string]any)
			if command["type"] != "array" || items["type"] != "string" {
				return fmt.Errorf("unsupported CommandExecParams.command overlay shape")
			}
			for key := range items {
				if key != "type" {
					return fmt.Errorf("unsupported command item constraint %s", key)
				}
			}
			for key := range command {
				if key != "type" && key != "items" && key != "description" && key != "minItems" {
					return fmt.Errorf("unsupported command constraint %s", key)
				}
			}
			minimum, exists := command["minItems"]
			if exists && minimum != json.Number("1") {
				return fmt.Errorf("unsupported command minItems %v", minimum)
			}
			if !exists {
				description, _ := command["description"].(string)
				if !strings.Contains(description, "Empty arrays are rejected.") {
					return fmt.Errorf("command overlay lacks reviewed source contract")
				}
				command["minItems"] = json.Number("1")
				changed = true
			}
		}
		if isRecord("ThreadRealtimeItemCompletedNotification") || isRecord("ThreadRealtimeItemStartedNotification") {
			item, _ := props["item"].(map[string]any)
			if len(item) != 1 || item["$ref"] != "#/definitions/ThreadRealtimeItem" {
				return fmt.Errorf("unsupported realtime notification item overlay shape")
			}
			props["item"] = true
			changed = true
		}
		if ref, ok := m["$ref"].(string); ok && !seen[ref] {
			seen[ref] = true
			target, err := pointer(contract, ref)
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
	err = visit(root, fragment)
	return contract, changed, err
}
