package codexsdk

import (
	"context"
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

func TestExactRunFinalResponseMatchesAppServerCompletionSummary(t *testing.T) {
	agent := func(id, text string, phase *string) map[string]any {
		item := map[string]any{"id": id, "type": "agentMessage", "text": text}
		if phase != nil {
			item["phase"] = *phase
		}
		return item
	}
	final := "final_answer"
	commentary := "commentary"
	for _, test := range []struct {
		name    string
		items   []map[string]any
		present bool
		want    string
	}{
		{
			name:    "phase absent nonempty",
			items:   []map[string]any{agent("legacy", "legacy-final", nil)},
			present: true,
			want:    "legacy-final",
		},
		{
			name:    "phase absent blank is not a completion",
			items:   []map[string]any{agent("blank", "   ", nil)},
			present: false,
			want:    "",
		},
		{
			name:    "commentary is not a completion",
			items:   []map[string]any{agent("note", "thinking", &commentary)},
			present: false,
			want:    "",
		},
		{
			name: "explicit final answer wins over later phase-none",
			items: []map[string]any{
				agent("explicit", "chosen", &final),
				agent("legacy", "ignored-legacy", nil),
			},
			present: true,
			want:    "chosen",
		},
		{
			name: "explicit final answer wins over earlier phase-none",
			items: []map[string]any{
				agent("legacy", "ignored-legacy", nil),
				agent("explicit", "chosen", &final),
			},
			present: true,
			want:    "chosen",
		},
		{
			name: "latest phase-none wins among fallbacks",
			items: []map[string]any{
				agent("older", "older-legacy", nil),
				agent("newer", "newer-legacy", nil),
			},
			present: true,
			want:    "newer-legacy",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			typed, err := exactNotification(rpcNotification{
				method: protocolv2.MethodTurnCompleted,
				params: map[string]any{
					"threadId": "thread-summary",
					"turn": map[string]any{
						"id":     "turn-summary",
						"status": "completed",
						"items":  test.items,
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			state := newExactRunState(nil, "thread-summary", StartedThreadRun{})
			state.mu.Lock()
			terminal, terminalErr := state.applyTerminalLocked(typed)
			state.mu.Unlock()
			if !terminal || terminalErr != nil {
				t.Fatalf("terminal=(%v,%v), want completed without SDK failure", terminal, terminalErr)
			}
			result := state.result.(StartedThreadRun)
			if result.Run.Turn.Status != protocolv2.TurnStatusCompleted {
				t.Fatalf("turn status = %q, want completed", result.Run.Turn.Status)
			}
			if len(result.Run.Turn.Items) != len(test.items) {
				t.Fatalf("exact items rewritten: %#v", result.Run.Turn.Items)
			}
			if result.Run.FinalResponsePresent != test.present || result.Run.FinalResponse != test.want {
				t.Fatalf("final response = (%q, present=%v), want (%q, present=%v)", result.Run.FinalResponse, result.Run.FinalResponsePresent, test.want, test.present)
			}
		})
	}
}

func TestExactRunnerStartProjectsPhaseAbsentCompletionSummary(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	root, err := New(ClientOptions{CWD: t.TempDir(), Command: fakeCommand("phase-none-final")})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	result, err := root.ThreadRunner().Start(context.Background(), StartThreadRunRequest{
		Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Turn.Status != protocolv2.TurnStatusCompleted {
		t.Fatalf("status = %s, want completed", result.Run.Turn.Status)
	}
	if !result.Run.FinalResponsePresent || result.Run.FinalResponse != "legacy-final" {
		t.Fatalf("final response = (%q, present=%v), want present legacy-final", result.Run.FinalResponse, result.Run.FinalResponsePresent)
	}
	if len(result.Run.Turn.Items) != 1 {
		t.Fatalf("exact items = %#v", result.Run.Turn.Items)
	}
	message, ok := result.Run.Turn.Items[0].AsAgentMessage()
	if !ok || (message.Phase != nil && message.Phase.Value != nil) {
		t.Fatalf("exact item phase rewritten: %#v", result.Run.Turn.Items[0])
	}
}

func TestExactRunnerStartPrefersExplicitFinalAnswerOverPhaseNone(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	root, err := New(ClientOptions{CWD: t.TempDir(), Command: fakeCommand("final-answer-over-phase-none")})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	result, err := root.ThreadRunner().Start(context.Background(), StartThreadRunRequest{
		Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Run.FinalResponsePresent || result.Run.FinalResponse != "chosen-final" {
		t.Fatalf("final response = (%q, present=%v), want explicit chosen-final", result.Run.FinalResponse, result.Run.FinalResponsePresent)
	}
}
