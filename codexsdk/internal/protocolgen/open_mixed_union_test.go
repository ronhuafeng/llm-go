package protocolgen

import "testing"

func TestGeneratedOpenMixedUnionAgainstIndependentOracle(t *testing.T) {
	const schema = `{"type":"object","properties":{"error":{"$ref":"#/definitions/Info"}},"required":["error"],"definitions":{"Info":{"anyOf":[{"type":"string","enum":["known"]},{"type":"object","additionalProperties":false,"properties":{"http":{"type":"object","properties":{"code":{"type":["integer","null"],"format":"uint16","minimum":0}}}},"required":["http"]},{"type":["string","object"]}]}}}`
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Kind: TypePlanObjectStructCandidate, GeneratedRoot: true, TypeName: "Container", SchemaPath: "Container.json", Schema: mustParseSchema(t, schema),
		GeneratedDefinitions: map[string]bool{"Info": true},
		Fields:               []FieldPlan{{FieldName: "error", GoType: "Info", Kind: FieldPlanRef, Required: true, RefPath: "Container.json#/definitions/Info", Path: "Container.json#/properties/error"}},
	}}}
	runGeneratedUnionOracle(t, schema, "Container", plan)
}

func TestOpenMixedUnionFallbackRequiresUnconstrainedDeclaredKinds(t *testing.T) {
	for _, fallback := range []string{
		`{"type":["string","object"],"minLength":1}`,
		`{"type":["string","object"],"additionalProperties":false}`,
		`{"type":["string","object","null"]}`,
		`{"type":["string","object"],"mystery":true}`,
	} {
		if isOpenMixedUnionFallback(mustParseSchema(t, fallback)) {
			t.Fatalf("constrained or unsupported fallback accepted: %s", fallback)
		}
	}
}
