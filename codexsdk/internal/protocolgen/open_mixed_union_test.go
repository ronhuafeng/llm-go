package protocolgen

import "testing"

func TestMixedUnionOpenBranchRequiresUnconstrainedKinds(t *testing.T) {
	const prefix = `{"anyOf":[{"type":"string","enum":["known"]},{"type":"object","properties":{"status":{"type":"string"}},"required":["status"],"additionalProperties":false},`
	valid := mustParseSchema(t, prefix+`{"type":["string","object"]}]}`)
	if !isMixedUnionDefinitionSchema(valid) {
		t.Fatal("source-valid open mixed union unsupported")
	}
	for _, branch := range []string{
		`{"type":["string","object"],"enum":[]}`,
		`{"type":["string","object"],"anyOf":[]}`,
		`{"type":["string","object"],"minLength":1}`,
		`{"type":["string","object"],"additionalProperties":false}`,
		`{"type":["string","object","null"]}`,
	} {
		if isMixedUnionDefinitionSchema(mustParseSchema(t, prefix+branch+`]}`)) {
			t.Errorf("constrained or different fallback silently admitted: %s", branch)
		}
	}
}

func TestImageAlternativesDoNotDiscardConstraints(t *testing.T) {
	for _, property := range []string{`{"type":"string","enum":[]}`, `{"type":"string","minLength":1}`, `{"type":"string","anyOf":[]}`} {
		schema := mustParseSchema(t, `{"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["image"]}},"required":["type"],"anyOf":[{"type":"object","properties":{"url":`+property+`},"required":["url"]},{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}]}]}`)
		_, err := GenerateProtocolTypes(ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, SchemaPath: "Input.json", TypeName: "Input", Schema: schema, GeneratedRoot: true}}})
		if err == nil {
			t.Errorf("alternative constraint silently discarded: %s", property)
		}
	}
}
