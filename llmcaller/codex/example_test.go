package codexcaller_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

type exampleRunner struct{}

func (exampleRunner) Start(_ context.Context, request codexsdk.StartThreadRunRequest) (codexsdk.StartedThreadRun, error) {
	start := protocolv2.ThreadStartResponse{
		ApprovalPolicy:    protocolv2.NewAskForApprovalNever(),
		ApprovalsReviewer: protocolv2.ApprovalsReviewerUser,
		Model:             "gpt-example",
		Sandbox:           protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{}),
		Thread: protocolv2.Thread{
			ID: "thread-example", Ephemeral: true,
			Source: protocolv2.NewSessionSourceAppServer(),
			Status: protocolv2.NewThreadStatusIdle(),
			Turns:  []protocolv2.Turn{},
		},
	}
	if request.AdmitTurn != nil {
		pending := request.Turn
		pending.ThreadID = start.Thread.ID
		if err := request.AdmitTurn(start, pending); err != nil {
			return codexsdk.StartedThreadRun{Start: start}, err
		}
	}
	return codexsdk.StartedThreadRun{
		Start: start,
		Run: codexsdk.ThreadRunResult{
			Turn:                 protocolv2.Turn{ID: "turn-example", Items: []protocolv2.ThreadItem{}, Status: protocolv2.TurnStatusCompleted},
			FinalResponse:        `{"answer":"three layers"}`,
			FinalResponsePresent: true,
		},
	}, nil
}

func (exampleRunner) StartStream(context.Context, codexsdk.StartThreadRunRequest) (*codexsdk.Stream[codexsdk.StartedThreadRun], error) {
	return nil, nil
}

func Example() {
	// The application owns both requested execution settings and the rule that
	// admits effective thread/start facts before turn/start.
	options := codexcaller.Options{
		Runner: exampleRunner{},
		Defaults: codexsdk.StartThreadRunRequest{
			Thread: protocolv2.ThreadStartParams{
				ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
				Ephemeral:      protocolv2.Value(true),
				Sandbox:        protocolv2.Value(protocolv2.SandboxModeReadOnly),
			},
			AdmitTurn: func(start protocolv2.ThreadStartResponse, pending protocolv2.TurnStartParams) error {
				if !start.ApprovalPolicy.IsValid() || start.ApprovalPolicy.Kind() != protocolv2.AskForApprovalKindNever {
					return errors.New("application rejected approval policy")
				}
				if !start.Sandbox.IsValid() || start.Sandbox.Kind() != protocolv2.SandboxPolicyKindReadOnly {
					return errors.New("application rejected sandbox")
				}
				if !start.Thread.Ephemeral {
					return errors.New("application rejected non-ephemeral thread")
				}
				if pending.ApprovalPolicy != nil && (pending.ApprovalPolicy.Value == nil || pending.ApprovalPolicy.Value.Kind() != protocolv2.AskForApprovalKindNever) {
					return errors.New("application rejected pending approval policy")
				}
				if pending.SandboxPolicy != nil && (pending.SandboxPolicy.Value == nil || pending.SandboxPolicy.Value.Kind() != protocolv2.SandboxPolicyKindReadOnly) {
					return errors.New("application rejected pending sandbox")
				}
				return nil
			},
		},
	}
	caller, err := codexcaller.New(options)
	if err != nil {
		panic(err)
	}
	type result struct {
		Answer string `json:"answer"`
	}
	got, err := llmadapter.Value[result](context.Background(), caller, "Describe the layering.")
	if err != nil {
		panic(err)
	}
	fmt.Println(got.Value.Answer)
	// Output: three layers
}
