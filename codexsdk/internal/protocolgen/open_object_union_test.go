package protocolgen

import "testing"

func TestGeneratedOpenObjectUnionAgainstIndependentOracle(t *testing.T) {
	const schema = `{"type":"object","properties":{"content":{"$ref":"#/definitions/Content"}},"required":["content"],"definitions":{"Content":{"anyOf":[{"type":"object","properties":{"text":{"type":"string"},"uri":{"type":"string"}},"required":["text","uri"]},{"type":"object","properties":{"blob":{"type":"string"},"uri":{"type":"string"}},"required":["blob","uri"]}]}}}`
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Kind: TypePlanObjectStructCandidate, GeneratedRoot: true, TypeName: "Container", SchemaPath: "Container.json", Schema: mustParseSchema(t, schema),
		GeneratedDefinitions: map[string]bool{"Content": true},
		Fields:               []FieldPlan{{FieldName: "content", GoType: "Content", Kind: FieldPlanRef, Required: true, RefPath: "Container.json#/definitions/Content", Path: "Container.json#/properties/content"}},
	}}}
	runGeneratedUnionOracle(t, schema, "Container", plan)
}
