package protocolv2

import (
	"encoding/json"
	"testing"
)

func TestImageSourceAlternativesPreservePresence(t *testing.T) {
	for _, tc := range []struct {
		name, tag, url, file string
		newValue             func() any
	}{
		{"user", "image", "url", "fileId", func() any { return new(UserInput) }},
		{"content", "input_image", "image_url", "file_id", func() any { return new(ContentItem) }},
		{"output", "input_image", "image_url", "file_id", func() any { return new(FunctionCallOutputContentItem) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fields := range []map[string]any{{tc.url: ""}, {tc.file: "file-1"}, {tc.url: "image", tc.file: "file-1"}} {
				fields["type"] = tc.tag
				raw, _ := json.Marshal(fields)
				value := tc.newValue()
				if err := json.Unmarshal(raw, value); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if string(encoded) != string(raw) {
					t.Fatalf("presence changed: %s -> %s", raw, encoded)
				}
			}
			for _, fields := range []map[string]any{{}, {tc.url: nil}, {tc.file: 3}, {tc.file: "file-1", tc.url: false}, {tc.url: "image", "extra": true}} {
				fields["type"] = tc.tag
				raw, _ := json.Marshal(fields)
				if err := json.Unmarshal(raw, tc.newValue()); err == nil {
					t.Fatalf("accepted invalid image: %s", raw)
				}
			}
		})
	}
	for _, value := range []any{NewUserInputImage(UserInputImage{}), NewContentItemInputImage(ContentItemInputImage{}), NewFunctionCallOutputContentItemInputImage(FunctionCallOutputContentItemInputImage{})} {
		if _, err := json.Marshal(value); err == nil {
			t.Fatal("encoded image without a source")
		}
	}
}

func TestNonNullableContainerElementsRejectNull(t *testing.T) {
	for _, tc := range []struct {
		input  string
		target any
	}{
		{`{"optOutNotificationMethods":[null]}`, new(InitializeCapabilities)},
		{`{"enablement":{"feature":null}}`, new(ExperimentalFeatureEnablementSetParams)},
		{`{"threadId":"t","transport":{"type":"websocket"},"codexResponseHandoffChannelPrefixes":{"commentary":null}}`, new(ThreadRealtimeStartParams)},
	} {
		if err := json.Unmarshal([]byte(tc.input), tc.target); err == nil {
			t.Fatalf("accepted null element: %s", tc.input)
		}
	}
	// Explicitly dynamic JSON still admits null inside maps and arrays.
	var response McpToolCallResult
	if err := json.Unmarshal([]byte(`{"content":[null],"structuredContent":{"value":null}}`), &response); err != nil {
		t.Fatal(err)
	}
}

func TestReasoningEffortUsesSourceMinLength(t *testing.T) {
	for _, input := range []string{`"low"`, `"future-effort"`, `"高"`} {
		var value ReasoningEffort
		if err := json.Unmarshal([]byte(input), &value); err != nil {
			t.Fatal(err)
		}
		if _, err := json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{`""`, `null`, `1`} {
		var value ReasoningEffort
		if err := json.Unmarshal([]byte(input), &value); err == nil {
			t.Fatalf("accepted invalid effort: %s", input)
		}
	}
	if _, err := json.Marshal(ReasoningEffort("")); err == nil {
		t.Fatal("encoded empty effort")
	}
}
