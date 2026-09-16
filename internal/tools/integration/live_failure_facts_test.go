package integration

import (
	"errors"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

func TestLiveFailureFactsReportNativeTurnError(t *testing.T) {
	turn := protocolv2.Turn{
		ID:     "turn-1",
		Status: protocolv2.TurnStatusFailed,
		Error: protocolv2.Value(protocolv2.TurnError{
			Message: "upstream rejected the responses request",
			CodexErrorInfo: protocolv2.Value(protocolv2.NewCodexErrorInfoHTTPConnectionFailed(protocolv2.CodexErrorInfoHTTPConnectionFailed{
				HTTPStatusCode: protocolv2.Value(uint16(502)),
			})),
		}),
	}
	err := &llmadapter.ValueError{
		Stage: llmadapter.ValueStageCall,
		Err:   &codexsdk.TurnError{ThreadID: "thread-1", Turn: turn, Err: codexsdk.ErrTurnFailed},
	}
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{
		GeneratedBaseline: codexsdk.GeneratedBaselineProvenance{
			SourceRefName: "rust-v0.154.0",
			SourceCommit:  strings.Repeat("a", 40),
		},
		RuntimeAppServer: codexsdk.RuntimeAppServerObservation{
			Observed:  true,
			UserAgent: "codex-cli 0.154.0",
		},
		Compatibility: codexsdk.RuntimeCompatibility{Kind: codexsdk.RuntimeCompatibilityUnknown},
	}, "codex-cli 0.154.0", "0.153.4", err, protocolv2.ThreadStartResponse{})

	want := []string{
		"live_failure.stage=terminal-turn",
		"live_failure.codex_cli_version=codex-cli 0.154.0",
		"live_failure.responses_proxy_version=0.153.4",
		"live_failure.generated_baseline.ref=rust-v0.154.0",
		"live_failure.generated_baseline.commit=" + strings.Repeat("a", 40),
		"live_failure.runtime_app_server.user_agent=codex-cli 0.154.0",
		"live_failure.runtime_compatibility=unknown",
		"live_failure.thread_id=thread-1",
		"live_failure.turn_id=turn-1",
		"live_failure.turn_status=failed",
		"live_failure.native_turn_error.message=upstream rejected the responses request",
		"live_failure.native_turn_error.codex_error_info=httpConnectionFailed",
		"live_failure.native_turn_error.http_status=502",
	}
	got := strings.Join(facts, "\n")
	for _, fact := range want {
		if !strings.Contains(got, fact) {
			t.Fatalf("missing %q in:\n%s", fact, got)
		}
	}
	if strings.Contains(got, "additionalDetails") {
		t.Fatal("unsafe additionalDetails leaked into live facts")
	}
}

func TestLiveFailureFactsReportTurnStartProtocolError(t *testing.T) {
	err := &codexsdk.ProtocolError{Method: protocolv2.MethodTurnStart, Code: -32602, Err: errors.New("invalid params")}
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", "", err, protocolv2.ThreadStartResponse{})
	got := strings.Join(facts, "\n")
	for _, fact := range []string{
		"live_failure.stage=turn-start",
		"live_failure.protocol.method=" + protocolv2.MethodTurnStart,
		"live_failure.protocol.code=-32602",
	} {
		if !strings.Contains(got, fact) {
			t.Fatalf("missing %q in:\n%s", fact, got)
		}
	}
}

func TestLiveFailureFactsReportObservedThreadProvider(t *testing.T) {
	err := &codexsdk.TurnError{
		ThreadID: "thread-1",
		Turn: protocolv2.Turn{
			ID:     "turn-1",
			Status: protocolv2.TurnStatusFailed,
			Error: protocolv2.Value(protocolv2.TurnError{
				Message:        "You've hit your usage limit.",
				CodexErrorInfo: protocolv2.Value(protocolv2.NewCodexErrorInfoUsageLimitExceeded()),
			}),
		},
		Err: codexsdk.ErrTurnFailed,
	}
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", "", err, protocolv2.ThreadStartResponse{
		Model:         "gpt-5.6-luna",
		ModelProvider: "openai",
		Thread:        protocolv2.Thread{ID: "thread-1"},
	})
	got := strings.Join(facts, "\n")
	for _, fact := range []string{
		"live_failure.stage=terminal-turn",
		"live_failure.native_turn_error.codex_error_info=usageLimitExceeded",
		"live_failure.thread.model=gpt-5.6-luna",
		"live_failure.thread.model_provider=openai",
	} {
		if !strings.Contains(got, fact) {
			t.Fatalf("missing %q in:\n%s", fact, got)
		}
	}
}

func TestLiveFailureFactsReportAdmissionStage(t *testing.T) {
	err := &codexsdk.TurnAdmissionError{Err: errApplicationAdmission}
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", "", err, protocolv2.ThreadStartResponse{
		ModelProvider: "openai",
		Thread:        protocolv2.Thread{ID: "thread-1"},
	})
	got := strings.Join(facts, "\n")
	for _, fact := range []string{
		"live_failure.stage=admission",
		"live_failure.thread.model_provider=openai",
	} {
		if !strings.Contains(got, fact) {
			t.Fatalf("missing %q in:\n%s", fact, got)
		}
	}
}

func TestLiveFailureFactsOmitAbsentNativeTurnError(t *testing.T) {
	err := &codexsdk.TurnError{
		ThreadID: "thread-1",
		Turn:     protocolv2.Turn{ID: "turn-1", Status: protocolv2.TurnStatusFailed},
		Err:      codexsdk.ErrTurnFailed,
	}
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", "", err, protocolv2.ThreadStartResponse{})
	got := strings.Join(facts, "\n")
	if !strings.Contains(got, "live_failure.native_turn_error=absent") {
		t.Fatalf("missing absent native error in:\n%s", got)
	}
}
