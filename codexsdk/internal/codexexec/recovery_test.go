package codexexec

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The official exec projection omits ErrorNotification.willRetry. Recovery is
// established by the event lifecycle and process exit, never by error wording.
func TestRunRecoveredErrorVerdicts(t *testing.T) {
	thread := `{"type":"thread.started","thread_id":"` + testSession + `"}` + "\n"
	turn := `{"type":"turn.started"}` + "\n"
	errorEvent := `{"type":"error","message":"stream disconnected before completion: PRIVATE"}` + "\n"
	answer := `{"type":"item.completed","item":{"type":"agent_message","id":"answer","text":"done"}}` + "\n"
	completed := `{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0}}` + "\n"
	failed := `{"type":"turn.failed","error":{"message":"HTTP 503 PRIVATE"}}` + "\n"
	for _, tc := range []struct {
		name, events, expected, diagnostic, errorDiagnostic string
		exitCode                                            int
	}{
		{"no turn start control", thread + answer + completed, "", "completed", "", 0},
		{"no error control", thread + turn + answer + completed, "", "completed", "", 0},
		{"recovered", thread + turn + errorEvent + answer + completed, "", "completed", "provider_failed", 0},
		{"recovered exact resume", thread + turn + errorEvent + answer + completed, testSession, "completed", "provider_failed", 0},
		{"recovery ignores prose", thread + turn + `{"type":"error","message":"unrecognized PRIVATE"}` + "\n" + answer + completed, "", "completed", "execution_failed", 0},
		{"repeated provisional errors", thread + turn + errorEvent + errorEvent + answer + completed, "", "completed", "provider_failed", 0},
		{"missing completion", thread + turn + errorEvent + answer, "", "provider_failed", "provider_failed", 0},
		{"failed terminal", thread + turn + errorEvent + failed, "", "provider_failed", "provider_failed", 0},
		{"failure before completion", thread + turn + errorEvent + failed + completed, "", "provider_failed", "provider_failed", 0},
		{"failure after completion", thread + turn + errorEvent + completed + failed, "", "provider_failed", "provider_failed", 0},
		{"nonzero exit", thread + turn + errorEvent + answer + completed, "", "execution_failed", "provider_failed", 17},
		{"missing session", turn + errorEvent + completed, "", "event_sequence_invalid", "provider_failed", 0},
		{"mismatched session", thread + turn + errorEvent + completed, "019a0000-1111-7222-8333-0123456789ac", "session_mismatch", "provider_failed", 0},
		{"recovered without turn start", thread + errorEvent + completed, "", "completed", "provider_failed", 0},
		{"turn before session", turn + thread + errorEvent + completed, "", "event_sequence_invalid", "provider_failed", 0},
		{"duplicate turn start", thread + turn + errorEvent + turn + completed, "", "event_sequence_invalid", "provider_failed", 0},
		{"error after completion", thread + turn + completed + errorEvent, "", "event_sequence_invalid", "provider_failed", 0},
		{"duplicate completion", thread + turn + errorEvent + completed + completed, "", "event_sequence_invalid", "provider_failed", 0},
		{"missing error message", thread + turn + `{"type":"error"}` + "\n" + completed, "", "event_decode_failed", "", 0},
		{"null error message", thread + turn + `{"type":"error","message":null}` + "\n" + completed, "", "event_decode_failed", "", 0},
		{"wrong error message type", thread + turn + `{"type":"error","message":42}` + "\n" + completed, "", "event_decode_failed", "", 0},
		{"corrupt before recovery", thread + turn + "PRIVATE malformed\n" + errorEvent + completed, "", "event_decode_failed", "provider_failed", 0},
		{"corrupt after recovery", thread + turn + errorEvent + completed + "PRIVATE malformed\n", "", "event_decode_failed", "provider_failed", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := fakeExecutor(t, "cat fixture.jsonl\nexit "+strconv.Itoa(tc.exitCode)+"\n")
			if err := os.WriteFile(filepath.Join(e.config.Workspace, "fixture.jsonl"), []byte(tc.events), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := e.Run(context.Background(), "bounded fixture", tc.expected)
			if result.Diagnostic != tc.diagnostic || result.ErrorDiagnostic != tc.errorDiagnostic || (err == nil) != (tc.diagnostic == "completed") {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if err == nil {
				if result.Status != "completed" || result.SessionID != testSession {
					t.Fatalf("result=%+v", result)
				}
			} else if result.Status == "completed" || err.Error() != tc.diagnostic+" process_exit="+strconv.Itoa(tc.exitCode) {
				t.Fatalf("status=%s error=%v", result.Status, err)
			}
			t.Logf("error_observed=%t diagnostic=%s terminal_completed=%t process_exit=%d result=%s", tc.errorDiagnostic != "", result.ErrorDiagnostic, strings.Contains(tc.events, completed), tc.exitCode, result.Status)
		})
	}
}

func TestProvisionalErrorCannotHideReaderFailure(t *testing.T) {
	start := `{"type":"thread.started","thread_id":"` + testSession + `"}` + "\n" + `{"type":"turn.started"}` + "\n"
	errorEvent := `{"type":"error","message":"HTTP 503 PRIVATE"}` + "\n"
	completed := `{"type":"turn.completed"}` + "\n"
	for _, tc := range []struct {
		name, diagnostic string
		input            io.Reader
	}{
		{"oversized", "event_too_large", strings.NewReader(start + errorEvent + commandEvent(t, 1024*1024) + completed)},
		{"unreadable after completed", "event_read_failed", io.MultiReader(strings.NewReader(start+errorEvent+completed), privateReadError{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseEvents(tc.input, "")
			if err == nil || result.Status == "completed" || result.Diagnostic != tc.diagnostic || result.ErrorDiagnostic != "provider_failed" || err.Error() != tc.diagnostic {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRecoveredErrorDoesNotOverrideCancellation(t *testing.T) {
	e := fakeExecutor(t, `printf '%s\n' '{"type":"thread.started","thread_id":"`+testSession+`"}' '{"type":"turn.started"}' '{"type":"error","message":"HTTP 503 PRIVATE"}' '{"type":"turn.completed"}'
sleep 60 &
wait
`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := e.Run(ctx, "bounded fixture", "")
	if err == nil || result.Status != "cancelled" || result.Diagnostic != "cancelled" || err.Error() != "cancelled" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
