package protocolgen

import (
	"strings"
	"testing"
)

// This fixture uses the exact structural change in rust-v0.162.1's
// CodexErrorInfo, without making its type name an admission rule.
func TestGenerateOpenMixedUnionFromShape(t *testing.T) {
	const branches = `{"type":"string","enum":["other"]},
		{"type":"object","additionalProperties":false,"required":["failed"],
		 "properties":{"failed":{"type":"object","required":["code"],"properties":{"code":{"type":"string"}}}}}`
	for _, tc := range []struct {
		name, keyword, catchall string
		wantOpen, wantError     bool
	}{
		{"closed", "oneOf", "", false, false},
		{"open", "anyOf", `,{"type":["string","object"]}`, true, false},
		{"constrained catchall", "anyOf", `,{"type":["string","object"],"minLength":1}`, false, true},
		{"unknown constraint", "anyOf", `,{"type":["string","object"],"futureConstraint":true}`, false, true},
		{"overlapping oneOf", "oneOf", `,{"type":["string","object"]}`, false, true},
		{"extra kind", "anyOf", `,{"type":["string","object","boolean"]}`, false, true},
		{"false branch", "anyOf", `,false`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := mustParseSchema(t, `{"type":"object","definitions":{"FutureError":{"`+tc.keyword+`":[`+branches+tc.catchall+`]}}}`)
			plan := ProtocolTypePlan{Types: []TypePlan{{
				Kind: TypePlanEmptyStructCandidate, SchemaPath: "Fixture.json", TypeName: "Fixture",
				Schema: schema, GeneratedDefinitions: map[string]bool{"FutureError": true},
			}}}
			generated, err := GenerateProtocolTypes(plan)
			if tc.wantError {
				if err == nil {
					t.Fatal("unsupported or constrained branch silently admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			text := string(generated)
			for _, want := range []string{"func NewFutureErrorOther()", "func NewFutureErrorFailed(payload FutureErrorFailed)", "func (value FutureError) AsFailed()", "missing required field"} {
				if !strings.Contains(text, want) {
					t.Fatalf("typed variant API or validation missing: %s", want)
				}
			}
			if strings.Contains(text, "func NewFutureErrorUnknown(payload JSONValue)") != tc.wantOpen {
				t.Fatalf("open API does not match explicit catchall: %s", text)
			}
		})
	}
}
