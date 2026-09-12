package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
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

func TestDecodeProtocolServerRequestPreservesURLModeElicitationPayload(t *testing.T) {
	request, err := decodeProtocolServerRequest(map[string]any{
		"id":     "elicit-1",
		"method": protocolv2.MethodMCPServerElicitationRequest,
		"params": map[string]any{
			"elicitationId": "elicit-1",
			"message":       "open the approval page",
			"mode":          "url",
			"serverName":    "mcp-server",
			"threadId":      "thread-1",
			"url":           "https://example.test/elicit",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := request.AsMCPServerElicitationRequest()
	if !ok {
		t.Fatalf("request kind = %s, want elicitation", request.Kind())
	}
	if payload.Params.ServerName != "mcp-server" || payload.Params.ThreadID != "thread-1" {
		t.Fatalf("shared elicitation fields = %#v", payload.Params)
	}
	urlPayload, ok := payload.Params.AsURL()
	if !ok || urlPayload.ElicitationID != "elicit-1" || urlPayload.Message != "open the approval page" || urlPayload.URL != "https://example.test/elicit" {
		t.Fatalf("url elicitation payload = %#v ok=%v", urlPayload, ok)
	}
}

func TestUnhandledExactServerRequestCauseCoversApplicationOwnedFamilies(t *testing.T) {
	for _, kind := range []protocolv2.ServerRequestKind{
		protocolv2.ServerRequestKindItemCommandExecutionRequestApproval,
		protocolv2.ServerRequestKindItemFileChangeRequestApproval,
		protocolv2.ServerRequestKindItemToolRequestUserInput,
		protocolv2.ServerRequestKindMCPServerElicitationRequest,
		protocolv2.ServerRequestKindItemPermissionsRequestApproval,
		protocolv2.ServerRequestKindCurrentTimeRead,
		protocolv2.ServerRequestKindApplyPatchApproval,
		protocolv2.ServerRequestKindExecCommandApproval,
	} {
		failure := unhandledExactServerRequest(kind)
		if failure.Kind != kind || failure.Reason != "no server request handler is configured" {
			t.Fatalf("unhandled cause for %s = %#v", kind, failure)
		}
		if !errors.Is(failure, ErrExactServerRequest) {
			t.Fatalf("unhandled cause for %s does not preserve ErrExactServerRequest: %v", kind, failure)
		}
	}
}

func TestNilServerRequestHandlerReturnsProtocolErrorForDecodedApplicationOwnedFamilies(t *testing.T) {
	tests := []struct {
		name   string
		method string
		params map[string]any
		kind   protocolv2.ServerRequestKind
	}{
		{
			name:   "current time",
			method: protocolv2.MethodCurrentTimeRead,
			params: map[string]any{"threadId": "thread-1"},
			kind:   protocolv2.ServerRequestKindCurrentTimeRead,
		},
		{
			name:   "permissions",
			method: protocolv2.MethodItemPermissionsRequestApproval,
			params: map[string]any{
				"cwd":         "/workspace",
				"itemId":      "item-1",
				"permissions": map[string]any{},
				"startedAtMs": 1,
				"threadId":    "thread-1",
				"turnId":      "turn-1",
			},
			kind: protocolv2.ServerRequestKindItemPermissionsRequestApproval,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := map[string]any{
				"id":     "request-1",
				"method": test.method,
				"params": test.params,
			}
			request, err := decodeProtocolServerRequest(message)
			if err != nil {
				t.Fatalf("decodeProtocolServerRequest: %v", err)
			}
			if request.Kind() != test.kind {
				t.Fatalf("request kind = %s, want %s", request.Kind(), test.kind)
			}

			client := newTransportHarness()
			client.respondToExactServerRequest(context.Background(), message["id"], request)

			written := strings.Split(strings.TrimSpace(client.stdin.(*recordingWriteCloser).String()), "\n")
			if len(written) == 0 || written[len(written)-1] == "" {
				t.Fatal("missing JSON-RPC failure response")
			}
			var response map[string]any
			if err := json.Unmarshal([]byte(written[len(written)-1]), &response); err != nil {
				t.Fatalf("decode written response: %v", err)
			}
			if _, ok := response["result"]; ok {
				t.Fatalf("nil handler synthesized successful result: %#v", response)
			}
			errorObject, ok := response["error"].(map[string]any)
			if !ok || errorObject["code"] != float64(-32000) {
				t.Fatalf("JSON-RPC error = %#v", response["error"])
			}
			if !strings.Contains(errorObject["message"].(string), "no server request handler is configured") {
				t.Fatalf("JSON-RPC error message = %#v", errorObject["message"])
			}
			if closeErr := client.Close(); !errors.Is(closeErr, ErrExactServerRequest) {
				t.Fatalf("Close error = %v, want typed exact server request cause", closeErr)
			}
		})
	}
}

func TestAdmissionClosedServerRequestReturnsProtocolErrorWithoutSemanticResult(t *testing.T) {
	message := map[string]any{
		"id":     "request-closed",
		"method": protocolv2.MethodCurrentTimeRead,
		"params": map[string]any{"threadId": "thread-1"},
	}
	request, err := decodeProtocolServerRequest(message)
	if err != nil {
		t.Fatal(err)
	}
	client := newTransportHarness()
	client.rejectExactServerRequestAfterAdmissionClosed(message["id"], request)

	written := strings.Split(strings.TrimSpace(client.stdin.(*recordingWriteCloser).String()), "\n")
	var response map[string]any
	if err := json.Unmarshal([]byte(written[len(written)-1]), &response); err != nil {
		t.Fatal(err)
	}
	if _, ok := response["result"]; ok {
		t.Fatalf("closed admission synthesized semantic result: %#v", response)
	}
	errorObject, ok := response["error"].(map[string]any)
	if !ok || !strings.Contains(errorObject["message"].(string), "callback admission is closed") {
		t.Fatalf("JSON-RPC error = %#v", response["error"])
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}
