package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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
			if closeErr := client.Close(); closeErr != nil {
				t.Fatalf("Close error = %v, want nil client cause for an application-level request failure", closeErr)
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

func TestApplicationServerRequestFailureStaysOnMatchingExactRun(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler ServerRequestHandler
		code    float64
	}{
		{name: "missing handler", code: -32000},
		{
			name: "handler error",
			code: -32000,
			handler: func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
				return ServerRequestResponse{}, errors.New("application rejected")
			},
		},
		{
			name: "handler panic",
			code: -32000,
			handler: func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
				panic("application panicked")
			},
		},
		{
			name: "mismatched response",
			code: -32602,
			handler: func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
				return FileChangeApprovalResponse(protocolv2.FileChangeRequestApprovalResponse{Decision: protocolv2.FileChangeApprovalDecisionDecline}), nil
			},
		},
		{
			name: "empty response",
			code: -32602,
			handler: func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
				return ServerRequestResponse{}, nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newTransportHarness()
			client.options.ServerRequestHandler = test.handler
			matched := liveExactRun(client, "thread-matched", "turn-matched")
			unrelated := liveExactRun(client, "thread-other", "turn-other")

			message := map[string]any{
				"id":     "request-1",
				"method": protocolv2.MethodItemCommandExecutionRequestApproval,
				"params": fakeCommandApprovalParams("thread-matched", "turn-matched"),
			}
			request, err := decodeProtocolServerRequest(message)
			if err != nil {
				t.Fatal(err)
			}
			client.respondToExactServerRequest(context.Background(), message["id"], request)

			assertJSONRPCError(t, client, test.code)
			if !exactRunFinished(matched) {
				t.Fatal("matched Exact Run was not finished")
			}
			if !errors.Is(matched.err, ErrExactServerRequest) && !errors.Is(matched.err, ErrHandlerFailed) {
				t.Fatalf("matched run error = %v, want application-level request cause", matched.err)
			}
			if exactRunFinished(unrelated) {
				t.Fatalf("unrelated Exact Run was finished: %v", unrelated.err)
			}
			if closeErr := client.Close(); closeErr != nil {
				t.Fatalf("Close error = %v, want nil client cause", closeErr)
			}
		})
	}
}

