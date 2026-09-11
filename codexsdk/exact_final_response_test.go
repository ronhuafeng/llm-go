package codexsdk

import (
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestExactRunFinalResponsePreservesPresenceWithoutStrengtheningCompletion(t *testing.T) {
	text := func(value string) *string { return &value }
	for _, test := range []struct {
		name    string
		text    *string
		present bool
		want    string
	}{
		{name: "absent", text: nil, present: false, want: ""},
		{name: "present empty", text: text(""), present: true, want: ""},
		{name: "present nonempty", text: text("done"), present: true, want: "done"},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := []map[string]any{}
			if test.text != nil {
				items = append(items, map[string]any{
					"id":    "item-final",
					"type":  "agentMessage",
					"text":  *test.text,
					"phase": "final_answer",
				})
			}
			typed, err := exactNotification(rpcNotification{
				method: protocolv2.MethodTurnCompleted,
				params: map[string]any{
					"threadId": "thread-presence",
					"turn": map[string]any{
						"id":     "turn-presence",
						"status": "completed",
						"items":  items,
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			state := newExactRunState(nil, "thread-presence", StartedThreadRun{})
			state.mu.Lock()
			terminal, terminalErr := state.applyTerminalLocked(typed)
			state.mu.Unlock()
			if !terminal || terminalErr != nil {
				t.Fatalf("terminal=(%v,%v), want completed without SDK failure", terminal, terminalErr)
			}
			result, ok := state.result.(StartedThreadRun)
			if !ok {
				t.Fatalf("result type = %T", state.result)
			}
			if result.Run.Turn.Status != protocolv2.TurnStatusCompleted {
				t.Fatalf("turn status = %q, want completed", result.Run.Turn.Status)
			}
			if result.Run.FinalResponsePresent != test.present || result.Run.FinalResponse != test.want {
				t.Fatalf("final response = (%q, present=%v), want (%q, present=%v)", result.Run.FinalResponse, result.Run.FinalResponsePresent, test.want, test.present)
			}
		})
	}
}
