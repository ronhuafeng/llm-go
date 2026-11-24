package protocolv2

import (
	"encoding/json"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// Examples follow the unmodified rust-v0.172.0 schemas at
// 3d86e43b075adeba2193ec5c22ac92f55afd82e6 and the public closed-record overlay.
// Expected validity is explicit, independent of the generated implementation.
func check0172Contract[T any](t *testing.T, source *validator.Schema, valid, invalid []string, open string) {
	t.Helper()
	for _, group := range []struct {
		wires []string
		valid bool
	}{{valid, true}, {invalid, false}} {
		for _, wire := range group.wires {
			t.Run(wire, func(t *testing.T) {
				want := decode0167JSON(t, []byte(wire))
				if got := source.Validate(want) == nil; got != group.valid {
					t.Fatalf("source validity=%v want=%v", got, group.valid)
				}
				var value T
				if err := json.Unmarshal([]byte(wire), &value); (err == nil) != group.valid {
					t.Fatalf("public validity=%v want=%v: %v", err == nil, group.valid, err)
				}
				if group.valid {
					out, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(decode0167JSON(t, out), want) {
						t.Fatalf("wire changed: %s became %s", wire, out)
					}
				}
			})
		}
	}
	if open != "" {
		if err := source.Validate(decode0167JSON(t, []byte(open))); err != nil {
			t.Fatalf("source should allow undeclared field: %v", err)
		}
		var value T
		if err := json.Unmarshal([]byte(open), &value); err == nil {
			t.Fatal("public closed-record overlay accepted undeclared field")
		}
	}
}

func TestBedrockSetup0172SourceContract(t *testing.T) {
	check0172Contract[BedrockSetupParams](t, compile0167Source(t, "BedrockSetupParams.json"), []string{
		`{"type":"profile","profile":"p","region":"r"}`,
		`{"type":"profile","profile":"p","region":"r","providerId":null}`,
		`{"type":"profile","profile":"p","region":"r","providerId":"amazon-bedrock-runtime"}`,
		`{"type":"environment","region":"r","providerId":"amazon-bedrock"}`,
		`{"type":"environment","region":"r","providerId":"amazon-bedrock-runtime"}`,
	}, []string{
		`{"type":"profile","region":"r","providerId":"amazon-bedrock-runtime"}`,
		`{"type":"environment","region":"r","providerId":"other"}`,
		`{"type":"environment","region":"r","providerId":3}`,
	}, `{"type":"environment","region":"r","future":true}`)
}

func TestDaybreak0172SourceContract(t *testing.T) {
	check0172Contract[ThreadStartParams](t, compile0167Source(t, "ThreadStartParams.json"), []string{
		`{}`, `{"daybreakProgramPropagationEnrolled":null}`,
		`{"daybreakProgramPropagationEnrolled":false}`, `{"daybreakProgramPropagationEnrolled":true}`,
	}, []string{`{"daybreakProgramPropagationEnrolled":"false"}`}, `{"daybreakProgramPropagationEnrolled":false,"future":true}`)
	check0172Contract[ModelAccessPrograms](t, compile0169Definition(t, "ModelListResponse.json", "ModelAccessPrograms"), []string{
		`{"cyber":[]}`, `{"cyber":[],"daybreakAvailability":"default"}`,
		`{"cyber":[],"daybreakAvailability":"available"}`,
		`{"cyber":[],"daybreakAvailability":"enrollmentRequired"}`,
		`{"cyber":[],"daybreakAvailability":"unavailableForModel"}`,
	}, []string{
		`{"cyber":[],"daybreakAvailability":null}`, `{"cyber":[],"daybreakAvailability":"other"}`,
		`{"daybreakAvailability":"default"}`,
	}, `{"cyber":[],"future":true}`)
}

func TestRealtimeSessionInput0172SourceContract(t *testing.T) {
	check0172Contract[ThreadRealtimeItem](t, compile0169Definition(t, "ThreadRealtimeListResponse.json", "ThreadRealtimeItem"), []string{
		`{"id":"i","realtimeSessionId":"s","type":"sessionInputSaved","input":"text","input_item_id":"i","reason":"chunk","source_event_ids":[]}`,
		`{"id":"i","realtimeSessionId":"s","type":"sessionInputSaved","input":"text","input_item_id":"i","reason":"sessionEnd","source_event_ids":["e"]}`,
	}, []string{
		`{"id":"i","realtimeSessionId":"s","type":"sessionInputSaved","input":"text","input_item_id":"i","source_event_ids":[]}`,
		`{"id":"i","realtimeSessionId":"s","type":"sessionInputSaved","input":"text","input_item_id":"i","reason":"other","source_event_ids":[]}`,
		`{"id":"i","realtimeSessionId":"s","type":"sessionEndInputSaved","input":"text","input_item_id":"i","source_event_ids":[]}`,
	}, `{"id":"i","realtimeSessionId":"s","type":"sessionInputSaved","input":"text","input_item_id":"i","reason":"chunk","source_event_ids":[],"future":true}`)
	if _, err := json.Marshal(NewThreadRealtimeItemSessionInputSaved(ThreadRealtimeItemSessionInputSaved{
		Input: "text", InputItemID: "i", SourceEventIDs: []string{},
	})); err == nil {
		t.Fatal("encoding accepted missing required reason")
	}
}

func TestSpendLimit0172SourceContract(t *testing.T) {
	check0172Contract[CodexErrorInfo](t, compile0169Definition(t, "ErrorNotification.json", "CodexErrorInfo"), []string{
		`"apiKeySpendLimitExceeded"`, `"projectSpendLimitExceeded"`, `"organizationSpendLimitExceeded"`,
	}, []string{`null`, `3`, `[]`}, "")
}

func TestSampleModel0172SourceContract(t *testing.T) {
	check0172Contract[ThreadSampleParams](t, compile0167Source(t, "ThreadSampleParams.json"), []string{
		`{"threadId":"t","expectedTurnId":"turn","prompt":"p"}`,
		`{"threadId":"t","expectedTurnId":"turn","prompt":"p","model":null}`,
		`{"threadId":"t","expectedTurnId":"turn","prompt":"p","model":"model"}`,
	}, []string{
		`{"threadId":"t","expectedTurnId":"turn","prompt":"p","model":3}`,
		`{"threadId":"t","prompt":"p","model":"model"}`,
	}, `{"threadId":"t","expectedTurnId":"turn","prompt":"p","model":"model","future":true}`)
}

func TestAppsDefaultReviewer0172SourceContract(t *testing.T) {
	check0172Contract[AppsDefaultConfig](t, compile0169Definition(t, "ConfigReadResponse.json", "AppsDefaultConfig"), []string{
		`{}`, `{"approvals_reviewer":null}`, `{"approvals_reviewer":"user"}`, `{"approvals_reviewer":"guardian_subagent"}`,
	}, []string{`{"approvals_reviewer":"other"}`, `{"approvals_reviewer":false}`}, `{"approvals_reviewer":"user","future":true}`)
}