func TestThreadScopedServerRequestFailureMatchesExactRunByThreadID(t *testing.T) {
	client := newTransportHarness()
	matched := liveExactRun(client, "thread-1", "turn-1")
	unrelated := liveExactRun(client, "thread-2", "turn-2")
	message := map[string]any{
		"id":     "time-1",
		"method": protocolv2.MethodCurrentTimeRead,
		"params": map[string]any{"threadId": "thread-1"},
	}
	request, err := decodeProtocolServerRequest(message)
	if err != nil {
		t.Fatal(err)
	}
	client.respondToExactServerRequest(context.Background(), message["id"], request)
	assertJSONRPCError(t, client, -32000)
	if !exactRunFinished(matched) || !errors.Is(matched.err, ErrExactServerRequest) {
		t.Fatalf("thread-scoped run error = %v", matched.err)
	}
	if exactRunFinished(unrelated) {
		t.Fatalf("other-thread run was finished: %v", unrelated.err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("Close error = %v, want nil client cause", closeErr)
	}
}

func TestTurnScopedServerRequestDoesNotFinishUnrelatedAttachingRun(t *testing.T) {
	client := newTransportHarness()
	live := liveExactRun(client, "thread-1", "turn-1")
	attaching := attachingExactRun(client, "thread-1")
	message := map[string]any{
		"id":     "request-1",
		"method": protocolv2.MethodItemCommandExecutionRequestApproval,
		"params": fakeCommandApprovalParams("thread-1", "turn-1"),
	}
	request, err := decodeProtocolServerRequest(message)
	if err != nil {
		t.Fatal(err)
	}
	client.respondToExactServerRequest(context.Background(), message["id"], request)
	assertJSONRPCError(t, client, -32000)
	if !exactRunFinished(live) || !errors.Is(live.err, ErrExactServerRequest) {
		t.Fatalf("live run error = %v", live.err)
	}
	if exactRunFinished(attaching) {
		t.Fatalf("unpublished attaching run was finished by a live turn's request: %v", attaching.err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("Close error = %v, want nil client cause", closeErr)
	}
}

func TestUnpublishedAttachingRunReceivesTurnScopedFailureBeforeTurnID(t *testing.T) {
	client := newTransportHarness()
	attaching := attachingExactRun(client, "thread-1")
	unrelated := liveExactRun(client, "thread-2", "turn-2")
	message := map[string]any{
		"id":     "request-1",
		"method": protocolv2.MethodItemCommandExecutionRequestApproval,
		"params": fakeCommandApprovalParams("thread-1", "turn-pending"),
	}
	request, err := decodeProtocolServerRequest(message)
	if err != nil {
		t.Fatal(err)
	}
	client.respondToExactServerRequest(context.Background(), message["id"], request)
	assertJSONRPCError(t, client, -32000)
	if !exactRunFinished(attaching) || !errors.Is(attaching.err, ErrExactServerRequest) {
		t.Fatalf("attaching run error = %v", attaching.err)
	}
	if exactRunFinished(unrelated) {
		t.Fatalf("other-thread run was finished: %v", unrelated.err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("Close error = %v, want nil client cause", closeErr)
	}
}

func TestConversationIDServerRequestFailureMatchesExactRunByThread(t *testing.T) {
	for _, test := range []struct {
		name    string
		message map[string]any
	}{
		{
			name: "apply patch",
			message: map[string]any{
				"id":     "patch-1",
				"method": protocolv2.MethodApplyPatchApproval,
				"params": map[string]any{
					"callId":         "call-1",
					"conversationId": "thread-1",
					"fileChanges":    map[string]any{"/tmp/a.txt": map[string]any{"type": "add", "content": "x"}},
				},
			},
		},
		{
			name: "exec command",
			message: map[string]any{
				"id":     "exec-1",
				"method": protocolv2.MethodExecCommandApproval,
				"params": map[string]any{
					"callId":         "call-1",
					"command":        []any{"echo"},
					"conversationId": "thread-1",
					"cwd":            "/tmp",
					"parsedCmd":      []any{map[string]any{"type": "unknown", "cmd": "echo"}},
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newTransportHarness()
			matched := liveExactRun(client, "thread-1", "turn-1")
			unrelated := liveExactRun(client, "thread-2", "turn-2")
			request, err := decodeProtocolServerRequest(test.message)
			if err != nil {
				t.Fatal(err)
			}
			client.respondToExactServerRequest(context.Background(), test.message["id"], request)
			assertJSONRPCError(t, client, -32000)
			if !exactRunFinished(matched) || !errors.Is(matched.err, ErrExactServerRequest) {
				t.Fatalf("conversationId-correlated run error = %v", matched.err)
			}
			if exactRunFinished(unrelated) {
				t.Fatalf("other-thread run was finished: %v", unrelated.err)
			}
			if closeErr := client.Close(); closeErr != nil {
				t.Fatalf("Close error = %v, want nil client cause", closeErr)
			}
		})
	}
}

func TestUncorrelatedServerRequestFailureIsNotGuessedOntoARun(t *testing.T) {
	client := newTransportHarness()
	matchedThread := liveExactRun(client, "thread-1", "turn-1")
	other := liveExactRun(client, "thread-2", "turn-2")

	message := map[string]any{
		"id":     "auth-1",
		"method": protocolv2.MethodAccountChatGPTAuthTokensRefresh,
		"params": map[string]any{"reason": "unauthorized"},
	}
	request, err := decodeProtocolServerRequest(message)
	if err != nil {
		t.Fatal(err)
	}
	client.respondToExactServerRequest(context.Background(), message["id"], request)

	assertJSONRPCError(t, client, -32000)
	if exactRunFinished(matchedThread) {
		t.Fatalf("uncorrelated failure attached to thread-1: %v", matchedThread.err)
	}
	if exactRunFinished(other) {
		t.Fatalf("uncorrelated failure attached to thread-2: %v", other.err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("Close error = %v, want nil client cause", closeErr)
	}
}

func TestDecodedServerRequestFailureDoesNotFailClient(t *testing.T) {
	client := newTransportHarness()
	run := liveExactRun(client, "thread-1", "turn-1")
	client.handleServerRequest(map[string]any{
		"id":     "request-bad",
		"method": protocolv2.MethodItemCommandExecutionRequestApproval,
		"params": fakeCommandApprovalParams("thread-1", "turn-1"),
		"future": true,
	})
	assertJSONRPCError(t, client, -32602)
	if exactRunFinished(run) {
		t.Fatalf("undecoded request was guessed onto a run: %v", run.err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("Close error = %v, want nil client cause", closeErr)
	}
}

func TestServerRequestResponseWriteFailureFailsClientGlobally(t *testing.T) {
	client := newTransportHarness()
	writeErr := errors.New("stdin write failed")
	client.stdin = failingWriteCloser{err: writeErr}
	client.options.ServerRequestHandler = func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
		return CommandExecutionApprovalResponse(protocolv2.CommandExecutionRequestApprovalResponse{
			Decision: protocolv2.NewCommandExecutionApprovalDecisionAccept(),
		}), nil
	}
	matched := liveExactRun(client, "thread-matched", "turn-matched")
	unrelated := liveExactRun(client, "thread-other", "turn-other")
	finished := make(chan *exactRunState, 2)
	for _, stream := range []*exactRunState{matched, unrelated} {
		go func(stream *exactRunState) {
			<-stream.done
			finished <- stream
		}(stream)
	}
	message := map[string]any{
		"id":     "request-1",
		"method": protocolv2.MethodItemCommandExecutionRequestApproval,
		"params": fakeCommandApprovalParams("thread-matched", "turn-matched"),
	}
	request, err := decodeProtocolServerRequest(message)
	if err != nil {
		t.Fatal(err)
	}
	client.respondToExactServerRequest(context.Background(), message["id"], request)
	seen := map[*exactRunState]bool{}
	deadline := time.After(time.Second)
	for len(seen) < 2 {
		select {
		case stream := <-finished:
			seen[stream] = true
		case <-deadline:
			t.Fatal("transport failure did not finish concurrent Exact Runs")
		}
	}
	if !errors.Is(client.Close(), writeErr) {
		t.Fatalf("Close error = %v, want transport write cause", client.failure)
	}
	if !errors.Is(matched.err, writeErr) {
		t.Fatalf("matched run error = %v, want transport write cause", matched.err)
	}
	if !errors.Is(unrelated.err, writeErr) {
		t.Fatalf("unrelated run error = %v, want client-global transport cause", unrelated.err)
	}
}

func liveExactRun(c *Client, threadID, turnID string) *exactRunState {
	state := newExactRunState(c, threadID, StartedThreadRun{})
	state.turnID = turnID
	if c.exactStreams[turnID] == nil {
		c.exactStreams[turnID] = map[*exactRunState]struct{}{}
	}
	c.exactStreams[turnID][state] = struct{}{}
	return state
}

func attachingExactRun(c *Client, threadID string) *exactRunState {
	state := newExactRunState(c, threadID, StartedThreadRun{})
	if c.exactAttaching[threadID] == nil {
		c.exactAttaching[threadID] = map[*exactRunState]struct{}{}
	}
	c.exactAttaching[threadID][state] = struct{}{}
	return state
}

func exactRunFinished(state *exactRunState) bool {
	select {
	case <-state.done:
		return true
	default:
		return false
	}
}

func assertJSONRPCError(t *testing.T, client *Client, code float64) {
	t.Helper()
	written := strings.Split(strings.TrimSpace(client.stdin.(*recordingWriteCloser).String()), "\n")
	if len(written) == 0 || written[len(written)-1] == "" {
		t.Fatal("missing JSON-RPC failure response")
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(written[len(written)-1]), &response); err != nil {
		t.Fatalf("decode written response: %v", err)
	}
	if _, ok := response["result"]; ok {
		t.Fatalf("synthesized successful result: %#v", response)
	}
	errorObject, ok := response["error"].(map[string]any)
	if !ok || errorObject["code"] != code {
		t.Fatalf("JSON-RPC error = %#v, want code %v", response["error"], code)
	}
}

type failingWriteCloser struct{ err error }

func (w failingWriteCloser) Write([]byte) (int, error) { return 0, w.err }

func (w failingWriteCloser) Close() error { return nil }
