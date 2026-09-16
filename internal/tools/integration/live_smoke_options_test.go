package integration

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
)

func liveSmokeProvider() (string, error) {
	provider := strings.TrimSpace(os.Getenv("LLMGO_LIVE_CODEX_PROVIDER"))
	if provider == "" {
		return "", errors.New("LLMGO_LIVE_CODEX_PROVIDER is required")
	}
	return provider, nil
}

func liveSmokeApplicationOptions(runner codexcaller.ThreadRunner, provider string) codexcaller.Options {
	options := readOnlyApplicationOptions(runner)
	inner := options.Defaults.AdmitTurn
	options.Defaults.Thread.ModelProvider = protocolv2.Value(provider)
	options.Defaults.AdmitTurn = func(start protocolv2.ThreadStartResponse, pending protocolv2.TurnStartParams) error {
		if err := inner(start, pending); err != nil {
			return err
		}
		return admitLiveSmokeProvider(start, provider)
	}
	return options
}

func admitLiveSmokeProvider(start protocolv2.ThreadStartResponse, provider string) error {
	if start.ModelProvider != provider {
		return fmt.Errorf("%w: model provider is %q, want %s", errApplicationAdmission, start.ModelProvider, provider)
	}
	return nil
}

func TestLiveSmokeProviderReadsEnv(t *testing.T) {
	t.Setenv("LLMGO_LIVE_CODEX_PROVIDER", "from-env")
	got, err := liveSmokeProvider()
	if err != nil || got != "from-env" {
		t.Fatalf("liveSmokeProvider() = %q, %v, want from-env", got, err)
	}
}

func TestLiveSmokeProviderRequired(t *testing.T) {
	t.Setenv("LLMGO_LIVE_CODEX_PROVIDER", "")
	if _, err := liveSmokeProvider(); err == nil {
		t.Fatal("liveSmokeProvider() error = nil, want required")
	}
}

func TestLiveSmokeApplicationOptionsPinIsolatedProvider(t *testing.T) {
	const provider = "isolated-provider"
	options := liveSmokeApplicationOptions(nil, provider)
	if options.Defaults.Thread.ModelProvider == nil || options.Defaults.Thread.ModelProvider.Value == nil || *options.Defaults.Thread.ModelProvider.Value != provider {
		t.Fatalf("Thread.ModelProvider = %#v, want %q", options.Defaults.Thread.ModelProvider, provider)
	}
}

func TestAdmitLiveSmokeProviderRejectsDefaultChatGPTProvider(t *testing.T) {
	err := admitLiveSmokeProvider(protocolv2.ThreadStartResponse{ModelProvider: "openai"}, "isolated-provider")
	if !errors.Is(err, errApplicationAdmission) {
		t.Fatalf("error = %v, want %v", err, errApplicationAdmission)
	}
}

func TestAdmitLiveSmokeProviderAcceptsIsolatedProxy(t *testing.T) {
	if err := admitLiveSmokeProvider(protocolv2.ThreadStartResponse{ModelProvider: "isolated-provider"}, "isolated-provider"); err != nil {
		t.Fatal(err)
	}
}
