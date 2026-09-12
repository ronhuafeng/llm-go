package codexsdk

import (
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestProtocolParamsAdmissionFollowsGeneratedClassification(t *testing.T) {
	client := &Client{}
	exclude := true
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodThreadResume, protocolv2.ThreadResumeParams{
		ThreadID:     "thread-1",
		ExcludeTurns: &exclude,
	}); err != nil {
		t.Fatalf("stable excludeTurns rejected: %v", err)
	}
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodThreadFork, protocolv2.ThreadForkParams{
		ThreadID:     "thread-1",
		ExcludeTurns: &exclude,
	}); err != nil {
		t.Fatalf("stable fork excludeTurns rejected: %v", err)
	}
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodThreadResume, protocolv2.ThreadResumeParams{
		ThreadID: "thread-1",
		Path:     protocolv2.Value("/tmp/rollout.jsonl"),
	}); err == nil || !strings.Contains(err.Error(), "thread/resume.path") {
		t.Fatalf("experimental resume path error = %v", err)
	}
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodTurnStart, protocolv2.TurnStartParams{
		Input:             []protocolv2.UserInput{},
		AdditionalContext: protocolv2.Value(map[string]protocolv2.AdditionalContextEntry{}),
	}); err == nil || !strings.Contains(err.Error(), "turn/start.additionalContext") {
		t.Fatalf("experimental additionalContext error = %v", err)
	}
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodTurnStart, protocolv2.TurnStartParams{
		Input: []protocolv2.UserInput{},
		Model: protocolv2.Value("gpt-stable"),
	}); err != nil {
		t.Fatalf("stable turn model rejected: %v", err)
	}
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodTurnSteer, protocolv2.TurnSteerParams{
		Input:             []protocolv2.UserInput{},
		AdditionalContext: protocolv2.Value(map[string]protocolv2.AdditionalContextEntry{}),
	}); err == nil || !strings.Contains(err.Error(), "turn/steer.additionalContext") {
		t.Fatalf("experimental steer additionalContext error = %v", err)
	}
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodAccountLoginStart, protocolv2.NewLoginAccountParamsAPIKey(protocolv2.LoginAccountParamsAPIKey{APIKey: "sk-test"})); err != nil {
		t.Fatalf("stable login variant rejected: %v", err)
	}
	enabled := true
	client.options.Initialize.Capabilities = protocolv2.Value(protocolv2.InitializeCapabilities{ExperimentalAPI: &enabled})
	if err := client.checkProtocolParamsAllowed(protocolv2.MethodThreadResume, protocolv2.ThreadResumeParams{
		ThreadID: "thread-1",
		Path:     protocolv2.Value("/tmp/rollout.jsonl"),
	}); err != nil {
		t.Fatalf("ExperimentalAPI still rejected classified experimental field: %v", err)
	}
}
