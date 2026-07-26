package codexsdk

import (
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestDecodeProtocolServerRequestRejectsAdditionalMembersRecursively(t *testing.T) {
	params := map[string]any{
		"itemId":      "item-1",
		"startedAtMs": 1,
		"threadId":    "thread-1",
		"turnId":      "turn-1",
	}
	for _, test := range []struct {
		name    string
		message map[string]any
	}{
		{
			name: "message root",
			message: map[string]any{
				"id":     "approval-1",
				"method": protocolv2.MethodItemCommandExecutionRequestApproval,
				"params": params,
				"future": true,
			},
		},
		{
			name: "nested params",
			message: map[string]any{
				"id":     "approval-1",
				"method": protocolv2.MethodItemCommandExecutionRequestApproval,
				"params": map[string]any{
					"itemId":      "item-1",
					"startedAtMs": 1,
					"threadId":    "thread-1",
					"turnId":      "turn-1",
					"future":      true,
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeProtocolServerRequest(test.message)
			if err == nil || !strings.Contains(err.Error(), `unknown field "future"`) {
				t.Fatalf("decodeProtocolServerRequest error = %v, want additional-member rejection", err)
			}
		})
	}
}
