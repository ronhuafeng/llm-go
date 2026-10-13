package protocolv2

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestImageSourceAlternativesRoundTrip(t *testing.T) {
	for _, family := range []struct {
		name, tag, url, id string
		newValue           func() any
	}{
		{"user_input", "image", "url", "fileId", func() any { return new(UserInput) }},
		{"tool_output", "input_image", "image_url", "file_id", func() any { return new(FunctionCallOutputContentItem) }},
	} {
		t.Run(family.name, func(t *testing.T) {
			for _, fields := range []map[string]any{
				{family.url: ""}, {family.id: "id"}, {family.url: "ok", family.id: "id"},
				{family.url: "ok", family.id: 3}, {family.url: false, family.id: "id"},
				{family.url: nil, family.id: "id"}, {family.url: "ok", family.id: map[string]any{"nested": []any{true, nil}}},
			} {
				fields["type"] = family.tag
				wire, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				value := family.newValue()
				if err := json.Unmarshal(wire, value); err != nil {
					t.Fatalf("%s: %v", wire, err)
				}
				out, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var got, want any
				_ = json.Unmarshal(out, &got)
				_ = json.Unmarshal(wire, &want)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s became %s", wire, out)
				}
			}
			for _, fields := range []map[string]any{
				{}, {family.url: 3, family.id: false}, {family.url: nil}, {family.url: "ok", "extra": true},
				{family.url: "ok", "detail": 7},
			} {
				fields["type"] = family.tag
				wire, _ := json.Marshal(fields)
				if err := json.Unmarshal(wire, family.newValue()); err == nil {
					t.Fatalf("accepted %s", wire)
				}
			}
		})
	}
}
