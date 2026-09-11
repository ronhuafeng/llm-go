package integration

import (
	"errors"
	"fmt"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
)

var errApplicationAdmission = errors.New("integration application rejected effective Codex execution facts")

func readOnlyApplicationOptions(runner codexcaller.ThreadRunner) codexcaller.Options {
	return codexcaller.Options{
		Runner: runner,
		Defaults: codexsdk.StartThreadRunRequest{
			Thread: protocolv2.ThreadStartParams{
				ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
				Ephemeral:      protocolv2.Value(true),
				Sandbox:        protocolv2.Value(protocolv2.SandboxModeReadOnly),
			},
			Turn: protocolv2.TurnStartParams{
				ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
				SandboxPolicy:  protocolv2.Value(protocolv2.NewSandboxPolicyReadOnly(protocolv2.SandboxPolicyReadOnly{})),
			},
			AdmitTurn: admitReadOnlyEphemeral,
		},
	}
}

func admitReadOnlyEphemeral(start protocolv2.ThreadStartResponse) error {
	if !start.ApprovalPolicy.IsValid() {
		return fmt.Errorf("%w: approval policy is unknown", errApplicationAdmission)
	}
	if start.ApprovalPolicy.Kind() != protocolv2.AskForApprovalKindNever {
		return fmt.Errorf("%w: approval policy is not never", errApplicationAdmission)
	}
	if !start.Sandbox.IsValid() {
		return fmt.Errorf("%w: sandbox is unknown", errApplicationAdmission)
	}
	if start.Sandbox.Kind() != protocolv2.SandboxPolicyKindReadOnly {
		return fmt.Errorf("%w: sandbox is not read-only", errApplicationAdmission)
	}
	if !start.Thread.Ephemeral {
		return fmt.Errorf("%w: thread is not ephemeral", errApplicationAdmission)
	}
	return nil
}
