package integration

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/internal/tools/internal/livecodex"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
)

func liveApplicationOptions(runner codexcaller.ThreadRunner, fixture livecodex.Fixture) codexcaller.Options {
	options := readOnlyApplicationOptions(runner)
	inner := options.Defaults.AdmitTurn
	options.Defaults.Thread.CWD = protocolv2.Value(fixture.Workspace)
	options.Defaults.Thread.Model = protocolv2.Value(livecodex.Model)
	options.Defaults.Thread.ModelProvider = protocolv2.Value(livecodex.Provider)
	options.Defaults.Turn.Effort = protocolv2.Value(protocolv2.ReasoningEffort(livecodex.Reasoning))
	options.Defaults.AdmitTurn = func(start protocolv2.ThreadStartResponse, pending protocolv2.TurnStartParams) error {
		if err := inner(start, pending); err != nil {
			return err
		}
		return admitLiveProvider(start, livecodex.Provider)
	}
	return options
}

func admitLiveProvider(start protocolv2.ThreadStartResponse, provider string) error {
	if start.ModelProvider != provider {
		return fmt.Errorf("%w: model provider is %q, want %s", errApplicationAdmission, start.ModelProvider, provider)
	}
	return nil
}

func TestLiveApplicationOptionsPinIsolatedProvider(t *testing.T) {
	const provider = livecodex.Provider
	options := liveApplicationOptions(nil, livecodex.Fixture{Workspace: "isolated-workspace"})
	if options.Defaults.Thread.ModelProvider == nil || options.Defaults.Thread.ModelProvider.Value == nil || *options.Defaults.Thread.ModelProvider.Value != provider {
		t.Fatalf("Thread.ModelProvider = %#v, want %q", options.Defaults.Thread.ModelProvider, provider)
	}
}

func TestAdmitLiveProviderRejectsDefaultChatGPTProvider(t *testing.T) {
	err := admitLiveProvider(protocolv2.ThreadStartResponse{ModelProvider: "openai"}, livecodex.Provider)
	if !errors.Is(err, errApplicationAdmission) {
		t.Fatalf("error = %v, want %v", err, errApplicationAdmission)
	}
}

func TestAdmitLiveProviderAcceptsConfiguredProvider(t *testing.T) {
	if err := admitLiveProvider(protocolv2.ThreadStartResponse{ModelProvider: livecodex.Provider}, livecodex.Provider); err != nil {
		t.Fatal(err)
	}
}
