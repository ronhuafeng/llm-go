package codexexec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

// These supplemental controls use the installed official CLI and local fixture
// state only. They do not establish real-model or child-agent task coverage.
func nativeCompatibilityExecutor(t *testing.T, ctx context.Context, endpoint string) *Executor {
	t.Helper()
	workspace := t.TempDir()
	if err := exec.Command("git", "-C", workspace, "init", "-q").Run(); err != nil {
		t.Fatal("cannot initialize native compatibility workspace")
	}
	e, err := New(ctx, Config{
		Workspace: workspace, StateDir: t.TempDir(), Binary: os.Getenv("CODEX_BINARY"),
		Version: os.Getenv("CODEX_CLI_VERSION"), BaseURL: endpoint,
		APIKey: "native_compatibility_fixture", Model: "gpt-6-sol", Reasoning: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	t.Logf("native_compatibility.runtime=%s provider=local_fixture model_calls=0", e.config.Version)
	return e
}

func TestNativeCodexForeignGoalBroadcast(t *testing.T) {
	if os.Getenv("LLMGO_NATIVE_GOAL_SANDBOX") != "1" {
		t.Skip("explicit native suite only")
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e := nativeCompatibilityExecutor(t, ctx, server.URL)
	observation := &goalObservation{objective: "owned native fixture goal", changed: make(chan struct{})}
	seen := make(chan protocolv2.ThreadGoalUpdatedNotification, 8)
	args := append(e.runtimeOverrides(), "-c", "features.goals=true", "app-server")
	command := e.command(ctx, args...)
	client, err := codexsdk.NewContext(ctx, codexsdk.ClientOptions{
		Command: command.Args, Env: command.Env, CWD: e.config.Workspace,
		ServerNotificationHandler: func(ctx context.Context, notification protocolv2.ServerNotification) error {
			if err := observation.observe(ctx, notification); err != nil {
				return err
			}
			if update, ok := notification.AsThreadGoalUpdated(); ok {
				select {
				case seen <- update.Params:
				default:
					return errors.New("native fixture notification overflow")
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("native App Server initialization failed: %v", err)
	}
	var owned []string
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, id := range owned {
			if _, err := client.Threads().Archive(cleanupCtx, protocolv2.ThreadArchiveParams{ThreadID: id}); err != nil {
				t.Errorf("native fixture thread cleanup failed: %v", err)
			}
		}
		if err := client.Close(); err != nil {
			t.Errorf("native fixture client cleanup failed: %v", err)
		}
		if requests.Load() != 0 {
			t.Error("no-turn goal control unexpectedly requested a provider")
		}
		t.Logf("native_goal.provider_requests=%d", requests.Load())
	})
	start := func() string {
		t.Helper()
		response, err := client.Threads().Start(ctx, protocolv2.ThreadStartParams{
			CWD: protocolv2.Value(e.config.Workspace), Model: protocolv2.Value(e.config.Model),
			ModelProvider: protocolv2.Value("llm-go-exec"), Ephemeral: protocolv2.Value(false),
			ApprovalPolicy: protocolv2.Value(protocolv2.NewAskForApprovalNever()),
			Sandbox:        protocolv2.Value(protocolv2.SandboxModeWorkspaceWrite),
		})
		if err != nil || !sessionPattern.MatchString(response.Thread.ID) {
			t.Fatalf("native fixture thread start failed: %v", err)
		}
		owned = append(owned, response.Thread.ID)
		return response.Thread.ID
	}
	parent, foreign := start(), start()
	if parent == foreign {
		t.Fatal("native fixture reused a thread identity")
	}
	observation.mu.Lock()
	observation.threadID = parent
	observation.mu.Unlock()
	setAndObserve := func(id, objective string, status protocolv2.ThreadGoalStatus) {
		t.Helper()
		response, err := client.Threads().GoalSet(ctx, protocolv2.ThreadGoalSetParams{
			ThreadID: id, Objective: protocolv2.Value(objective), Status: protocolv2.Value(status),
		})
		if err != nil || response.Goal.ThreadID != id || response.Goal.Objective != objective || response.Goal.Status != status {
			t.Fatalf("native fixture goal set failed: %v", err)
		}
		for {
			select {
			case update := <-seen:
				if update.ThreadID == id && update.Goal.ThreadID == id && update.Goal.Objective == objective && update.Goal.Status == status {
					return
				}
			case <-ctx.Done():
				t.Fatal("native goal notification was not observed")
			}
		}
	}
	// Complete/paused mutations cannot start a turn. An owned paused goal would
	// intentionally fail goalObservation, so complete is the positive control.
	setAndObserve(parent, observation.objective, protocolv2.ThreadGoalStatusComplete)
	assertOwnedState := func() {
		t.Helper()
		observation.mu.Lock()
		valid := observation.threadID == parent && observation.objective == "owned native fixture goal" && observation.status == protocolv2.ThreadGoalStatusComplete && observation.failure == "" && observation.activeTurn == "" && !observation.completedTurn && !observation.verifying
		observation.mu.Unlock()
		if !valid {
			t.Fatal("foreign native goal update changed the owned observation")
		}
	}
	assertOwnedState()
	setAndObserve(foreign, "foreign native fixture goal", protocolv2.ThreadGoalStatusPaused)
	t.Log("native_goal.foreign_notification=observed")
	assertOwnedState()
	// Require a subsequent owned native notification as a delivery control.
	setAndObserve(parent, observation.objective, protocolv2.ThreadGoalStatusComplete)
	t.Log("native_goal.subsequent_owned_notification=observed")
	assertOwnedState()
	current, err := client.Threads().GoalGet(ctx, protocolv2.ThreadGoalGetParams{ThreadID: parent})
	if err != nil || current.Goal == nil || current.Goal.Value == nil || current.Goal.Value.ThreadID != parent || current.Goal.Value.Objective != observation.objective || current.Goal.Value.Status != protocolv2.ThreadGoalStatusComplete {
		t.Fatal("owned native goal did not retain its identity and state")
	}

	// Positive wire producer/consumer control: the installed official App
	// Server has already emitted an ordinary success response and a typed
	// notification. Now require a real error response, then a subsequent
	// successful response on the same live connection (no model calls).
	_, err = client.Threads().Read(ctx, protocolv2.ThreadReadParams{ThreadID: "missing-native-envelope-thread"})
	if err == nil {
		t.Fatal("official App Server did not deliver an error response for an unknown thread")
	}
	read, err := client.Threads().Read(ctx, protocolv2.ThreadReadParams{ThreadID: parent})
	if err != nil || read.Thread.ID != parent {
		t.Fatalf("official error response corrupted subsequent response routing: %v", err)
	}
	t.Log("native_wire.response=observed notification=observed error=observed subsequent_response=observed")
}

func TestNativeCodexRecoveredExecRetry(t *testing.T) {
	if os.Getenv("LLMGO_NATIVE_GOAL_SANDBOX") != "1" {
		t.Skip("explicit native suite only")
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer native_compatibility_fixture" {
			t.Error("unexpected native fixture provider request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if attempt == 1 {
			// End an established stream before response.completed. An HTTP 503
			// could retry inside HTTP transport without emitting a JSONL error.
			fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"native-interrupted\"}}\n\n")
			w.(http.Flusher).Flush()
			return
		}
		if attempt != 2 {
			t.Error("native retry fixture made an unexpected extra attempt")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `data: {"type":"response.created","response":{"id":"native-recovered"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"id":"native-message","type":"message","role":"assistant","content":[{"type":"output_text","text":"native retry recovered"}]}}

data: {"type":"response.completed","response":{"id":"native-recovered","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

`)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	e := nativeCompatibilityExecutor(t, ctx, server.URL)
	result, err := e.Run(ctx, "Return the fixture response without using tools.", "")
	t.Logf("native_retry.provider_attempts=%d", requests.Load())
	if err != nil || result.Status != "completed" || result.Diagnostic != "completed" || !sessionPattern.MatchString(result.SessionID) {
		t.Fatalf("native exec did not recover: status=%s diagnostic=%s error=%v", result.Status, result.Diagnostic, err)
	}
	if result.ErrorDiagnostic == "" {
		t.Fatal("native exec emitted no intermediate JSONL error; recovery was not exercised")
	}
	if requests.Load() != 2 {
		t.Fatalf("native provider attempts=%d, want 2", requests.Load())
	}
	final, err := os.ReadFile(result.FinalMessagePath)
	if err != nil || strings.TrimSpace(string(final)) != "native retry recovered" {
		t.Fatal("native exec did not preserve the recovered final response")
	}
	t.Logf("native_retry.attempts=%d intermediate_error=%s terminal=completed process_exit=0", requests.Load(), result.ErrorDiagnostic)
}
