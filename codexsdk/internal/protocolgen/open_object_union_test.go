package protocolgen

import "testing"

func TestGeneratedOpenObjectUnionContracts(t *testing.T) {
	const schema = `{"type":"object","properties":{"content":{"$ref":"#/definitions/Content"}},"required":["content"],"definitions":{"Content":{"anyOf":[{"type":"object","properties":{"text":{"type":"string"},"uri":{"type":"string"}},"required":["text","uri"]},{"type":"object","properties":{"blob":{"type":"string"},"uri":{"type":"string"}},"required":["blob","uri"]}]}}}`
	plan := ProtocolTypePlan{Types: []TypePlan{{
		Kind: TypePlanObjectStructCandidate, GeneratedRoot: true, TypeName: "Container", SchemaPath: "Container.json", Schema: mustParseSchema(t, schema),
		GeneratedDefinitions: map[string]bool{"Content": true},
		Fields:               []FieldPlan{{FieldName: "content", GoType: "Content", Kind: FieldPlanRef, Required: true, RefPath: "Container.json#/definitions/Content", Path: "Container.json#/properties/content"}},
	}}}
	runGeneratedContracts(t, "Container", plan, []string{
		`{"content":{"text":"t","uri":"u"}}`,
		`{"content":{"blob":"b","uri":"u"}}`,
		`{"content":{"text":"t","blob":"b","uri":"u"}}`,
		`{"content":{"text":"t","blob":null,"uri":"u"}}`,
	}, []string{
		`{"content":{"uri":"u"}}`,
		`{"content":{"text":5,"uri":"u"}}`,
		`{"content":null}`,
		`{}`,
	})
}

// Reduced from rust-v0.167.0 AccountSessionsSwitchResponse: the login
// alternative requires two fields. Its name must not replace branch validation.
func TestGeneratedObjectAnyOfMultipleRequiredFields(t *testing.T) {
	const schema = `{"anyOf":[{"type":"object","properties":{"sessions":{"type":"array","items":{"type":"string"}}},"required":["sessions"]},{"type":"object","properties":{"auth_url":{"type":"string"},"login_id":{"type":"string"}},"required":["auth_url","login_id"]}]}`
	plan := ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanAnyOfDeferred, GeneratedRoot: true, TypeName: "SwitchResponse", SchemaPath: "SwitchResponse.json", Schema: mustParseSchema(t, schema)}}}
	runGeneratedContracts(t, "SwitchResponse", plan, []string{
		`{"sessions":[]}`,
		`{"auth_url":"url","login_id":"id"}`,
		`{"sessions":[],"auth_url":"url","login_id":"id"}`,
		`{"sessions":[],"auth_url":3,"login_id":null}`,
		`{"sessions":false,"auth_url":"url","login_id":"id"}`,
	}, []string{
		`{}`, `{"auth_url":"url"}`, `{"login_id":"id"}`,
		`{"auth_url":"url","login_id":3}`, `{"sessions":false,"auth_url":3,"login_id":"id"}`,
	})
}
