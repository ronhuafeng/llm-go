package protocolv2

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// The unchanged upstream anyOf is open. The accepted public MCP overlay keeps
// text/blob exclusive and each record closed, as the frozen baseline test owns.
func TestResourceAnyOfUnmodifiedSourceAndRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocolschema/appserver/v2/v2/McpResourceReadResponse.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	c := validator.NewCompiler()
	const resource = "https://resource-contract.invalid/read.json"
	if err := c.AddResource(resource, doc); err != nil {
		t.Fatal(err)
	}
	source, err := c.Compile(resource + "#/definitions/ResourceContent")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		wire   string
		valid  bool
		public bool
	}{
		{`{"text":"hello","uri":"file://readme"}`, true, true},
		{`{"blob":"Ymlu","uri":"file://readme"}`, true, true},
		{`{"blob":"Ymlu","text":"hello","uri":"file://readme"}`, true, false},
		{`{"blob":3,"text":"hello","uri":"file://readme"}`, true, false},
		{`{"blob":"Ymlu","text":3,"uri":"file://readme"}`, true, false},
		{`{"text":"hello","uri":"file://readme","future":{"data":[null,true,3]}}`, true, false},
		{`{"text":"hello"}`, false, false},
		{`{"text":3,"blob":false,"uri":"file://readme"}`, false, false},
		{`{"text":"hello","blob":"Ymlu","uri":3}`, false, false},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			var want any
			if err := json.Unmarshal([]byte(tc.wire), &want); err != nil {
				t.Fatal(err)
			}
			if valid := source.Validate(want) == nil; valid != tc.valid {
				t.Fatalf("unmodified source validity=%v want=%v", valid, tc.valid)
			}
			var content ResourceContent
			if err := json.Unmarshal([]byte(tc.wire), &content); (err == nil) != tc.public {
				t.Fatalf("public validity=%v want=%v: %v", err == nil, tc.public, err)
			}
			if !tc.public {
				return
			}
			out, err := json.Marshal(content)
			if err != nil {
				t.Fatal(err)
			}
			var got any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resource changed: %s became %s", tc.wire, out)
			}
		})
	}
}
