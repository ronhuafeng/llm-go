package protocolv2

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// Examples are grounded in the unmodified rust-v0.169.0 source schemas at
// d32063f96c2dff1dc96af839b3952abb6acfeda4, with the public closed-record overlay.
func TestFunctionOutputAudio0169SourceContract(t *testing.T) {
	source := compile0169Definition(t, "TurnStartParams.json", "FunctionCallOutputContentItem")
	for _, tc := range []struct {
		wire           string
		source, public bool
	}{
		{`{"type":"input_audio","audio_url":"ok"}`, true, true},
		{`{"type":"input_audio","file_id":"f"}`, true, true},
		{`{"type":"input_audio","audio_url":"ok","file_id":"f"}`, true, true},
		{`{"type":"input_audio","audio_url":"ok","file_id":3}`, true, true},
		{`{"type":"input_audio","audio_url":null,"file_id":"f"}`, true, true},
		{`{"type":"input_audio","audio_url":{"n":9007199254740993},"file_id":"f"}`, true, true},
		{`{"type":"input_audio","audio_url":"ok","undeclared":true}`, true, false},
		{`{"type":"input_audio"}`, false, false},
		{`{"type":"input_audio","audio_url":3,"file_id":null}`, false, false},
		{`{"type":"input_audio","file_id":3}`, false, false},
		{`{"type":"other","audio_url":"ok"}`, false, false},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			want := decode0167JSON(t, []byte(tc.wire))
			if valid := source.Validate(want) == nil; valid != tc.source {
				t.Fatalf("source validity=%v want=%v", valid, tc.source)
			}
			var value FunctionCallOutputContentItem
			if err := json.Unmarshal([]byte(tc.wire), &value); (err == nil) != tc.public {
				t.Fatalf("public validity=%v want=%v: %v", err == nil, tc.public, err)
			}
			if tc.public {
				out, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(decode0167JSON(t, out), want) {
					t.Fatalf("wire changed: %s became %s", tc.wire, out)
				}
			}
		})
	}
}

func TestProviderPatch0169SourceContract(t *testing.T) {
	source := compile0167Source(t, "ModelProviderConfigWriteParams.json")
	for _, tc := range []struct {
		wire  string
		valid bool
	}{
		{`{"settings":{"type":"customGateway"}}`, true},
		{`{"settings":{"type":"customGateway","providerId":null,"baseUrl":null,"envKey":"KEY","name":"Gateway"}}`, true},
		{`{"settings":{"type":"bedrock","providerId":"amazon-bedrock","profile":null,"region":"region"}}`, true},
		{`{"settings":{"type":"gcp","providerId":"gcp","credentialFile":null,"projectId":"project"}}`, true},
		{`{"settings":{"type":"bedrock"}}`, false},
		{`{"settings":{"type":"bedrock","providerId":"gcp"}}`, false},
		{`{"settings":{"type":"gcp","providerId":"gcp","profile":"not-a-gcp-field"}}`, false},
		{`{"settings":{"type":"customGateway","name":null}}`, false},
		{`{"settings":{"type":"customGateway"},"undeclared":true}`, false},
	} {
		if valid := source.Validate(decode0167JSON(t, []byte(tc.wire))) == nil; valid != tc.valid {
			t.Fatalf("source validity=%v want=%v: %s", valid, tc.valid, tc.wire)
		}
		var value ModelProviderConfigWriteParams
		if err := json.Unmarshal([]byte(tc.wire), &value); (err == nil) != tc.valid {
			t.Fatalf("public validity=%v want=%v: %s: %v", err == nil, tc.valid, tc.wire, err)
		}
		if tc.valid {
			out, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decode0167JSON(t, out), decode0167JSON(t, []byte(tc.wire))) {
				t.Fatalf("wire changed: %s became %s", tc.wire, out)
			}
		}
	}
}

func TestOAuth0169SourceContract(t *testing.T) {
	source := compile0169Definition(t, "../ClientRequest.json", "")
	for _, tc := range []struct {
		params         string
		source, public bool
	}{
		{``, true, true},
		{`,"params":null`, true, true},
		{`,"params":{}`, true, true},
		{`,"params":{"modelProvider":"gateway","deviceAuth":true}`, true, true},
		{`,"params":{"modelProvider":null,"deviceAuth":false}`, true, true},
		{`,"params":{"deviceAuth":true,"undeclared":1}`, true, false},
		{`,"params":{"deviceAuth":"yes"}`, false, false},
		{`,"params":{"modelProvider":3}`, false, false},
		{`,"params":[]`, false, false},
	} {
		wire := []byte(`{"id":1,"method":"account/gatewayOAuth/login"` + tc.params + `}`)
		want := decode0167JSON(t, wire)
		if valid := source.Validate(want) == nil; valid != tc.source {
			t.Fatalf("source validity=%v want=%v: %s", valid, tc.source, wire)
		}
		var value ClientRequest
		if err := json.Unmarshal(wire, &value); (err == nil) != tc.public {
			t.Fatalf("public validity=%v want=%v: %s: %v", err == nil, tc.public, wire, err)
		}
		if tc.public {
			out, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, decode0167JSON(t, out)) {
				t.Fatalf("wire changed: %s became %s", wire, out)
			}
		}
	}
}

func compile0169Definition(t *testing.T, file, definition string) *validator.Schema {
	t.Helper()
	raw, err := os.ReadFile("../internal/protocolschema/appserver/v2/v2/" + file)
	if err != nil {
		t.Fatal(err)
	}
	doc := decode0167JSON(t, raw).(map[string]any)
	root := doc
	if definition != "" {
		root = map[string]any{"$schema": doc["$schema"], "$ref": "#/definitions/" + definition, "definitions": doc["definitions"]}
	}
	c := validator.NewCompiler()
	const resource = "https://upstream-contract.invalid/definition.json"
	if err := c.AddResource(resource, root); err != nil {
		t.Fatal(err)
	}
	source, err := c.Compile(resource)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
