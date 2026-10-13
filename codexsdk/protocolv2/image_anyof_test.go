package protocolv2

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestImageAnyOfSourceContract(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocolschema/appserver/v2/v2/TurnStartParams.json")
	if err != nil {
		t.Fatal(err)
	}
	decode := func(raw []byte) any {
		t.Helper()
		var value any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	c := validator.NewCompiler()
	const resource = "https://image-contract.invalid/turn.json"
	if err := c.AddResource(resource, decode(raw)); err != nil {
		t.Fatal(err)
	}
	source, err := c.Compile(resource + "#/definitions/UserInput")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		wire           string
		source, public bool
	}{
		{`{"type":"image","url":"ok"}`, true, true},
		{`{"type":"image","fileId":"file-1"}`, true, true},
		{`{"type":"image","url":"ok","fileId":"file-1"}`, true, true},
		{`{"type":"image","url":"ok","fileId":3}`, true, true},
		{`{"type":"image","url":3,"fileId":"file-1"}`, true, true},
		{`{"type":"image","url":"","fileId":null,"detail":null}`, true, true},
		{`{"type":"image","url":{"nested":[null,123456789012345678901234567890]},"fileId":"file-1"}`, true, true},
		{`{"type":"image","url":"ok","extra":true}`, true, false},
		{`{"type":"image"}`, false, false},
		{`{"type":"image","url":3,"fileId":null}`, false, false},
		{`{"type":"image","url":null}`, false, false},
		{`{"type":"image","url":"ok","detail":"invalid"}`, false, false},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			want := decode([]byte(tc.wire))
			if valid := source.Validate(want) == nil; valid != tc.source {
				t.Fatalf("unmodified source validity=%v want=%v", valid, tc.source)
			}
			var input UserInput
			err := json.Unmarshal([]byte(tc.wire), &input)
			if (err == nil) != tc.public {
				t.Fatalf("public validity=%v want=%v: %v", err == nil, tc.public, err)
			}
			if !tc.public {
				return
			}
			out, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(out); !reflect.DeepEqual(got, want) {
				t.Fatalf("wire changed: %s became %s", tc.wire, out)
			}
		})
	}
}

func TestImageAnyOfConstructorsAndObservationPolicy(t *testing.T) {
	url, file := JSONString("ok"), JSONNull()
	input := NewUserInputImage(UserInputImage{URL: &url, FileID: &file})
	if _, err := json.Marshal(input); err != nil {
		t.Fatalf("valid URL branch rejected during encoding: %v", err)
	}
	for _, payload := range []UserInputImage{{}, {URL: &file, FileID: &file}} {
		if _, err := json.Marshal(NewUserInputImage(payload)); err == nil {
			t.Fatal("encoding accepted a value matching no branch")
		}
	}
	if err := input.unmarshalJSON([]byte(`{"type":"image","url":"ok","fileId":null,"future":true}`), wireDecodeServerObservation); err != nil {
		t.Fatalf("observation rejected valid branch or unknown field: %v", err)
	}
	out, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["future"]; exists || string(fields["fileId"]) != "null" {
		t.Fatalf("observation policy or known-field presence changed: %s", out)
	}
	if err := input.unmarshalJSON([]byte(`{"type":"image","url":null,"fileId":3,"future":true}`), wireDecodeServerObservation); err == nil {
		t.Fatal("observation ignored invalid known branch fields")
	}
}
