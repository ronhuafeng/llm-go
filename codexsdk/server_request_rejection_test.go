package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestServerRequestRejectionClassifiesMethodWithoutGuessingRun(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		code   float64
	}{
		{"unknown method", "fixture/unsupported", -32601},
		{"client request method", protocolv2.MethodTurnStart, -32601},
		{"notification as request", protocolv2.MethodTurnCompleted, -32601},
		{"known request invalid params", protocolv2.MethodItemToolCall, -32602},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := retentionClient(t, 100, 1<<20)
			defer client.Close()
			matched := retentionRun(client, "thread-1", "turn-1")
			other := retentionRun(client, "thread-2", "turn-2")
			var calls atomic.Int32
			client.options.ServerRequestHandler = func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
				calls.Add(1)
				return CommandExecutionApprovalResponse(protocolv2.CommandExecutionRequestApprovalResponse{
					Decision: protocolv2.NewCommandExecutionApprovalDecisionAccept(),
				}), nil
			}
			line, err := json.Marshal(map[string]any{
				"id": "rejected", "method": test.method,
				// These unvalidated IDs must not become run ownership.
				"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.ingestJSONRPCLine(line); err != nil {
				t.Fatal(err)
			}
			assertJSONRPCError(t, client, test.code)
			if calls.Load() != 0 || exactRunFinished(matched) || exactRunFinished(other) || client.isClosed() {
				t.Fatal("unvalidated request invoked a handler, acquired a run, or failed the client")
			}

			// Rejection does not prevent a later typed request or normal terminal.
			line, err = json.Marshal(map[string]any{
				"id": "accepted", "method": protocolv2.MethodItemCommandExecutionRequestApproval,
				"params": fakeCommandApprovalParams("thread-1", "turn-1"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.ingestJSONRPCLine(line); err != nil {
				t.Fatal(err)
			}
			waitForWrittenToken(t, client, `"id":"accepted"`)
			if calls.Load() != 1 || exactRunFinished(matched) || exactRunFinished(other) {
				t.Fatal("valid handler response changed request/run isolation")
			}
			terminal := correlationTerminal("thread-1", "turn-1", protocolv2.TurnStatusCompleted, "recovered")
			client.routeNotification(terminal)
			result, err := (&Stream[StartedThreadRun]{state: matched}).Wait(correlationWaitContext(t))
			if err != nil || !result.Run.FinalResponsePresent || result.Run.FinalResponse != "recovered" || exactRunFinished(other) {
				t.Fatalf("completion after rejection = %#v, err=%v", result.Run, err)
			}
			if err := client.Close(); err != nil {
				t.Fatalf("client failure after request rejection: %v", err)
			}
		})
	}
}

func TestRejectedServerRequestWriteFailureRemainsClientGlobal(t *testing.T) {
	for _, method := range []string{"fixture/unsupported", protocolv2.MethodItemToolCall} {
		t.Run(method, func(t *testing.T) {
			client := newTransportHarness()
			defer client.Close()
			writeErr := errors.New("rejection response write failed")
			client.stdin = failingWriteCloser{err: writeErr}
			matched := liveExactRun(client, "thread-1", "turn-1")
			other := liveExactRun(client, "thread-2", "turn-2")
			line, err := json.Marshal(map[string]any{
				"id": "rejected", "method": method,
				"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.ingestJSONRPCLine(line); err != nil {
				t.Fatal(err)
			}
			for _, run := range []*exactRunState{matched, other} {
				select {
				case <-run.done:
				case <-time.After(time.Second):
					t.Fatal("rejection response transport failure did not finish every run")
				}
				if !errors.Is(run.err, writeErr) {
					t.Fatalf("run failure = %v, want transport cause", run.err)
				}
			}
			if err := client.Close(); !errors.Is(err, writeErr) {
				t.Fatalf("Close error = %v, want transport cause", err)
			}
		})
	}
}
