package codexsdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestTurnResponseSnapshotDoesNotReplaceCompletionNotification(t *testing.T) {
	for _, status := range []protocolv2.TurnStatus{protocolv2.TurnStatusCompleted, protocolv2.TurnStatusFailed, protocolv2.TurnStatusInterrupted} {
		t.Run(string(status), func(t *testing.T) {
			client := retentionClient(t, 100, 1<<20)
			defer client.Close()
			state := newExactRunState(client, "thread-1", StartedThreadRun{Start: facadeThreadStartResponse("thread-1", "fixture")})
			// Deliberately nonstandard for Codex 0.162.1: an inline terminal-looking
			// response is a partial snapshot, not a substitute completion event.
			client.attachExactStreamForTurn(state, protocolv2.Turn{
				ID: "turn-1", Status: status,
				Items: []protocolv2.ThreadItem{protocolv2.NewThreadItemAgentMessage(protocolv2.ThreadItemAgentMessage{
					ID: "inline-message", Text: "inline snapshot", Phase: protocolv2.Value(protocolv2.MessagePhaseFinalAnswer),
				})},
			})
			stream := &Stream[StartedThreadRun]{state: state}
			ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
			defer cancel()
			partial, err := stream.Wait(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || partial.Run.Turn.ID != "turn-1" || partial.Run.Turn.Status != status || len(partial.Run.Turn.Items) != 1 {
				t.Fatalf("response-only Wait = %#v, err=%v", partial.Run, err)
			}
			if _, available := stream.Result(); !available || exactRunFinished(state) || stream.Err() != nil || partial.Run.FinalResponsePresent || len(partial.Run.Notifications) != 0 {
				t.Fatal("inline response or caller deadline completed the shared run")
			}
			partial.Run.Turn.ID = "caller-local-mutation"
			client.routeNotification(correlationTerminal("thread-1", "turn-1", protocolv2.TurnStatusCompleted, "observed terminal"))
			result, err := stream.Wait(correlationWaitContext(t))
			if err != nil || result.Run.Turn.ID != "turn-1" || result.Run.Turn.Status != protocolv2.TurnStatusCompleted || !result.Run.FinalResponsePresent || result.Run.FinalResponse != "observed terminal" || len(result.Run.Notifications) != 1 {
				t.Fatalf("notification completion = %#v, err=%v", result.Run, err)
			}
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
