package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/internal/tools/internal/livecodex"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

// A usable response is a requirement of these tasks, not a new SDK default.
func liveTurnError(run codexsdk.ThreadRunResult, previousTurnID string) error {
	if run.Turn.ID == "" || (previousTurnID != "" && run.Turn.ID == previousTurnID) {
		return errors.New("live work did not produce a new turn identity")
	}
	if run.Turn.Status != protocolv2.TurnStatusCompleted {
		return errors.New("live turn did not complete")
	}
	if !run.FinalResponsePresent || strings.TrimSpace(run.FinalResponse) == "" {
		return errors.New("live turn did not return usable work")
	}
	return nil
}

type arithmeticResponse string

func (response arithmeticResponse) Call(context.Context, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{FinalResponse: llmadapter.Observed(string(response))}, nil
}

func TestLiveArithmeticSchemaBoundary(t *testing.T) {
	for _, test := range []struct {
		name, response string
		accepted       bool
	}{
		{"integer", `{"number":7}`, true},
		{"equivalent formatting", "{\n  \"number\" : 7\n}", true},
		{"missing number", `{}`, false},
		{"extra property", `{"number":7,"extra":true}`, false},
		{"fraction", `{"number":7.5}`, false},
		{"string", `{"number":"7"}`, false},
		{"null", `{"number":null}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := llmadapter.Value[liveArithmeticResult](context.Background(), arithmeticResponse(test.response), "What is 3 + 4?")
			if (err == nil) != test.accepted {
				t.Fatalf("accepted=%v, err=%v", test.accepted, err)
			}
			if test.accepted && got.Value.Number != 7 {
				t.Fatal("typed result did not preserve the parsed integer")
			}
		})
	}
}

func TestLiveResumeAdmissionBindsObservedAndPendingThread(t *testing.T) {
	for _, test := range []struct {
		name, expected, observed, pending, provider string
		ephemeral, accepted                         bool
	}{
		{"parent continuation", "parent", "parent", "parent", livecodex.Provider, false, true},
		{"fork continuation", "fork", "fork", "fork", livecodex.Provider, false, true},
		{"fork work returned parent", "fork", "parent", "parent", livecodex.Provider, false, false},
		{"pending turn targets parent", "fork", "fork", "parent", livecodex.Provider, false, false},
		{"unrelated thread", "parent", "unrelated", "unrelated", livecodex.Provider, false, false},
		{"missing identity", "", "", "", livecodex.Provider, false, false},
		{"wrong provider", "parent", "parent", "parent", "openai", false, false},
		{"ephemeral continuation", "parent", "parent", "parent", livecodex.Provider, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := liveResumeRequest(livecodex.Fixture{Workspace: "isolated-workspace"}, test.expected, "new work")
			pending := request.Turn
			pending.ThreadID = test.pending
			err := request.AdmitTurn(protocolv2.ThreadResumeResponse{
				Thread:         protocolv2.Thread{ID: test.observed, Ephemeral: test.ephemeral},
				ApprovalPolicy: protocolv2.NewAskForApprovalNever(),
				Sandbox:        protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{}),
				ModelProvider:  test.provider,
			}, pending)
			if (err == nil) != test.accepted {
				t.Fatalf("accepted=%v, err=%v", test.accepted, err)
			}
		})
	}
}

func TestLiveTurnRequiresNewCompletedWork(t *testing.T) {
	for _, test := range []struct {
		name, id, previous, response string
		status                       protocolv2.TurnStatus
		present, accepted            bool
	}{
		{name: "new completed turn", id: "new", status: protocolv2.TurnStatusCompleted, present: true, response: "7", accepted: true},
		{name: "old parent turn", id: "old", previous: "old", status: protocolv2.TurnStatusCompleted, present: true, response: "7"},
		{name: "failed with partial text", id: "new", status: protocolv2.TurnStatusFailed, present: true, response: "7"},
		{name: "incomplete", id: "new", status: protocolv2.TurnStatusInProgress, present: true, response: "7"},
		{name: "missing turn identity", status: protocolv2.TurnStatusCompleted, present: true, response: "7"},
		{name: "absent output", id: "new", status: protocolv2.TurnStatusCompleted, response: "7"},
		{name: "present empty output", id: "new", status: protocolv2.TurnStatusCompleted, present: true},
		{name: "blank output", id: "new", status: protocolv2.TurnStatusCompleted, present: true, response: " \n\t"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := liveTurnError(codexsdk.ThreadRunResult{
				Turn:          protocolv2.Turn{ID: test.id, Status: test.status},
				FinalResponse: test.response, FinalResponsePresent: test.present,
			}, test.previous)
			if (err == nil) != test.accepted {
				t.Fatalf("accepted=%v, err=%v", test.accepted, err)
			}
		})
	}
}
