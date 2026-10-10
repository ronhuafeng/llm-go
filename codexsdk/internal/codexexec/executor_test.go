package codexexec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testSession = "019a0000-1111-7222-8333-0123456789ab"

func TestEvents(t *testing.T) {
	started := `{"type":"thread.started","thread_id":"` + testSession + `"}` + "\n"
	completed := `{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}` + "\n"
	for _, tc := range []struct {
		name, events, expected, diagnostic string
		ok                                 bool
	}{
		{"success", started + completed, "", "completed", true},
		{"exact resume", started + completed, testSession, "completed", true},
		{"missing session", completed, "", "invalid_event_stream", false},
		{"empty stream", "", "", "missing_session", false},
		{"incomplete", started, "", "execution_failed", false},
		{"failed turn", started + `{"type":"turn.failed","error":{"message":"failed"}}`, "", "execution_failed", false},
		{"404 blocked", started + `{"type":"turn.failed","error":{"message":"HTTP 404 key=TOPSECRET"}}`, "", "endpoint_not_found", false},
		{"auth", started + `{"type":"error","message":"401 TOPSECRET"}`, "", "authentication_failed", false},
		{"provider", started + `{"type":"error","message":"503 TOPSECRET"}`, "", "provider_failed", false},
		{"wrong session", started + completed, "019a0000-1111-7222-8333-0123456789ac", "session_mismatch", false},
		{"failure dominates", started + `{"type":"turn.failed","error":{"message":"failed"}}` + "\n" + completed, "", "execution_failed", false},
		{"invalid JSON", "TOPSECRET", "", "invalid_event_stream", false},
		{"duplicate completion", started + completed + completed, "", "invalid_event_stream", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseEvents(strings.NewReader(tc.events), tc.expected)
			if (err == nil) != tc.ok || result.Diagnostic != tc.diagnostic {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if strings.Contains(result.Diagnostic, "TOPSECRET") {
				t.Fatal("secret leaked")
			}
		})
	}
}
func fakeExecutor(t *testing.T, body string) *Executor {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires a POSIX host; required repair runner is Linux")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'codex-cli 0.160.0'; exit 0; fi\n" + body
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	e, err := New(context.Background(), Config{Workspace: workspace, StateDir: dir, Binary: binary, Version: "0.160.0", BaseURL: "https://example.invalid/v1", APIKey: " TOPSECRET ", Model: "model", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}
func TestRunExactResumeAndIsolation(t *testing.T) {
	t.Setenv("GH_TOKEN", "MUST_NOT_REACH_MODEL")
	t.Setenv("OPENAI_API_KEY", "AMBIENT_MUST_NOT_REACH_MODEL")
	e := fakeExecutor(t, `test -z "$GH_TOKEN" && test -z "$OPENAI_API_KEY" || exit 9
 test "$AZURE_OPENAI_API_KEY" = TOPSECRET || exit 8
 printf '%s\n' "$@" > args.txt
 cat >/dev/null
 printf '%s\n' '{"type":"thread.started","thread_id":"`+testSession+`"}' '{"type":"turn.completed"}'
`)
	result, err := e.Run(context.Background(), "prompt", testSession)
	if err != nil || result.Status != "completed" || result.SessionID != testSession {
		t.Fatalf("%+v %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(e.config.Workspace, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "resume\n"+testSession+"\n-\n") || strings.Contains(string(data), "--last") {
		t.Fatalf("args=%s", data)
	}
	config, err := os.ReadFile(filepath.Join(e.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "TOPSECRET") || !strings.Contains(string(config), "workspace-write") {
		t.Fatal("invalid native config")
	}
}
func TestCancellation(t *testing.T) {
	e := fakeExecutor(t, "sleep 60 &\nwait\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := e.Run(ctx, "prompt", "")
	if err == nil || result.Status != "cancelled" || result.Diagnostic != "cancelled" {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestBlocked404(t *testing.T) {
	e := fakeExecutor(t, "echo 'HTTP 404 TOPSECRET' >&2\nexit 1\n")
	result, err := e.Run(context.Background(), "prompt", "")
	if err == nil || result.Status != "blocked" || result.Diagnostic != "endpoint_not_found" {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestPreflightRequiresActualWrite(t *testing.T) {
	events := `printf '%s\n' '{"type":"thread.started","thread_id":"` + testSession + `"}' '{"type":"turn.completed"}'` + "\n"
	e := fakeExecutor(t, "cat >/dev/null\n"+events)
	if err := e.Preflight(context.Background()); err == nil || err.Error() != "preflight: sandbox_write_unverified" {
		t.Fatalf("%v", err)
	}
	e = fakeExecutor(t, `prompt="$(cat)"
 token="$(printf '%s' "$prompt" | sed -n 's/.*exactly \([0-9a-f]*\) (no newline).*/\1/p')"
 printf '%s' "$token" > ".codex-preflight-$token"
`+events)
	if err := e.Preflight(context.Background()); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(e.config.Workspace, ".codex-preflight-*"))
	if len(files) != 0 {
		t.Fatal("preflight file retained")
	}
}
func TestInvalidInput(t *testing.T) {
	for _, endpoint := range []string{"https://user:secret@example.invalid/v1", "https://example.invalid/v1?key=secret", "https://example.invalid/v1#secret", "https://example.invalid/v1/responses"} {
		_, err := New(context.Background(), Config{BaseURL: endpoint, APIKey: "key"})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe validation: %v", err)
		}
	}
	e := fakeExecutor(t, "")
	if _, err := e.Run(context.Background(), "prompt", "--last"); err == nil {
		t.Fatal("accepted non UUID")
	}
}

func TestDiagnosticHTTPStatusRequiresStatusContext(t *testing.T) {
	for _, message := range []string{"thread 00404abc failed", "write /tmp/process503/file: denied"} {
		if classify(message) != "execution_failed" {
			t.Fatal("invented HTTP attribution", message)
		}
	}
}
func TestPreflightToolDiagnosticDoesNotRejectRecoveredTurn(t *testing.T) {
	input := `{"type":"thread.started","thread_id":"` + testSession + `"}
{"type":"item.completed","item":{"type":"command_execution","exit_code":1,"aggregated_output":"bwrap: operation not permitted PRIVATE"}}
{"type":"turn.completed"}
`
	result, err := parseEvents(strings.NewReader(input), "")
	if err != nil || result.Status != "completed" || result.ToolDiagnostic != "sandbox_unavailable" {
		t.Fatalf("%+v %v", result, err)
	}
	if strings.Contains(result.ToolDiagnostic, "PRIVATE") {
		t.Fatal("raw output leaked")
	}
}

func TestTerminalToolFailureWithoutExitCodeIsDiagnosed(t *testing.T) {
	for _, tc := range []struct{ item, want string }{
		{`{"type":"command_execution","status":"failed","exit_code":null,"aggregated_output":"bwrap: Operation not permitted PRIVATE"}`, "sandbox_unavailable"},
		{`{"type":"command_execution","status":"declined","exit_code":null}`, "command_failed"},
		{`{"type":"error","message":"sandbox failed PRIVATE"}`, "sandbox_unavailable"},
		{`{"type":"error","message":"generic PRIVATE"}`, "runtime_warning"},
	} {
		events := `{"type":"thread.started","thread_id":"12345678-1234-1234-1234-123456789abc"}` + "\n" + `{"type":"item.completed","item":` + tc.item + `}` + "\n" + `{"type":"turn.completed"}`
		result, err := parseEvents(strings.NewReader(events), "")
		if err != nil || result.Status != "completed" || result.ToolDiagnostic != tc.want {
			t.Fatalf("result=%+v error=%v want=%s", result, err, tc.want)
		}
	}
}
