package codexexec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func goalFixture(t *testing.T, scenario string) *Executor {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	launcherDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(launcherDir, "bwrap"), []byte("#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", launcherDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := fakeExecutor(t, "exec "+strconv.Quote(binary)+" -test.run=^TestGoalAppServerChild$ -- goal-child \"$@\"\n")
	if err := os.Mkdir(filepath.Join(e.config.Workspace, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	e.config.GoalSandbox = &GoalSandboxConfig{ModuleCache: t.TempDir(), RuntimeDirs: []string{filepath.Dir(binary)}}
	if err := os.WriteFile(filepath.Join(e.config.Workspace, ".goal-fixture"), []byte(scenario), 0600); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRunGoalNativeLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		timeout      time.Duration
	}{
		{"multi-turn", "complete", 4 * time.Second},
		{"complete-before-turn", "complete", 4 * time.Second},
		{"blocked", "goal_blocked", 4 * time.Second},
		{"paused", "goal_paused", 4 * time.Second},
		{"usageLimited", "goal_usage_limited", 4 * time.Second},
		{"budgetLimited", "goal_budget_limited", 4 * time.Second},
		{"failed-turn", "goal_turn_failed", 4 * time.Second},
		{"interrupted-turn", "goal_turn_failed", 4 * time.Second},
		{"wrong-thread", "goal_identity_invalid", 4 * time.Second},
		{"wrong-goal-thread", "goal_identity_invalid", 4 * time.Second},
		{"wrong-objective", "goal_identity_invalid", 4 * time.Second},
		{"wrong-turn", "goal_turn_sequence_invalid", 4 * time.Second},
		{"missing-start", "goal_turn_sequence_invalid", 4 * time.Second},
		{"cleared", "goal_cleared", 4 * time.Second},
		{"disconnect", "goal_transport_failed", 4 * time.Second},
		{"server-request", "goal_server_request_rejected", 4 * time.Second},
		{"set-response-only", "cancelled", 4 * time.Second},
		{"get-response-only", "cancelled", 4 * time.Second},
		{"late-revocation", "goal_completion_invalid", 4 * time.Second},
		{"complete-no-turn", "cancelled", 4 * time.Second},
		{"cancel", "cancelled", 4 * time.Second},
		{"unsafe-sandbox", "goal_thread_rejected", 4 * time.Second},
		{"ephemeral", "goal_thread_rejected", 4 * time.Second},
		{"bad-set", "goal_set_invalid", 4 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := goalFixture(t, tc.name)
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			if tc.status == "cancelled" {
				go func() {
					ticker := time.NewTicker(5 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							if _, err := os.Stat(filepath.Join(e.config.Workspace, ".fixture-ready")); err == nil {
								cancel()
								return
							}
						}
					}
				}()
			}
			result, err := e.runGoal(ctx, "complete the bounded fixture objective", nil, 20*time.Millisecond)
			if result.Status != tc.status || (err == nil) != (tc.status == "complete") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if err != nil && (err.Error() != result.Status || strings.Contains(err.Error(), "SECRET")) {
				t.Fatalf("unsafe error: %v", err)
			}
			if tc.name == "complete-before-turn" {
				if _, err := os.Stat(filepath.Join(e.config.Workspace, ".turn-completed")); err != nil {
					t.Fatal("returned before the active turn completed")
				}
			}
			pidData, err := os.ReadFile(filepath.Join(e.config.Workspace, ".goal-pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, _ := strconv.Atoi(string(pidData))
			if pid <= 0 {
				t.Fatal("missing child PID")
			}
			process, _ := os.FindProcess(pid)
			if process.Signal(syscall.Signal(0)) == nil {
				t.Fatal("child was not reaped")
			}
			data, err := os.ReadFile(filepath.Join(e.config.Workspace, ".goal-requests"))
			if err != nil {
				t.Fatal(err)
			}
			methods := string(data)
			if strings.Count(methods, "thread/goal/set\n") > 1 || strings.Contains(methods, "turn/start") {
				t.Fatalf("runner scheduled turns: %s", methods)
			}
			if tc.name != "disconnect" && !strings.Contains(methods, "thread/archive\n") {
				t.Fatalf("owned thread not archived: %s", methods)
			}
		})
	}
}

func TestRunGoalIsolation(t *testing.T) {
	t.Setenv("GH_TOKEN", "REPOSITORY_SECRET")
	t.Setenv("OPENAI_API_KEY", "AMBIENT_SECRET")
	e := goalFixture(t, "isolation")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	result, err := e.runGoal(ctx, "complete the bounded fixture objective", nil, 20*time.Millisecond)
	if err != nil || result.Status != "complete" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

// TestGoalAppServerChild implements the actual SDK's JSON-RPC child-process
// boundary. It never launches Codex or calls a model/provider.
func TestGoalAppServerChild(t *testing.T) {
	child := false
	for _, arg := range os.Args {
		if arg == "goal-child" {
			child = true
		}
	}
	if !child {
		return
	}
	raw, err := os.ReadFile(".goal-fixture")
	if err != nil {
		os.Exit(71)
	}
	scenario := string(raw)
	_ = os.WriteFile(".goal-pid", []byte(strconv.Itoa(os.Getpid())), 0600)
	cwd, _ := os.Getwd()
	encode := json.NewEncoder(os.Stdout)
	send := func(v any) {
		if encode.Encode(v) != nil {
			os.Exit(72)
		}
	}
	notify := func(method string, params any) { send(map[string]any{"method": method, "params": params}) }
	goal := func(status protocolv2.ThreadGoalStatus) protocolv2.ThreadGoal {
		return protocolv2.ThreadGoal{ThreadID: testSession, Objective: "complete the bounded fixture objective", Status: status}
	}
	goalEvent := func(status protocolv2.ThreadGoalStatus) {
		notify("thread/goal/updated", protocolv2.ThreadGoalUpdatedNotification{ThreadID: testSession, Goal: goal(status)})
	}
	turn := func(id string, status protocolv2.TurnStatus) protocolv2.Turn {
		return protocolv2.Turn{ID: id, Status: status, Items: []protocolv2.ThreadItem{}}
	}
	startTurn := func(id string) {
		notify("turn/started", protocolv2.TurnStartedNotification{ThreadID: testSession, Turn: turn(id, protocolv2.TurnStatusInProgress)})
	}
	endTurn := func(id string, status protocolv2.TurnStatus) {
		notify("turn/completed", protocolv2.TurnCompletedNotification{ThreadID: testSession, Turn: turn(id, status)})
	}
	requests, _ := os.OpenFile(".goal-requests", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	defer requests.Close()
	if scenario == "isolation" {
		if os.Getenv("GH_TOKEN") != "" || os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("AZURE_OPENAI_API_KEY") != "TOPSECRET" || os.Getenv("CODEX_HOME") == "" || strings.HasPrefix(os.Getenv("CODEX_HOME"), cwd+string(os.PathSeparator)) {
			os.Exit(73)
		}
		args := strings.Join(os.Args, "\n")
		for _, want := range (&Executor{config: Config{Model: "model", Reasoning: "high", BaseURL: "https://example.invalid/v1"}}).runtimeOverrides() {
			if !strings.Contains(args, want) {
				os.Exit(74)
			}
		}
		if !strings.Contains(args, "features.goals=true") || !strings.HasSuffix(args, "app-server") {
			os.Exit(75)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(76)
		}
		fmt.Fprintln(requests, request.Method)
		respond := func(value any) { send(map[string]any{"id": request.ID, "result": value}) }
		if request.Method == "" {
			if strings.HasPrefix(scenario, "verify-") {
				_ = os.WriteFile(".tool-response", scanner.Bytes(), 0600)
				var response protocolv2.DynamicToolCallResponse
				if json.Unmarshal(request.Result, &response) == nil && response.Success {
					goalEvent(protocolv2.ThreadGoalStatusComplete)
					endTurn("one", protocolv2.TurnStatusCompleted)
				}
			}
			continue
		}
		switch request.Method {
		case "initialize":
			respond(protocolv2.InitializeResponse{UserAgent: "codex-cli 0.160.0", CodexHome: os.Getenv("CODEX_HOME"), PlatformFamily: "unix", PlatformOs: "linux"})
		case "initialized":
		case "thread/start":
			var params protocolv2.ThreadStartParams
			if json.Unmarshal(request.Params, &params) != nil || params.Ephemeral == nil || params.Ephemeral.Value == nil || *params.Ephemeral.Value || params.CWD == nil || params.CWD.Value == nil || *params.CWD.Value != cwd {
				os.Exit(77)
			}
			if strings.HasPrefix(scenario, "verify-") {
				if params.DynamicTools == nil || params.DynamicTools.Value == nil || len(*params.DynamicTools.Value) != 1 {
					os.Exit(81)
				}
				actual, _ := json.Marshal((*params.DynamicTools.Value)[0])
				expected, _ := json.Marshal(verificationTool())
				if string(actual) != string(expected) {
					os.Exit(82)
				}
			}
			no, yes := false, true
			roots := []string{}
			policy := protocolv2.NewSandboxPolicyWorkspaceWrite(protocolv2.SandboxPolicyWorkspaceWrite{NetworkAccess: &no, ExcludeSlashTmp: &yes, ExcludeTmpdirEnvVar: &yes, WritableRoots: &roots})
			if scenario == "unsafe-sandbox" {
				policy = protocolv2.NewSandboxPolicyDangerFullAccess()
			}
			respond(protocolv2.ThreadStartResponse{ApprovalPolicy: protocolv2.NewAskForApprovalNever(), ApprovalsReviewer: protocolv2.ApprovalsReviewerUser, CWD: cwd, Model: "model", ModelProvider: "llm-go-exec", Sandbox: policy,
				Thread: protocolv2.Thread{ID: testSession, SessionID: testSession, CWD: cwd, CliVersion: "0.160.0", ModelProvider: "llm-go-exec", Ephemeral: scenario == "ephemeral", Source: protocolv2.NewSessionSourceAppServer(), Status: protocolv2.NewThreadStatusIdle(), Turns: []protocolv2.Turn{}}})
		case "thread/goal/set":
			var params protocolv2.ThreadGoalSetParams
			if json.Unmarshal(request.Params, &params) != nil || params.ThreadID != testSession || params.Objective == nil || params.Objective.Value == nil || *params.Objective.Value != goal(protocolv2.ThreadGoalStatusActive).Objective {
				os.Exit(78)
			}
			g := goal(protocolv2.ThreadGoalStatusActive)
			if scenario == "bad-set" {
				g.ThreadID = "SECRET-wrong"
			}
			respond(protocolv2.ThreadGoalSetResponse{Goal: g})
			switch scenario {
			case "disconnect":
				fmt.Fprintln(os.Stderr, "SECRET private provider body")
				os.Exit(0)
			case "verify-ok", "verify-failedchecks", "verify-error", "verify-cancel", "verify-panic", "verify-args", "verify-namespace", "verify-thread", "verify-tool":
				startTurn("one")
				params := protocolv2.DynamicToolCallParams{Arguments: protocolv2.JSONObject(map[string]protocolv2.JSONValue{}), CallID: "tool-1", ThreadID: testSession, Tool: goalVerificationTool, TurnID: "one"}
				switch scenario {
				case "verify-args":
					params.Arguments = protocolv2.JSONObject(map[string]protocolv2.JSONValue{"command": protocolv2.JSONString("SECRET-untrusted-command")})
				case "verify-namespace":
					params.Namespace = protocolv2.Value("SECRET-untrusted-namespace")
				case "verify-thread":
					params.ThreadID = "SECRET-other-thread"
				case "verify-tool":
					params.Tool = "SECRET-other-tool"
				}
				send(map[string]any{"id": "verify-request", "method": "item/tool/call", "params": params})
			case "server-request":
				send(map[string]any{"id": "request-1", "method": "item/tool/call", "params": protocolv2.DynamicToolCallParams{Arguments: protocolv2.JSONObject(map[string]protocolv2.JSONValue{}), CallID: "tool-1", ThreadID: testSession, Tool: "arbitrary-host-command", TurnID: "one"}})
			case "set-response-only", "get-response-only", "bad-set":
			case "complete-no-turn":
				goalEvent(protocolv2.ThreadGoalStatusComplete)
			case "blocked", "paused", "usageLimited", "budgetLimited":
				goalEvent(protocolv2.ThreadGoalStatus(scenario))
			case "wrong-thread":
				notify("thread/goal/updated", protocolv2.ThreadGoalUpdatedNotification{ThreadID: "SECRET-wrong", Goal: g})
			case "wrong-goal-thread":
				g.ThreadID = "SECRET-wrong"
				notify("thread/goal/updated", protocolv2.ThreadGoalUpdatedNotification{ThreadID: testSession, Goal: g})
			case "wrong-objective":
				g.Objective = "SECRET-wrong"
				notify("thread/goal/updated", protocolv2.ThreadGoalUpdatedNotification{ThreadID: testSession, Goal: g})
			case "cleared":
				notify("thread/goal/cleared", protocolv2.ThreadGoalClearedNotification{ThreadID: testSession})
			case "missing-start":
				endTurn("one", protocolv2.TurnStatusCompleted)
			default:
				startTurn("one")
				switch scenario {
				case "cancel":
				case "failed-turn":
					endTurn("one", protocolv2.TurnStatusFailed)
				case "interrupted-turn":
					endTurn("one", protocolv2.TurnStatusInterrupted)
				case "wrong-turn":
					endTurn("SECRET-other", protocolv2.TurnStatusCompleted)
				case "complete-before-turn":
					goalEvent(protocolv2.ThreadGoalStatusComplete)
				default:
					endTurn("one", protocolv2.TurnStatusCompleted)
					startTurn("two")
					goalEvent(protocolv2.ThreadGoalStatusComplete)
					endTurn("two", protocolv2.TurnStatusCompleted)
				}
			}
			_ = os.WriteFile(".fixture-ready", []byte("ready"), 0600)
		case "thread/goal/get":
			g := goal(protocolv2.ThreadGoalStatusActive)
			if scenario == "get-response-only" {
				g.Status = protocolv2.ThreadGoalStatusComplete
			}
			respond(protocolv2.ThreadGoalGetResponse{Goal: protocolv2.Value(g)})
			if scenario == "complete-before-turn" {
				_ = os.WriteFile(".turn-completed", []byte("done"), 0600)
				endTurn("one", protocolv2.TurnStatusCompleted)
			}
		case "thread/archive":
			if scenario == "late-revocation" {
				goalEvent(protocolv2.ThreadGoalStatusActive)
			}
			respond(protocolv2.ThreadArchiveResponse{})
		default:
			os.Exit(79)
		}
	}
	os.Exit(0)
}

func TestGoalThreadAdmission(t *testing.T) {
	yes, no := true, false
	empty, outside := []string{}, []string{"/outside"}
	e := &Executor{config: Config{Workspace: "/workspace", Model: "model"}}
	base := protocolv2.ThreadStartResponse{ApprovalPolicy: protocolv2.NewAskForApprovalNever(), CWD: "/workspace", Model: "model", ModelProvider: "llm-go-exec", Thread: protocolv2.Thread{CWD: "/workspace", ModelProvider: "llm-go-exec"}}
	for _, tc := range []struct {
		name   string
		policy protocolv2.SandboxPolicyWorkspaceWrite
		want   bool
	}{
		{"explicit", protocolv2.SandboxPolicyWorkspaceWrite{NetworkAccess: &no, ExcludeSlashTmp: &yes, ExcludeTmpdirEnvVar: &yes, WritableRoots: &empty}, true},
		{"schema defaults", protocolv2.SandboxPolicyWorkspaceWrite{ExcludeSlashTmp: &yes, ExcludeTmpdirEnvVar: &yes}, true},
		{"network", protocolv2.SandboxPolicyWorkspaceWrite{NetworkAccess: &yes, ExcludeSlashTmp: &yes, ExcludeTmpdirEnvVar: &yes}, false},
		{"slash tmp", protocolv2.SandboxPolicyWorkspaceWrite{ExcludeTmpdirEnvVar: &yes}, false},
		{"environment tmp", protocolv2.SandboxPolicyWorkspaceWrite{ExcludeSlashTmp: &yes}, false},
		{"extra root", protocolv2.SandboxPolicyWorkspaceWrite{ExcludeSlashTmp: &yes, ExcludeTmpdirEnvVar: &yes, WritableRoots: &outside}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := base
			start.Sandbox = protocolv2.NewSandboxPolicyWorkspaceWrite(tc.policy)
			if e.goalThreadAdmitted(start) != tc.want {
				t.Fatal("unexpected admission")
			}
		})
	}
	base.Sandbox = protocolv2.NewSandboxPolicyWorkspaceWrite(protocolv2.SandboxPolicyWorkspaceWrite{ExcludeSlashTmp: &yes, ExcludeTmpdirEnvVar: &yes})
	for _, tc := range []struct {
		name   string
		change func(*protocolv2.ThreadStartResponse)
	}{
		{"old turns", func(s *protocolv2.ThreadStartResponse) { s.Thread.Turns = []protocolv2.Turn{{ID: "old"}} }},
		{"runtime root", func(s *protocolv2.ThreadStartResponse) { s.RuntimeWorkspaceRoots = &outside }},
		{"model", func(s *protocolv2.ThreadStartResponse) { s.Model = "other" }},
		{"provider", func(s *protocolv2.ThreadStartResponse) { s.ModelProvider = "other" }},
		{"cwd", func(s *protocolv2.ThreadStartResponse) { s.CWD = "/outside" }},
		{"thread cwd", func(s *protocolv2.ThreadStartResponse) { s.Thread.CWD = "/outside" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := base
			tc.change(&start)
			if e.goalThreadAdmitted(start) {
				t.Fatal("unsafe admission")
			}
		})
	}
}

func TestRunGoalVerificationBridge(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		calls        int32
	}{
		{"verify-ok", "complete", 1},
		{"verify-failedchecks", "complete", 1},
		{"verify-error", "goal_verification_failed", 1},
		{"verify-panic", "goal_verification_failed", 1},
		{"verify-cancel", "cancelled", 1},
		{"verify-args", "goal_server_request_rejected", 0},
		{"verify-namespace", "goal_server_request_rejected", 0},
		{"verify-thread", "goal_server_request_rejected", 0},
		{"verify-tool", "goal_server_request_rejected", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := goalFixture(t, tc.name)
			path := filepath.Join(t.TempDir(), "feedback.jsonl")
			if err := os.WriteFile(path, []byte("{\"finding\":\"complete test feedback\"}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var calls atomic.Int32
			cancelled := make(chan struct{})
			verify := func(callbackCtx context.Context) (string, error) {
				calls.Add(1)
				switch tc.name {
				case "verify-failedchecks":
					return path, errors.New("SECRET check output and provider URL")
				case "verify-error":
					return "", errors.New("SECRET check output and provider URL")
				case "verify-panic":
					panic("SECRET panic output")
				case "verify-cancel":
					cancel()
					<-callbackCtx.Done()
					close(cancelled)
					return path, callbackCtx.Err()
				}
				return path, nil
			}
			result, err := e.runGoal(ctx, "complete the bounded fixture objective", verify, 20*time.Millisecond)
			if result.Status != tc.status || (err == nil) != (tc.status == "complete") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if calls.Load() != tc.calls {
				t.Fatalf("callback calls=%d want=%d", calls.Load(), tc.calls)
			}
			if tc.name == "verify-cancel" {
				select {
				case <-cancelled:
				default:
					t.Fatal("callback was not cancelled")
				}
			}
			data, readErr := os.ReadFile(filepath.Join(e.config.Workspace, ".tool-response"))
			if strings.Contains(string(data), "SECRET") {
				t.Fatalf("callback diagnostics leaked: %s", data)
			}
			if tc.status == "complete" {
				if readErr != nil {
					t.Fatal(readErr)
				}
				var frame struct {
					Result protocolv2.DynamicToolCallResponse `json:"result"`
				}
				if json.Unmarshal(data, &frame) != nil || !frame.Result.Success || len(frame.Result.ContentItems) != 1 {
					t.Fatalf("bad tool response: %s", data)
				}
				content, ok := frame.Result.ContentItems[0].AsInputText()
				if !ok || content.Text != path {
					t.Fatalf("tool result must only be feedback path: %s", data)
				}
			}
		})
	}
}

func TestGoalVerificationTurnBinding(t *testing.T) {
	s := &goalObservation{threadID: testSession, activeTurn: "owned-turn", changed: make(chan struct{})}
	var calls atomic.Int32
	request := protocolv2.NewServerRequestItemToolCall(protocolv2.ServerRequestItemToolCall{Params: protocolv2.DynamicToolCallParams{
		ThreadID: testSession, TurnID: "wrong-turn", CallID: "call", Tool: goalVerificationTool, Arguments: protocolv2.JSONObject(map[string]protocolv2.JSONValue{}),
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.verifier(context.Background(), func(context.Context) (string, error) { calls.Add(1); return "", nil })(ctx, request)
	if err == nil || err.Error() != "goal_server_request_rejected" || calls.Load() != 0 {
		t.Fatalf("unowned turn admitted: calls=%d err=%v", calls.Load(), err)
	}
}

func TestGoalVerificationRejectsUnboundInputs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*protocolv2.DynamicToolCallParams)
	}{
		{"null arguments", func(p *protocolv2.DynamicToolCallParams) { p.Arguments = protocolv2.JSONNull() }},
		{"extra argument", func(p *protocolv2.DynamicToolCallParams) {
			p.Arguments = protocolv2.JSONObject(map[string]protocolv2.JSONValue{"path": protocolv2.JSONString("SECRET")})
		}},
		{"namespace", func(p *protocolv2.DynamicToolCallParams) { p.Namespace = protocolv2.Value("") }},
		{"wrong thread", func(p *protocolv2.DynamicToolCallParams) { p.ThreadID = "SECRET" }},
		{"missing turn", func(p *protocolv2.DynamicToolCallParams) { p.TurnID = "" }},
		{"missing call", func(p *protocolv2.DynamicToolCallParams) { p.CallID = "" }},
		{"wrong tool", func(p *protocolv2.DynamicToolCallParams) { p.Tool = "arbitrary-command" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &goalObservation{threadID: testSession, activeTurn: "owned-turn", changed: make(chan struct{})}
			p := protocolv2.DynamicToolCallParams{ThreadID: testSession, TurnID: "owned-turn", CallID: "call", Tool: goalVerificationTool, Arguments: protocolv2.JSONObject(map[string]protocolv2.JSONValue{})}
			tc.change(&p)
			called := false
			_, err := s.verifier(context.Background(), func(context.Context) (string, error) { called = true; return "", nil })(context.Background(), protocolv2.NewServerRequestItemToolCall(protocolv2.ServerRequestItemToolCall{Params: p}))
			if err == nil || err.Error() != "goal_server_request_rejected" || called {
				t.Fatalf("unbound input accepted: called=%v err=%v", called, err)
			}
		})
	}
}

func TestGoalVerificationAwaitsTurnObservation(t *testing.T) {
	s := &goalObservation{threadID: testSession, changed: make(chan struct{})}
	path := filepath.Join(t.TempDir(), "feedback.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.verifier(ctx, func(context.Context) (string, error) { calls.Add(1); return path, nil })(ctx, protocolv2.NewServerRequestItemToolCall(protocolv2.ServerRequestItemToolCall{Params: protocolv2.DynamicToolCallParams{ThreadID: testSession, TurnID: "owned-turn", CallID: "call", Tool: goalVerificationTool, Arguments: protocolv2.JSONObject(map[string]protocolv2.JSONValue{})}}))
		done <- err
	}()
	if err := s.observe(ctx, protocolv2.NewServerNotificationTurnStarted(protocolv2.ServerNotificationTurnStarted{Params: protocolv2.TurnStartedNotification{ThreadID: testSession, Turn: protocolv2.Turn{ID: "owned-turn", Status: protocolv2.TurnStatusInProgress, Items: []protocolv2.ThreadItem{}}}})); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || calls.Load() != 1 {
		t.Fatalf("preceding observation did not bind: calls=%d err=%v", calls.Load(), err)
	}
}

func TestRunGoalObjectiveWhitespace(t *testing.T) {
	e := goalFixture(t, "multi-turn")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	result, err := e.runGoal(ctx, " \ncomplete the bounded fixture objective\n\t", nil, 20*time.Millisecond)
	if err != nil || result.Status != "complete" {
		t.Fatalf("native objective normalization failed: %+v %v", result, err)
	}
}
