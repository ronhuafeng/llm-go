package protocolv2

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// rust-v0.164.0 permits either result or fileId, with the other branch's
// field unconstrained. Validate the unchanged source before the public overlay.
func TestImageGenerationAnyOfSourceContract(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocolschema/appserver/v2/v2/ThreadStartResponse.json")
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
	const resource = "https://image-generation-contract.invalid/thread.json"
	if err := c.AddResource(resource, decode(raw)); err != nil {
		t.Fatal(err)
	}
	source, err := c.Compile(resource + "#/definitions/ThreadItem")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		members        string
		source, public bool
	}{
		{`,"result":"ok"`, true, true},
		{`,"fileId":"file-1"`, true, true},
		{`,"result":"ok","fileId":"file-1"`, true, true},
		{`,"result":"ok","fileId":3`, true, true},
		{`,"result":null,"fileId":"file-1"`, true, true},
		{`,"result":{"nested":[null,123456789012345678901234567890]},"fileId":"file-1"`, true, true},
		{`,"result":"ok","inputImages":[]`, true, true},
		{`,"fileId":"file-1","inputImages":[{"path":"/tmp/input.png","sha256":"abc"}]`, true, true},
		{`,"result":"ok","extra":true`, true, false},
		{``, false, false},
		{`,"result":3,"fileId":null`, false, false},
		{`,"result":null`, false, false},
		{`,"result":"ok","inputImages":null`, false, false},
		{`,"result":"ok","inputImages":[{"path":"/tmp/input.png"}]`, false, false},
		{`,"result":"ok","inputImages":[{"path":"/tmp/input.png","sha256":3}]`, false, false},
		{`,"result":"ok","inputImages":[{"path":"/tmp/input.png","sha256":"abc","extra":true}]`, true, false},
	} {
		t.Run(tc.members, func(t *testing.T) {
			wire := `{"type":"imageGeneration","id":"image-1","status":"completed"` + tc.members + `}`
			want := decode([]byte(wire))
			if valid := source.Validate(want) == nil; valid != tc.source {
				t.Fatalf("unmodified source validity=%v want=%v", valid, tc.source)
			}
			var item ThreadItem
			err := json.Unmarshal([]byte(wire), &item)
			if (err == nil) != tc.public {
				t.Fatalf("public validity=%v want=%v: %v", err == nil, tc.public, err)
			}
			if !tc.public {
				return
			}
			out, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(out); !reflect.DeepEqual(got, want) {
				t.Fatalf("wire changed: %s became %s", wire, out)
			}
		})
	}
}

func TestImageGenerationAnyOfConstructionAndObservation(t *testing.T) {
	result, file := JSONString("ok"), JSONNull()
	item := NewThreadItemImageGeneration(ThreadItemImageGeneration{
		ID: "image-1", Status: "completed", Result: &result, FileID: &file,
	})
	if _, err := json.Marshal(item); err != nil {
		t.Fatalf("valid result branch rejected during encoding: %v", err)
	}
	for _, payload := range []ThreadItemImageGeneration{
		{ID: "image-1", Status: "completed"},
		{ID: "image-1", Status: "completed", Result: &file, FileID: &file},
	} {
		if _, err := json.Marshal(NewThreadItemImageGeneration(payload)); err == nil {
			t.Fatal("encoding accepted a value matching no branch")
		}
	}
	wire := []byte(`{"type":"imageGeneration","id":"image-1","status":"completed","result":"ok","fileId":null,"inputImages":[{"path":"/tmp/input.png","sha256":"abc","future":true}],"future":true}`)
	if err := item.unmarshalJSON(wire, wireDecodeServerObservation); err != nil {
		t.Fatalf("observation rejected valid branch or unknown fields: %v", err)
	}
	if err := unmarshalActionBearingMessage(wire, &item); err == nil {
		t.Fatal("action-bearing decode accepted unknown fields")
	}
	if err := item.unmarshalJSON([]byte(`{"type":"imageGeneration","id":"image-1","status":"completed","result":null,"fileId":3,"future":true}`), wireDecodeServerObservation); err == nil {
		t.Fatal("observation ignored invalid known branch fields")
	}
}
