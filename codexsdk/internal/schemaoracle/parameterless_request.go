package schemaoracle

import "path"

// Accepted ClientRequest encoding omits optional null-only params after decoding
// them (protocolv2/protocol_types_test.go, TestGeneratedClientRequestMarshalAndUnmarshal;
// docs/verifier-repair-flow.md, Evaluation). Source identity selects the envelope;
// neither a Go binding nor an arbitrary property named params grants this rule.
// Stay conservative on schema evolution: only direct singleton method branches
// with required id/method and explicit null-only params are covered.
func parameterlessRequestMethods(root any, name, fragment string) []string {
	if path.Base(name) != "ClientRequest.json" || fragment != "#" {
		return nil
	}
	r, _ := root.(map[string]any)
	// Additional envelope constraints (for example a parent required params)
	// need separate contract review, not extrapolation from today's branches.
	for key := range r {
		switch key {
		case "$schema", "definitions", "$defs", "oneOf", "title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly":
		default:
			return nil
		}
	}
	branches, _ := r["oneOf"].([]any)
	counts := map[string]int{}
	eligible := map[string]bool{}
	for _, node := range branches {
		branch, _ := node.(map[string]any)
		if _, referenced := branch["$ref"]; referenced {
			return nil
		}
		properties, _ := branch["properties"].(map[string]any)
		methodSchema, _ := properties["method"].(map[string]any)
		if _, referenced := methodSchema["$ref"]; referenced {
			return nil
		}
		methods, _ := methodSchema["enum"].([]any)
		if len(methods) != 1 || methodSchema["type"] != "string" {
			// A broad/unknown discriminator could overlap an eligible branch.
			return nil
		}
		method, ok := methods[0].(string)
		if !ok || method == "" {
			return nil
		}
		counts[method]++
		required := map[string]bool{}
		fields, _ := branch["required"].([]any)
		for _, field := range fields {
			if key, ok := field.(string); ok {
				required[key] = true
			}
		}
		params, _ := properties["params"].(map[string]any)
		if _, referenced := params["$ref"]; referenced {
			continue // Draft-07 references can ignore the sibling type keyword.
		}
		simple := true
		for key := range branch {
			switch key {
			case "type", "properties", "required", "additionalProperties", "title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly":
			default:
				simple = false
			}
		}
		if simple && branch["type"] == "object" && required["id"] && required["method"] && !required["params"] && params["type"] == "null" {
			eligible[method] = true
		}
	}
	var result []string
	for _, method := range keys(eligible) {
		if counts[method] == 1 {
			result = append(result, method)
		}
	}
	return result
}
