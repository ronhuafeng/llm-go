package protocolv2

import (
	"encoding/json"
	"testing"
)

func TestNullableMarshalNullAndValue(t *testing.T) {
	nullData, err := json.Marshal(Null[string]())
	if err != nil {
		t.Fatal(err)
	}
	if string(nullData) != "null" {
		t.Fatalf("Null marshaled to %s, want null", nullData)
	}

	valueData, err := json.Marshal(Value("auto"))
	if err != nil {
		t.Fatal(err)
	}
	if string(valueData) != `"auto"` {
		t.Fatalf("Value marshaled to %s, want string", valueData)
	}
}

func TestNullablePointerFieldOmitIsSurroundingStructStrategy(t *testing.T) {
	type request struct {
		ServiceTier *Nullable[string] `json:"serviceTier,omitempty"`
	}

	omitted, err := json.Marshal(request{})
	if err != nil {
		t.Fatal(err)
	}
	if string(omitted) != "{}" {
		t.Fatalf("nil *Nullable field marshaled to %s, want omission by surrounding struct", omitted)
	}

	null, err := json.Marshal(request{ServiceTier: Null[string]()})
	if err != nil {
		t.Fatal(err)
	}
	if string(null) != `{"serviceTier":null}` {
		t.Fatalf("null field marshaled to %s", null)
	}

	value, err := json.Marshal(request{ServiceTier: Value("flex")})
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != `{"serviceTier":"flex"}` {
		t.Fatalf("value field marshaled to %s", value)
	}
}

func TestNullableFromJSONPreservesPresentNullAndValue(t *testing.T) {
	null, err := NullableFromJSON[string]([]byte(" null "))
	if err != nil {
		t.Fatal(err)
	}
	if null == nil || null.Value != nil {
		t.Fatalf("NullableFromJSON(null) = %#v, want present null", null)
	}

	value, err := NullableFromJSON[string]([]byte(`"flex"`))
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || value.Value == nil || *value.Value != "flex" {
		t.Fatalf("NullableFromJSON(value) = %#v, want flex", value)
	}
}

func TestNullableUnmarshalSupportsTypedMapValues(t *testing.T) {
	var env map[string]*Nullable[string]
	if err := json.Unmarshal([]byte(`{"PATH":"/bin","REMOVE":null}`), &env); err != nil {
		t.Fatal(err)
	}
	if env["PATH"] == nil || env["PATH"].Value == nil || *env["PATH"].Value != "/bin" {
		t.Fatalf("decoded PATH = %#v", env["PATH"])
	}
	if _, ok := env["REMOVE"]; !ok || env["REMOVE"] != nil {
		t.Fatalf("decoded REMOVE = %#v, present=%t; want present null as nil map value", env["REMOVE"], ok)
	}

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"PATH":"/bin","REMOVE":null}` {
		t.Fatalf("nullable map values JSON = %s", raw)
	}
}

func TestStandardPointerUnmarshalCannotPreserveNullablePresence(t *testing.T) {
	type request struct {
		ServiceTier *Nullable[string] `json:"serviceTier,omitempty"`
	}

	var omitted request
	if err := json.Unmarshal([]byte(`{}`), &omitted); err != nil {
		t.Fatal(err)
	}
	var explicitNull request
	if err := json.Unmarshal([]byte(`{"serviceTier":null}`), &explicitNull); err != nil {
		t.Fatal(err)
	}
	if omitted.ServiceTier != nil || explicitNull.ServiceTier != nil {
		t.Fatalf("ordinary json.Unmarshal changed expected pointer behavior: omitted=%#v null=%#v", omitted.ServiceTier, explicitNull.ServiceTier)
	}
}
