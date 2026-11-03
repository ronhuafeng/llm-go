package protocolgen

import (
	"os"
	"path/filepath"
	"testing"
)

// These are reduced structural examples from OpenAI Codex 0.166.0, commit
// 6ff1e1ca3b30fc8b914720681fcf5fe62e95f856. They test generator capabilities,
// not a claim that the checked-in protocol baseline has been upgraded.
func TestGeneratedFlattenedThreadItemContracts(t *testing.T) {
	const schema = `{"type":"object","properties":{"startedAtMs":{"type":["integer","null"],"format":"int64"},"completedAtMs":{"type":["integer","null"],"format":"int64"}},"oneOf":[{"type":"object","properties":{"type":{"type":"string","enum":["imageGeneration"]},"id":{"type":"string"},"status":{"type":"string"},"generationId":{"type":["string","null"]}},"required":["type","id","status"],"anyOf":[{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]},{"type":"object","properties":{"result":{"type":"string"}},"required":["result"]}]},{"type":"object","properties":{"type":{"type":"string","enum":["agentMessage"]},"id":{"type":"string"},"text":{"type":"string"}},"required":["type","id","text"]}]}`
	plan := ProtocolTypePlan{Types: []TypePlan{{Kind: TypePlanTaggedUnionCandidate, GeneratedRoot: true, TypeName: "ThreadItem", SchemaPath: "ThreadItem.json", Schema: mustParseSchema(t, schema)}}}
	runGeneratedContracts(t, "ThreadItem", plan, []string{
		`{"type":"imageGeneration","id":"i","status":"completed","fileId":"f"}`,
		`{"type":"imageGeneration","id":"i","status":"completed","result":"data","startedAtMs":10,"completedAtMs":20}`,
		`{"type":"imageGeneration","id":"i","status":"completed","fileId":"f","result":"","startedAtMs":null,"generationId":null}`,
		`{"type":"agentMessage","id":"i","text":"done","completedAtMs":20}`,
		`{"type":"agentMessage","id":"i","text":"done","startedAtMs":9007199254740993,"completedAtMs":9223372036854775807}`,
	}, []string{
		`{"type":"imageGeneration","id":"i","status":"completed"}`,
		`{"type":"imageGeneration","id":"i","status":"completed","fileId":4}`,
		`{"type":"imageGeneration","id":"i","status":"completed","fileId":"f","startedAtMs":"10"}`,
		`{"type":"agentMessage","id":"i","text":"done","unknown":true}`,
		`{"type":"agentMessage","id":"i","text":"done","startedAtMs":9223372036854775808}`,
	})
}

func TestGeneratedNestedStringMapContracts(t *testing.T) {
	const schema = `{"title":"Requirements","type":"object","properties":{"tracestate":{"type":["object","null"],"additionalProperties":{"type":"object","additionalProperties":{"type":"string"}}}}}`
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Requirements.json"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	matrix := CoverageMatrix{Types: []CoverageType{{Schema: "Requirements.json", Type: "Requirements", Stability: "stable"}}, Fields: []CoverageField{{Field: "tracestate", Schema: "Requirements.json", Path: "Requirements.json#/properties/tracestate"}}}
	manifest := Manifest{Entries: []ManifestEntry{{Kind: "request", Direction: "client_to_server", Method: "requirements/read", SourceSchema: "Requirements.json"}}}
	plan, err := BuildProtocolTypePlanFromFacts(root, matrix, manifest)
	if err != nil {
		t.Fatal(err)
	}
	field := plan.Types[0].Fields[0]
	if field.GoType != "*protocolv2.Nullable[map[string]map[string]string]" {
		t.Fatalf("nested map loses typed values: %s", field.GoType)
	}
	runGeneratedContracts(t, "Requirements", plan, []string{
		`{}`, `{"tracestate":null}`, `{"tracestate":{}}`,
		`{"tracestate":{"vendor":{"trace":"abc","span":"def"},"empty":{}}}`,
	}, []string{`{"tracestate":[]}`, `{"tracestate":{"vendor":null}}`, `{"tracestate":{"vendor":{"trace":1}}}`})
}

func TestNestedMapRejectsUnrepresentedObjectConstraints(t *testing.T) {
	for _, extra := range []string{`,"minProperties":1`, `,"properties":{"fixed":{"type":"string"}}`, `,"required":["fixed"]`} {
		schema := mustParseSchema(t, `{"type":"object","additionalProperties":{"type":"string"}`+extra+`}`)
		if _, _, err := mapValueType("Example#/properties/map", "Example.json", schema); err == nil {
			t.Fatalf("lost map constraint: %s", extra)
		}
	}
}
