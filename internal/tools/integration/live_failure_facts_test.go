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
	}, "codex-cli 0.154.0", err, protocolv2.ThreadStartResponse{})

	want := []string{
		"live_failure.stage=terminal-turn",
		"live_failure.codex_cli_version=codex-cli 0.154.0",
		"live_failure.generated_baseline.ref=rust-v0.154.0",
		"live_failure.generated_baseline.commit=" + strings.Repeat("a", 40),
		"live_failure.runtime_app_server.observed=true",
		"live_failure.runtime_compatibility=unknown",
		"live_failure.turn_status=failed",
		"live_failure.native_turn_error.codex_error_info=httpConnectionFailed",
		"live_failure.native_turn_error.http_status=502",
	}
	got := strings.Join(facts, "\n")
	for _, fact := range want {
		if !strings.Contains(got, fact) {
			t.Fatalf("missing %q in:\n%s", fact, got)
		}
	}
	if strings.Contains(got, "thread-1") || strings.Contains(got, "turn-1") || strings.Contains(got, "additionalDetails") {
		t.Fatal("unsafe additionalDetails leaked into live facts")
	}
	if strings.Contains(got, "native_turn_error.message=") || strings.Contains(got, "upstream rejected the responses request") {
		t.Fatal("raw native turn error message leaked into live facts")
	}
}

func TestLiveFailureFactsReportTurnStartProtocolError(t *testing.T) {
	err := &codexsdk.ProtocolError{Method: protocolv2.MethodTurnStart, Code: -32602, Err: errors.New("invalid params")}
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", err, protocolv2.ThreadStartResponse{})
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
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", err, protocolv2.ThreadStartResponse{
		Model:         "gpt-5.6-luna",
		ModelProvider: "openai",
		Thread:        protocolv2.Thread{ID: "thread-1"},
	})
	got := strings.Join(facts, "\n")
	for _, fact := range []string{
		"live_failure.stage=terminal-turn",
		"live_failure.native_turn_error.codex_error_info=usageLimitExceeded",
		"live_failure.thread.model_matches_fixture=false",
		"live_failure.thread.provider_matches_fixture=false",
	} {
		if !strings.Contains(got, fact) {
			t.Fatalf("missing %q in:\n%s", fact, got)
		}
	}
	if strings.Contains(got, "You've hit your usage limit") || strings.Contains(got, "native_turn_error.message=") {
		t.Fatal("raw native turn error message leaked into live facts")
	}
}

func TestLiveFailureFactsReportAdmissionStage(t *testing.T) {
	err := &codexsdk.TurnAdmissionError{Err: errApplicationAdmission}
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", err, protocolv2.ThreadStartResponse{
		ModelProvider: "openai",
		Thread:        protocolv2.Thread{ID: "thread-1"},
	})
	got := strings.Join(facts, "\n")
	for _, fact := range []string{
		"live_failure.stage=admission",
		"live_failure.thread.provider_matches_fixture=false",
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
	facts := liveFailureFacts(codexsdk.ConnectionProvenance{}, "", err, protocolv2.ThreadStartResponse{})
	got := strings.Join(facts, "\n")
	if !strings.Contains(got, "live_failure.native_turn_error=absent") {
		t.Fatalf("missing absent native error in:\n%s", got)
	}
}

func TestNativeMessageSignatureIsAnObservationNotAttribution(t *testing.T) {
	for _, tc := range []struct{ name, message, want string }{
		{"content filter", "stream disconnected before completion: Incomplete response returned, reason: content_filter", "content_filter_message"},
		{"request timeout", "request timed out", "request_timeout_message"},
		{"child timeout", "timeout waiting for child process to exit", "child_timeout_message"},
		{"child stdio", "spawn failed: child stdout/stderr not captured", "child_stdio_message"},
		{"sandbox executable", "codex-linux-sandbox was required but not provided", "sandbox_executable_message"},
		{"stream payload", "stream disconnected before completion: PRIVATE https://secret.invalid/?key=PRIVATE", "stream_disconnected_prefix"},
		{"fatal payload", "Fatal error: PRIVATE", "fatal_prefix"},
		{"missing env payload", "Missing environment variable: `PRIVATE`. PRIVATE", "missing_environment_prefix"},
		{"unknown provider text", "HTTP 401 PRIVATE model unavailable", "unknown"},
		{"embedded prefix", "PRIVATE stream disconnected before completion: PRIVATE", "unknown"},
		{"multiline mixed", "Fatal error: PRIVATE\nrequest timed out", "unknown"},
		{"suffix on literal", "request timed out PRIVATE", "unknown"},
		{"leading whitespace", " request timed out", "unknown"},
		{"case folded", "REQUEST TIMED OUT", "unknown"},
		{"control character", "Fatal error: \x1b[31mPRIVATE", "unknown"},
		{"oversized", "Fatal error: " + strings.Repeat("PRIVATE", 3000), "unknown"},
		{"empty prefix", "Fatal error: ", "unknown"},
		{"empty", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeMessageSignature(tc.message); got != tc.want {
				t.Fatalf("signature=%q, want=%q", got, tc.want)
			}
			for _, withInfo := range []bool{true, false} {
				native := protocolv2.TurnError{Message: tc.message}
				if withInfo {
					native.CodexErrorInfo = protocolv2.Value(protocolv2.NewCodexErrorInfoOther())
				}
				facts := nativeTurnErrorFacts(protocolv2.Turn{Error: protocolv2.Value(native)})
				got := strings.Join(facts, "\n")
				if !strings.Contains(got, "message_signature="+tc.want) {
					t.Fatal("missing fixed signature")
				}
				for _, private := range []string{"PRIVATE", "secret.invalid", "HTTP 401", "key="} {
					if strings.Contains(got, private) {
						t.Fatal("native error payload leaked")
					}
				}
			}
		})
	}
}
