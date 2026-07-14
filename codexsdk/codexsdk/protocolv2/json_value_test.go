package protocolv2

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestJSONValueZeroValueIsInvalidAndNotNull(t *testing.T) {
	var value JSONValue
	if value.IsValid() {
		t.Fatal("zero value should be invalid")
	}
	if value.Kind() != JSONKindInvalid {
		t.Fatalf("zero value kind = %q, want invalid", value.Kind())
	}
	if _, err := json.Marshal(value); err == nil {
		t.Fatal("MarshalJSON should reject invalid zero value")
	}

	data, err := json.Marshal(JSONNull())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "null" {
		t.Fatalf("JSONNull marshaled to %s, want null", data)
	}
}

func TestJSONValueHasNoExportedDataFields(t *testing.T) {
	typ := reflect.TypeOf(JSONValue{})
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if field.IsExported() {
			t.Fatalf("JSONValue field %s must not be exported", field.Name)
		}
	}
}

func TestJSONValueRoundTripsAllKindsAndPreservesNumbers(t *testing.T) {
	value, err := ParseJSONValue([]byte(`{"array":[null,true,"text",-12.340e+5],"object":{"n":9007199254740993}}`))
	if err != nil {
		t.Fatal(err)
	}

	object, ok := value.AsObject()
	if !ok {
		t.Fatal("root should be object")
	}
	array, ok := object["array"].AsArray()
	if !ok {
		t.Fatal("array member should be array")
	}
	if array[0].Kind() != JSONKindNull {
		t.Fatalf("array[0] kind = %q, want null", array[0].Kind())
	}
	if got, ok := array[1].AsBool(); !ok || !got {
		t.Fatalf("array[1] = %v/%v, want true", got, ok)
	}
	if got, ok := array[2].AsString(); !ok || got != "text" {
		t.Fatalf("array[2] = %q/%v, want text", got, ok)
	}
	if got, ok := array[3].AsNumber(); !ok || got.String() != "-12.340e+5" {
		t.Fatalf("array[3] = %q/%v, want original number token", got, ok)
	}
	nested, ok := object["object"].AsObject()
	if !ok {
		t.Fatal("object member should be object")
	}
	if got, ok := nested["n"].AsNumber(); !ok || got.String() != "9007199254740993" {
		t.Fatalf("nested number = %q/%v, want original integer token", got, ok)
	}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"array":[null,true,"text",-12.340e+5],"object":{"n":9007199254740993}}` {
		t.Fatalf("marshal = %s", data)
	}
}

func TestJSONValueUnmarshalRejectsDuplicateKeys(t *testing.T) {
	var value JSONValue
	err := json.Unmarshal([]byte(`{"outer":{"id":1,"id":2}}`), &value)
	if err == nil {
		t.Fatal("expected duplicate key error")
	}
	if !strings.Contains(err.Error(), "/outer") || !strings.Contains(err.Error(), "id") {
		t.Fatalf("error %q should contain duplicate key path", err)
	}
}

func TestJSONNumberValidation(t *testing.T) {
	valid := []string{"0", "-0", "1", "-12.34", "1e9", "1E-9"}
	for _, token := range valid {
		if _, err := JSONNumber(json.Number(token)); err != nil {
			t.Fatalf("JSONNumber(%q) unexpected error: %v", token, err)
		}
	}

	invalid := []string{"", "01", "+1", "1.", ".1", "NaN", "Infinity", "1 2", " 1"}
	for _, token := range invalid {
		if _, err := JSONNumber(json.Number(token)); err == nil {
			t.Fatalf("JSONNumber(%q) should fail", token)
		}
	}
}

func TestParseJSONValueRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPath string
		wantKey  string
	}{
		{
			name:     "root",
			input:    `{"model":"gpt-5","model":"gpt-4.1"}`,
			wantPath: "/",
			wantKey:  "model",
		},
		{
			name:     "nested object",
			input:    `{"outer":{"name":"a","name":"b"}}`,
			wantPath: "/outer",
			wantKey:  "name",
		},
		{
			name:     "array object",
			input:    `{"items":[{"id":1,"id":2}]}`,
			wantPath: "/items/0",
			wantKey:  "id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseJSONValue([]byte(tt.input))
			if err == nil {
				t.Fatal("expected duplicate key error")
			}
			message := err.Error()
			if !strings.Contains(message, tt.wantPath) || !strings.Contains(message, tt.wantKey) {
				t.Fatalf("error %q should contain path %q and key %q", message, tt.wantPath, tt.wantKey)
			}
		})
	}
}

func TestJSONValueConstructorsAndAccessorsCopyContainers(t *testing.T) {
	arraySource := []JSONValue{JSONString("before")}
	arrayValue := JSONArray(arraySource)
	arraySource[0] = JSONString("after")

	arrayCopy, ok := arrayValue.AsArray()
	if !ok {
		t.Fatal("expected array")
	}
	if got, _ := arrayCopy[0].AsString(); got != "before" {
		t.Fatalf("constructor did not copy array, got %q", got)
	}
	arrayCopy[0] = JSONString("changed")
	arrayCopyAgain, _ := arrayValue.AsArray()
	if got, _ := arrayCopyAgain[0].AsString(); got != "before" {
		t.Fatalf("accessor did not copy array, got %q", got)
	}

	objectSource := map[string]JSONValue{"key": JSONString("before")}
	objectValue := JSONObject(objectSource)
	objectSource["key"] = JSONString("after")

	objectCopy, ok := objectValue.AsObject()
	if !ok {
		t.Fatal("expected object")
	}
	if got, _ := objectCopy["key"].AsString(); got != "before" {
		t.Fatalf("constructor did not copy object, got %q", got)
	}
	objectCopy["key"] = JSONString("changed")
	objectCopyAgain, _ := objectValue.AsObject()
	if got, _ := objectCopyAgain["key"].AsString(); got != "before" {
		t.Fatalf("accessor did not copy object, got %q", got)
	}
}

func TestJSONValueAccessorsDeepCopyNestedContainers(t *testing.T) {
	value := JSONObject(map[string]JSONValue{
		"outer": JSONObject(map[string]JSONValue{
			"inner": JSONArray([]JSONValue{JSONString("before")}),
		}),
	})

	objectCopy, ok := value.AsObject()
	if !ok {
		t.Fatal("expected object")
	}
	outerCopy, _ := objectCopy["outer"].AsObject()
	innerCopy, _ := outerCopy["inner"].AsArray()
	innerCopy[0] = JSONString("after")
	outerCopy["inner"] = JSONArray(innerCopy)
	objectCopy["outer"] = JSONObject(outerCopy)

	originalObject, _ := value.AsObject()
	originalOuter, _ := originalObject["outer"].AsObject()
	originalInner, _ := originalOuter["inner"].AsArray()
	if got, _ := originalInner[0].AsString(); got != "before" {
		t.Fatalf("nested accessor mutation changed original value to %q", got)
	}
}

func TestJSONValueObjectMarshalIsDeterministic(t *testing.T) {
	value := JSONObject(map[string]JSONValue{
		"z": JSONString("last"),
		"a": JSONString("first"),
		"m": JSONObject(map[string]JSONValue{
			"beta":  JSONBool(true),
			"alpha": JSONNull(),
		}),
	})

	var previous string
	for attempt := 0; attempt < 25; attempt++ {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		if got != `{"a":"first","m":{"alpha":null,"beta":true},"z":"last"}` {
			t.Fatalf("unexpected marshal order/content: %s", got)
		}
		if previous != "" && got != previous {
			t.Fatalf("marshal changed from %s to %s", previous, got)
		}
		previous = got
	}
}

func TestJSONValueMarshalRejectsInvalidNestedValue(t *testing.T) {
	value := JSONObject(map[string]JSONValue{"bad": {}})
	_, err := json.Marshal(value)
	if err == nil {
		t.Fatal("expected nested invalid value to fail")
	}
	if !strings.Contains(err.Error(), "/bad") {
		t.Fatalf("error %q should contain nested path", err)
	}
}
