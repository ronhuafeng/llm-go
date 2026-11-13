package protocolupgrade

import (
	"encoding/json"
	"testing"
)

func TestAggregateParamsShapes(t *testing.T) {
	for _, tc := range []struct {
		wire, ref string
		nullable  bool
	}{
		{`{"$ref":"#/definitions/Params"}`, "#/definitions/Params", false},
		{`{"type":"null"}`, "", true},
		{`{"anyOf":[{"$ref":"#/definitions/Params"},{"type":"null"}]}`, "#/definitions/Params", true},
		{`{"anyOf":[{"type":"null"},{"$ref":"#/definitions/Params"}]}`, "#/definitions/Params", true},
	} {
		ref, nullable, err := aggregateParamsShape(json.RawMessage(tc.wire))
		if err != nil || ref != tc.ref || nullable != tc.nullable {
			t.Fatalf("%s: ref=%q nullable=%v err=%v", tc.wire, ref, nullable, err)
		}
	}
	for _, wire := range []string{
		`{}`, `true`, `{"type":"string"}`, `{"$ref":"external.json"}`,
		`{"anyOf":[{"$ref":"#/definitions/Params"},{"type":"integer"}]}`,
		`{"anyOf":[{"$ref":"#/definitions/Params"},{"type":"null"}],"not":{}}`,
		`{"anyOf":[{"$ref":"#/definitions/Params","additionalProperties":false},{"type":"null"}]}`,
		`{"anyOf":[{"type":"null"},{"type":"null"}]}`,
		`{"anyOf":[{"anyOf":[{"$ref":"#/definitions/Params"},{"type":"null"}]},{"type":"null"}]}`,
	} {
		if _, _, err := aggregateParamsShape(json.RawMessage(wire)); err == nil {
			t.Fatalf("unsupported parameter shape silently accepted: %s", wire)
		}
	}
}
