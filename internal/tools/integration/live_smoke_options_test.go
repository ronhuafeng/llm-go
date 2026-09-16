package integration

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
)

const liveSmokeModelProvider = "llm-go-smoke"

func liveSmokeApplicationOptions(runner codexcaller.ThreadRunner) codexcaller.Options {
	options := readOnlyApplicationOptions(runner)
	inner := options.Defaults.AdmitTurn
	options.Defaults.Thread.ModelProvider = protocolv2.Value(liveSmokeModelProvider)
	options.Defaults.AdmitTurn = func(start protocolv2.ThreadStartResponse, pending protocolv2.TurnStartParams) error {
		if err := inner(start, pending); err != nil {
			return err
		}
		return admitLiveSmokeProvider(start)
	}
	return options
}

func admitLiveSmokeProvider(start protocolv2.ThreadStartResponse) error {
	if start.ModelProvider != liveSmokeModelProvider {
		return fmt.Errorf("%w: model provider is %q, want %s", errApplicationAdmission, start.ModelProvider, liveSmokeModelProvider)
	}
	return nil
}

func TestLiveSmokeApplicationOptionsPinIsolatedProvider(t *testing.T) {
	options := liveSmokeApplicationOptions(nil)
	if options.Defaults.Thread.ModelProvider == nil || options.Defaults.Thread.ModelProvider.Value == nil || *options.Defaults.Thread.ModelProvider.Value != liveSmokeModelProvider {
		t.Fatalf("Thread.ModelProvider = %#v, want %q", options.Defaults.Thread.ModelProvider, liveSmokeModelProvider)
	}
}

func TestAdmitLiveSmokeProviderRejectsDefaultChatGPTProvider(t *testing.T) {
	err := admitLiveSmokeProvider(protocolv2.ThreadStartResponse{ModelProvider: "openai"})
	if !errors.Is(err, errApplicationAdmission) {
		t.Fatalf("error = %v, want %v", err, errApplicationAdmission)
	}
}

func TestAdmitLiveSmokeProviderAcceptsIsolatedProxy(t *testing.T) {
	if err := admitLiveSmokeProvider(protocolv2.ThreadStartResponse{ModelProvider: liveSmokeModelProvider}); err != nil {
		t.Fatal(err)
	}
}
