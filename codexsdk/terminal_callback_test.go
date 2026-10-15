package codexsdk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestTerminalCallbacksCanReadWithoutReleasingRun(t *testing.T) {
	for _, replay := range []bool{false, true} {
		name := "attached"
		if replay {
			name = "replayed"
		}
		for _, disposition := range []string{"success", "handler-failure", "close", "backpressure"} {
			t.Run(name+"/"+disposition, func(t *testing.T) {
				t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				var client *Client
				type observation struct {
					raw bool
					err error
				}
				observed := make(chan observation, 2)
				rawRelease, typedRelease := make(chan struct{}), make(chan struct{})
				var rawOnce, typedOnce sync.Once
				releaseRaw := func() { rawOnce.Do(func() { close(rawRelease) }) }
				releaseTyped := func() { typedOnce.Do(func() { close(typedRelease) }) }
				cause := errors.New("terminal observer rejected delivery")
				callback := func(callbackCtx context.Context, raw bool) error {
					// Deliberately no RPC deadline: only test cleanup may cancel it.
					read, err := client.Threads().Read(callbackCtx, protocolv2.ThreadReadParams{ThreadID: "thread-1"})
					if err == nil && read.Thread.ID != "thread-1" {
						err = errors.New("callback read returned a different thread")
					}
					observed <- observation{raw: raw, err: err}
					release := typedRelease
					if raw {
						release = rawRelease
					}
					select {
					case <-release:
					case <-callbackCtx.Done():
						return callbackCtx.Err()
					}
					if !raw && disposition == "handler-failure" {
						return cause
					}
					return err
				}
				mode := "terminal-callback-rpc"
				if replay {
					mode += "-replay"
				}
				var err error
				client, err = NewContext(ctx, ClientOptions{
					CWD: t.TempDir(), Command: fakeCommand(mode),
					ServerNotificationHandler: func(ctx context.Context, n protocolv2.ServerNotification) error {
						if _, ok := n.AsTurnCompleted(); ok {
							return callback(ctx, false)
						}
						return nil
					},
					RawNotifications: &RawNotificationSubscription{
						Methods: []string{protocolv2.MethodTurnCompleted},
						Handler: func(ctx context.Context, _ RawNotification) error { return callback(ctx, true) },
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { releaseRaw(); releaseTyped(); _ = client.Close() })
				if replay {
					pending := make(chan struct{})
					client.testBeforeExactTurnAttach = func() {
						select {
						case <-pending:
						case <-ctx.Done():
						}
					}
					client.testPendingExactNotification = func(n rpcNotification) {
						if n.method == protocolv2.MethodTurnCompleted {
							close(pending)
						}
					}
				}
				stream, err := client.ThreadRunner().StartStream(ctx, StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
				if err != nil {
					t.Fatal(err)
				}
				if !replay {
					if _, err := client.call(ctx, "fixture/complete", nil); err != nil {
						t.Fatal("reader stalled behind terminal callback:", err)
					}
				}
				assertReadWhileRunPending := func(raw bool) {
					t.Helper()
					select {
					case got := <-observed:
						if got.raw != raw || got.err != nil {
							t.Fatalf("callback raw=%t, want %t, read error=%v", got.raw, raw, got.err)
						}
					case <-ctx.Done():
						t.Fatal("callback RPC could not make progress before release")
					}
					canceled, stop := context.WithCancel(ctx)
					stop()
					partial, err := stream.Wait(canceled)
					if !errors.Is(err, context.Canceled) || partial.Run.Turn.Status != protocolv2.TurnStatusCompleted {
						t.Fatalf("Run escaped the callback fence: status=%s err=%v", partial.Run.Turn.Status, err)
					}
					if len(partial.Run.Notifications) != 3 {
						t.Fatalf("post-terminal read notification changed history: %d observations", len(partial.Run.Notifications))
					}
				}
				assertReadWhileRunPending(true)
				releaseRaw()
				assertReadWhileRunPending(false)
				var want error
				switch disposition {
				case "handler-failure":
					want = cause
				case "close":
					closeCtx, stop := context.WithCancel(context.Background())
					stop()
					_, _ = client.CloseContext(closeCtx)
					want = ErrClientClosed
				case "backpressure":
					// The typed callback is held, so fill the existing bounded queue.
					for i := 0; i <= cap(client.notifications); i++ {
						client.routeNotification(tokenUsageNotification("thread-1", "turn-1"))
					}
					want = ErrNotificationBackpressure
				}
				releaseTyped()
				result, err := stream.Wait(ctx)
				if !errors.Is(err, want) {
					t.Fatalf("Run error=%v, want %v", err, want)
				}
				if len(result.Run.Notifications) != 3 || result.Run.Turn.Status != protocolv2.TurnStatusCompleted {
					t.Fatal("terminal snapshot changed during callback delivery")
				}
				shutdown, closeErr := client.CloseContext(ctx)
				if !shutdown.ProcessReaped || shutdown.CallbackWorkPending != 0 {
					t.Fatalf("callback/process work did not drain: %+v, err=%v", shutdown, closeErr)
				}
				if disposition == "handler-failure" || disposition == "backpressure" {
					if !errors.Is(closeErr, want) {
						t.Fatalf("shutdown replaced first cause: %v", closeErr)
					}
				} else if disposition == "success" && closeErr != nil {
					t.Fatal(closeErr)
				}
			})
		}
	}
}

func TestTerminalAdmissionFreezesDuplicateBeforeCallbackCompletion(t *testing.T) {
	client := retentionClient(t, 64, 1<<20)
	state := retentionRun(client, "thread-1", "turn-1")
	terminal := func(status protocolv2.TurnStatus) protocolv2.ServerNotification {
		return protocolv2.NewServerNotificationTurnCompleted(protocolv2.ServerNotificationTurnCompleted{Params: protocolv2.TurnCompletedNotification{
			ThreadID: "thread-1", Turn: protocolv2.Turn{ID: "turn-1", Status: status, Items: []protocolv2.ThreadItem{}},
		}})
	}
	complete, err := state.acceptOrderedBeforeTerminalCompletion(terminal(protocolv2.TurnStatusCompleted))
	if err != nil || complete == nil {
		t.Fatalf("first terminal admission: complete=%t error=%v", complete != nil, err)
	}
	count, bytes := retentionFacts(client.retention)
	for range 2 {
		duplicate, err := state.acceptOrderedBeforeTerminalCompletion(terminal(protocolv2.TurnStatusFailed))
		if duplicate != nil || err != nil {
			t.Fatalf("duplicate terminal acquired a completion: complete=%t error=%v", duplicate != nil, err)
		}
	}
	if afterCount, afterBytes := retentionFacts(client.retention); afterCount != count || afterBytes != bytes {
		t.Fatal("duplicate terminal retained another run representation")
	}
	complete()
	result, err := (&Stream[StartedThreadRun]{state: state}).Wait(context.Background())
	if err != nil || result.Run.Turn.Status != protocolv2.TurnStatusCompleted || len(result.Run.Notifications) != 1 {
		t.Fatalf("duplicate terminal changed the first snapshot: %+v, error=%v", result.Run, err)
	}
}
