package codexsdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestDirectTypedCallbacksPermitSameThreadContinuation(t *testing.T) {
	for _, opening := range []string{"start", "resume"} {
		t.Run(opening, func(t *testing.T) {
			t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
			type observation struct {
				method, threadID, turnID string
				raw                      bool
			}
			observed := make(chan observation, 32)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := NewContext(ctx, ClientOptions{
				CWD: t.TempDir(), Command: fakeCommand("direct-callback"),
				ServerNotificationHandler: func(_ context.Context, n protocolv2.ServerNotification) error {
					id := extractNotificationIdentity(n)
					observed <- observation{method: string(n.Kind()), threadID: id.threadID, turnID: id.turnID}
					return nil
				},
				RawNotifications: &RawNotificationSubscription{
					Methods: []string{protocolv2.MethodTurnCompleted},
					Handler: func(_ context.Context, n RawNotification) error {
						observed <- observation{method: n.Method, threadID: n.ThreadID, turnID: n.TurnID, raw: true}
						return nil
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var threadID string
			if opening == "start" {
				response, err := c.Threads().Start(ctx, protocolv2.ThreadStartParams{})
				if err != nil {
					t.Fatal(err)
				}
				threadID = response.Thread.ID
			} else {
				response, err := c.Threads().Resume(ctx, protocolv2.ThreadResumeParams{ThreadID: "thread-existing"})
				if err != nil {
					t.Fatal(err)
				}
				threadID = response.Thread.ID
			}
			assertDelivered := func(turnID string) {
				t.Helper()
				// The controlled server writes this turn's notifications before
				// answering Read. Read proves ingestion, never callback delivery.
				if _, err := c.Threads().Read(ctx, protocolv2.ThreadReadParams{ThreadID: threadID}); err != nil {
					t.Fatal(err)
				}
				want := []observation{
					{method: protocolv2.MethodItemCompleted, threadID: threadID, turnID: turnID},
					{method: protocolv2.MethodThreadTokenUsageUpdated, threadID: threadID, turnID: turnID},
					{method: protocolv2.MethodTurnCompleted, threadID: threadID, turnID: turnID, raw: true},
					{method: protocolv2.MethodTurnCompleted, threadID: threadID, turnID: turnID},
				}
				wait, stop := context.WithTimeout(ctx, 2*time.Second)
				defer stop()
				for _, expected := range want {
					select {
					case actual := <-observed:
						if actual != expected {
							t.Fatalf("callback = %+v, want %+v", actual, expected)
						}
					case <-wait.Done():
						t.Fatal("admitted direct callback was not delivered before Close")
					}
				}
			}
			// Both direct turns must deliver normally on the same live process.
			for range 2 {
				response, err := c.Turns().Start(ctx, protocolv2.TurnStartParams{ThreadID: threadID, Input: []protocolv2.UserInput{}})
				if err != nil {
					t.Fatal(err)
				}
				assertDelivered(response.Turn.ID)
				c.turnMu.Lock()
				unowned := len(c.armedThreadAttach) + len(c.armedTurnAttach) + len(c.pendingEvents) + len(c.attachmentRetention)
				c.turnMu.Unlock()
				if unowned != 0 {
					t.Fatalf("direct facade left %d attachment reservations", unowned)
				}
			}
			// Mixing the two public paths must preserve the runner's ordinary
			// terminal fence and must not inherit a stalled direct-facade queue.
			result, err := c.ThreadRunner().Resume(ctx, ResumeThreadRunRequest{
				Thread: protocolv2.ThreadResumeParams{ThreadID: threadID},
				Turn:   protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}},
			})
			if err != nil || result.Run.Turn.Status != protocolv2.TurnStatusCompleted || !result.Run.FinalResponsePresent {
				t.Fatalf("runner continuation: status=%s final=%t error=%v", result.Run.Turn.Status, result.Run.FinalResponsePresent, err)
			}
			assertDelivered(result.Run.Turn.ID)
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer closeCancel()
			shutdown, err := c.CloseContext(closeCtx)
			if err != nil || !shutdown.ProcessReaped || shutdown.CallbackWorkPending != 0 {
				t.Fatalf("shutdown=%+v error=%v", shutdown, err)
			}
			select {
			case duplicate := <-observed:
				t.Fatalf("duplicate callback after drain: %+v", duplicate)
			default:
			}
		})
	}
}

func TestDirectTypedCallbackFailureKeepsFirstCause(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	cause := errors.New("direct callback failed")
	called := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := NewContext(ctx, ClientOptions{
		CWD: t.TempDir(), Command: fakeCommand("direct-callback"),
		ServerNotificationHandler: func(context.Context, protocolv2.ServerNotification) error {
			called <- struct{}{}
			return cause
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	thread, err := c.Threads().Start(ctx, protocolv2.ThreadStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Turns().Start(ctx, protocolv2.TurnStartParams{ThreadID: thread.Thread.ID, Input: []protocolv2.UserInput{}}); err != nil && !errors.Is(err, cause) {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-ctx.Done():
		t.Fatal("direct callback required Close before executing")
	}
	select {
	case <-c.closeCausePublished:
	case <-ctx.Done():
		t.Fatal("direct callback failure did not settle the client")
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	shutdown, err := c.CloseContext(closeCtx)
	if !errors.Is(err, cause) || !errors.Is(err, ErrHandlerFailed) || !shutdown.ProcessReaped || shutdown.CallbackWorkPending != 0 {
		t.Fatalf("shutdown=%+v error=%v", shutdown, err)
	}
}
