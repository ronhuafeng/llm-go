package protocolv2

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// Expectations come from the unmodified rust-v0.167.0 schemas, commit
// 086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a, rather than generated Go fields.
func TestAccountSessionsSwitchSourceContract(t *testing.T) {
	source := compile0167Source(t, "AccountSessionsSwitchResponse.json")
	for _, tc := range []struct {
		wire           string
		source, public bool
	}{
		{`{"sessions":[]}`, true, true},
		{`{"sessions":[],"activeSessionId":null}`, true, true},
		{`{"auth_url":"url","login_id":"id"}`, true, true},
		{`{"sessions":[],"auth_url":"url","login_id":"id"}`, true, true},
		{`{"sessions":[],"auth_url":3,"login_id":null}`, true, true},
		{`{"sessions":false,"activeSessionId":3,"auth_url":"url","login_id":"id"}`, true, true},
		{`{"sessions":[],"future":{"nested":[null,9007199254740993]}}`, true, true},
		{`{"sessions":[{"sessionId":"s","isActive":true,"lastUsedAt":9007199254740993,"workspaces":[]}]}`, true, true},
		{`{"sessions":[{"sessionId":"s","isActive":true,"lastUsedAt":1,"workspaces":[],"undeclared":true}]}`, true, false},
		{`{}`, false, false},
		{`{"auth_url":"url"}`, false, false},
		{`{"login_id":"id"}`, false, false},
		{`{"auth_url":"url","login_id":3}`, false, false},
		{`{"sessions":false,"auth_url":3,"login_id":"id"}`, false, false},
		{`{"sessions":[],"activeSessionId":3}`, false, false},
		{`{"sessions":[{"sessionId":"s"}]}`, false, false},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			want := decode0167JSON(t, []byte(tc.wire))
			if valid := source.Validate(want) == nil; valid != tc.source {
				t.Fatalf("source validity=%v want=%v", valid, tc.source)
			}
			var value AccountSessionsSwitchResponse
			if err := json.Unmarshal([]byte(tc.wire), &value); (err == nil) != tc.public {
				t.Fatalf("public validity=%v want=%v: %v", err == nil, tc.public, err)
			}
			if !tc.public {
				return
			}
			out, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode0167JSON(t, out); !reflect.DeepEqual(got, want) {
				t.Fatalf("wire changed: %s became %s", tc.wire, out)
			}
		})
	}
}

func TestReviewDelivery0167SourceContract(t *testing.T) {
	source := compile0167Source(t, "ReviewStartParams.json")
	for _, tc := range []struct {
		delivery string
		valid    bool
	}{
		{``, true}, {`,"delivery":null`, true}, {`,"delivery":"inline"`, true},
		{`,"delivery":"detached"`, false}, {`,"delivery":"unknown"`, false},
	} {
		wire := []byte(`{"threadId":"t","target":{"type":"commit","sha":"abc"}` + tc.delivery + `}`)
		if valid := source.Validate(decode0167JSON(t, wire)) == nil; valid != tc.valid {
			t.Fatalf("source validity=%v want=%v: %s", valid, tc.valid, wire)
		}
		var params ReviewStartParams
		if err := json.Unmarshal(wire, &params); (err == nil) != tc.valid {
			t.Fatalf("public validity=%v want=%v: %s: %v", err == nil, tc.valid, wire, err)
		}
	}
	params := ReviewStartParams{ThreadID: "t", Target: NewReviewTargetUncommittedChanges(), Delivery: Value(ReviewDelivery("detached"))}
	if _, err := json.Marshal(params); err == nil {
		t.Fatal("encoding accepted the removed detached delivery")
	}
}

func compile0167Source(t *testing.T, name string) *validator.Schema {
	t.Helper()
	raw, err := os.ReadFile("../internal/protocolschema/appserver/v2/v2/" + name)
	if err != nil {
		t.Fatal(err)
	}
	c := validator.NewCompiler()
	const resource = "https://upstream-contract.invalid/source.json"
	if err := c.AddResource(resource, decode0167JSON(t, raw)); err != nil {
		t.Fatal(err)
	}
	source, err := c.Compile(resource)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func decode0167JSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
