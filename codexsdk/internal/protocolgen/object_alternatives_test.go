package protocolgen

import (
	"reflect"
	"testing"
)

func TestTaggedObjectAlternativesKeepConjunctiveChoice(t *testing.T) {
	const prefix = `{"type":"object","properties":{"type":{"type":"string","enum":["image"]}},"required":["type"],"anyOf":[`
	const first = `{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`
	const second = `{"type":"object","properties":{"fileId":{"type":"string"}},"required":["fileId"]}`
	schema := mustParseSchema(t, prefix+first+","+second+`]}`)
	flat, groups, err := taggedObjectAlternatives(schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(flat.Properties) != 3 || !reflect.DeepEqual(flat.Required, []string{"type"}) || !reflect.DeepEqual(groups, [][]string{{"url"}, {"fileId"}}) || len(schema.Properties) != 1 {
		t.Fatal("choice fields, shared requiredness or source identity changed")
	}
	for _, branch := range []string{
		`false`,
		`{"type":"object","properties":{"url":{"type":"string"}}}`,
		`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"enum":[]}`,
		`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"oneOf":[]}`,
		`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"futureConstraint":true}`,
		`{"type":"object","properties":{"type":{"type":"string"}},"required":["type"]}`,
		first,
	} {
		// A duplicate first branch is also rejected: flattening overlapping
		// alternatives could silently change branch-specific field constraints.
		if _, _, err := taggedObjectAlternatives(mustParseSchema(t, prefix+first+","+branch+`]}`)); err == nil {
			t.Fatalf("unsupported choice silently flattened: %s", branch)
		}
	}
}
