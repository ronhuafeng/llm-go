package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

func reportLiveFailure(t testingTB, client *codexsdk.Client, start protocolv2.ThreadStartResponse, err error) {
	t.Helper()
	var provenance codexsdk.ConnectionProvenance
	if client != nil {
		provenance = client.Provenance()
	}
	for _, fact := range liveFailureFacts(provenance, liveCLIVersion(), os.Getenv("LLMGO_LIVE_CODEX_PROXY_VERSION"), err, start) {
		t.Log(fact)
	}
}

type testingTB interface {
	Helper()
	Log(args ...any)
}

func liveFailureFacts(provenance codexsdk.ConnectionProvenance, cliVersion, proxyVersion string, err error, start protocolv2.ThreadStartResponse) []string {
	facts := []string{
		"live_failure.stage=" + liveFailureStage(err),
	}
	if cliVersion != "" {
		facts = append(facts, "live_failure.codex_cli_version="+cliVersion)
	}
	if proxyVersion != "" {
		facts = append(facts, "live_failure.responses_proxy_version="+proxyVersion)
	}
	baseline := provenance.GeneratedBaseline
	if baseline.SourceRefName != "" || baseline.SourceCommit != "" {
		facts = append(facts,
			"live_failure.generated_baseline.ref="+baseline.SourceRefName,
			"live_failure.generated_baseline.commit="+baseline.SourceCommit,
		)
	}
	if provenance.RuntimeAppServer.Observed {
		facts = append(facts, "live_failure.runtime_app_server.user_agent="+provenance.RuntimeAppServer.UserAgent)
	} else {
		facts = append(facts, "live_failure.runtime_app_server.observed=false")
	}
	kind := provenance.Compatibility.Kind
	if kind == "" {
		kind = codexsdk.RuntimeCompatibilityUnknown
	}
	facts = append(facts, "live_failure.runtime_compatibility="+string(kind))

	var proto *codexsdk.ProtocolError
	if errors.As(err, &proto) {
		facts = append(facts,
			"live_failure.protocol.method="+proto.Method,
			fmt.Sprintf("live_failure.protocol.code=%d", proto.Code),
		)
	}

	var turnErr *codexsdk.TurnError
	if errors.As(err, &turnErr) {
		facts = append(facts,
			"live_failure.thread_id="+turnErr.ThreadID,
			"live_failure.turn_id="+turnErr.Turn.ID,
			"live_failure.turn_status="+string(turnErr.Turn.Status),
		)
		facts = append(facts, nativeTurnErrorFacts(turnErr.Turn)...)
	}
	facts = append(facts, observedThreadStartFacts(start)...)
	return facts
}

func liveFailureStage(err error) string {
	var proto *codexsdk.ProtocolError
	if errors.As(err, &proto) {
		switch proto.Method {
		case protocolv2.MethodThreadStart:
			return "thread-start"
		case protocolv2.MethodTurnStart:
			return "turn-start"
		default:
			if proto.Method != "" {
				return "protocol-request"
			}
		}
	}
	if errors.Is(err, codexsdk.ErrTurnAdmissionRejected) {
		return "admission"
	}
	var turnErr *codexsdk.TurnError
	if errors.As(err, &turnErr) {
		return "terminal-turn"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var valueErr *llmadapter.ValueError
	if errors.As(err, &valueErr) && valueErr.Stage == llmadapter.ValueStageRequest {
		return "startup"
	}
	return "unknown"
}

func observedThreadStartFacts(start protocolv2.ThreadStartResponse) []string {
	var facts []string
	if start.Model != "" {
		facts = append(facts, "live_failure.thread.model="+start.Model)
	}
	if start.ModelProvider != "" {
		facts = append(facts, "live_failure.thread.model_provider="+start.ModelProvider)
	} else if start.Thread.ID != "" {
		facts = append(facts, "live_failure.thread.model_provider=absent")
	}
	return facts
}

func nativeTurnErrorFacts(turn protocolv2.Turn) []string {
	if turn.Error == nil || turn.Error.Value == nil {
		return []string{"live_failure.native_turn_error=absent"}
	}
	native := *turn.Error.Value
	facts := []string{"live_failure.native_turn_error.message=" + native.Message}
	if native.CodexErrorInfo == nil || native.CodexErrorInfo.Value == nil {
		return append(facts, "live_failure.native_turn_error.codex_error_info=absent")
	}
	info := *native.CodexErrorInfo.Value
	facts = append(facts, "live_failure.native_turn_error.codex_error_info="+string(info.Kind()))
	if status, ok := httpStatusFromCodexErrorInfo(info); ok {
		facts = append(facts, fmt.Sprintf("live_failure.native_turn_error.http_status=%d", status))
	}
	return facts
}

func httpStatusFromCodexErrorInfo(info protocolv2.CodexErrorInfo) (uint16, bool) {
	if v, ok := info.AsHTTPConnectionFailed(); ok {
		return nullableUint16(v.HTTPStatusCode)
	}
	if v, ok := info.AsResponseStreamConnectionFailed(); ok {
		return nullableUint16(v.HTTPStatusCode)
	}
	if v, ok := info.AsResponseStreamDisconnected(); ok {
		return nullableUint16(v.HTTPStatusCode)
	}
	if v, ok := info.AsResponseTooManyFailedAttempts(); ok {
		return nullableUint16(v.HTTPStatusCode)
	}
	return 0, false
}

func nullableUint16(value *protocolv2.Nullable[uint16]) (uint16, bool) {
	if value == nil || value.Value == nil {
		return 0, false
	}
	return *value.Value, true
}

func liveCLIVersion() string {
	out, err := exec.Command("codex", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
