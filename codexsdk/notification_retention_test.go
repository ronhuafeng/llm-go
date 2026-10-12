package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func retentionFacts(b *notificationRetention) (int, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count, b.bytes
}
func retentionClient(t *testing.T, count int, bytes int64, options ...ClientOptions) *Client {
	t.Helper()
	c := newTransportHarness()
	if len(options) > 0 {
		c.options = options[0]
	}
	c.options.NotificationRetention = NotificationRetentionBudget{MaxCount: count, MaxBytes: bytes}
	c.retention = &notificationRetention{limit: c.options.NotificationRetention}
	c.notifications = make(chan acceptedNotification, 4096)
	c.dispatchStop = make(chan struct{})
	c.dispatcherDone = make(chan struct{})
	c.closeCausePublished = make(chan struct{})
	c.pendingThreadNotifications = map[string][]protocolv2.ServerNotification{}
	c.pendingDiagnostics = map[string][]DiagnosticRef{}
	go c.notificationDispatcher()
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func retentionRun(c *Client, thread, turn string) *exactRunState {
	s := newExactRunState(c, thread, StartedThreadRun{Start: facadeThreadStartResponse(thread, "fixture")})
	s.setTurn(protocolv2.Turn{ID: turn, Status: protocolv2.TurnStatusInProgress, Items: []protocolv2.ThreadItem{}})
	c.turnMu.Lock()
	c.exactStreams[turn] = map[*exactRunState]struct{}{s: {}}
	c.turnMu.Unlock()
	return s
}
func requireRetentionFailure(t *testing.T, c *Client) {
	t.Helper()
	c.closeMu.Lock()
	err := c.failure
	c.closeMu.Unlock()
	if !errors.Is(err, ErrNotificationRetention) || err.Error() != ErrNotificationRetention.Error() {
		t.Fatal("retention failure missing or unsafe")
	}
	count, bytes := retentionFacts(c.retention)
	if count > c.retention.limit.MaxCount || bytes > c.retention.limit.MaxBytes {
		t.Fatal("retained state exceeded the configured budget")
	}
}

func TestNotificationRetentionHistoryOverflowPreventsTerminalSuccess(t *testing.T) {
	for _, dimension := range []string{"count", "bytes"} {
		t.Run(dimension, func(t *testing.T) {
			n := tokenUsageNotification("thread-1", "turn-1")
			typed, err := exactNotification(n)
			if err != nil {
				t.Fatal(err)
			}
			count, bytes := 1000, int64(1<<20)
			if dimension == "count" {
				count = 6
			} else {
				bytes = 6 * retainedValueBytes(typed)
			}
			c := retentionClient(t, count, bytes)
			s := retentionRun(c, "thread-1", "turn-1")
			// Exercise the history seam directly: no callback or downstream buffer can
			// account for the growing retained SDK history.
			for i := 0; i < 3; i++ {
				_, err := s.acceptOrderedBeforeTerminalCompletion(typed)
				if err != nil {
					c.failClient(err)
					break
				}
			}
			requireRetentionFailure(t, c)
			result, err := (&Stream[StartedThreadRun]{state: s}).Wait(context.Background())
			if !errors.Is(err, ErrNotificationRetention) || len(result.Run.Notifications) != 2 {
				t.Fatalf("partial history err=%v count=%d", err, len(result.Run.Notifications))
			}
			terminal := protocolv2.NewServerNotificationTurnCompleted(protocolv2.ServerNotificationTurnCompleted{Params: protocolv2.TurnCompletedNotification{ThreadID: "thread-1", Turn: protocolv2.Turn{ID: "turn-1", Status: protocolv2.TurnStatusCompleted, Items: []protocolv2.ThreadItem{}}}})
			if err := s.accept(terminal); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(s.err, ErrNotificationRetention) || result.Run.Turn.Status == protocolv2.TurnStatusCompleted {
				t.Fatal("late terminal weakened overflow")
			}
		})
	}
}

func TestNotificationRetentionPendingReplayTransfersReservedEvidence(t *testing.T) {
	c := retentionClient(t, 9, 1<<20)
	s := newExactRunState(c, "thread-1", StartedThreadRun{Start: facadeThreadStartResponse("thread-1", "fixture")})
	if err := c.registerAttachingExactStream(s); err != nil {
		t.Fatal(err)
	}
	if err := c.armTurnAttach("turn-1"); err != nil {
		t.Fatal(err)
	}
	n := tokenUsageNotification("thread-1", "turn-1")
	typed, err := exactNotification(n)
	if err != nil {
		t.Fatal(err)
	}
	// Two pending observations consume the entire budget, including their future
	// history copies, before attachment takes ownership of replay.
	for i := 0; i < 2; i++ {
		if !c.routeExactNotification(n, typed) {
			t.Fatal("pending evidence not admitted")
		}
	}
	count, _ := retentionFacts(c.retention)
	if count != 9 {
		t.Fatal("pending copies not reserved")
	}
	c.attachExactStreamForTurn(s, protocolv2.Turn{ID: "turn-1", Status: protocolv2.TurnStatusInProgress, Items: []protocolv2.ThreadItem{}})
	if len(exactRunNotifications(s)) != 2 {
		t.Fatal("replay lost or duplicated admitted evidence")
	}
	count, _ = retentionFacts(c.retention)
	if count != 8 {
		t.Fatal("replay changed reservation ownership")
	}
	c.routeExactNotification(n, typed)
	requireRetentionFailure(t, c)
	result, err := (&Stream[StartedThreadRun]{state: s}).Wait(context.Background())
	if !errors.Is(err, ErrNotificationRetention) || len(result.Run.Notifications) != 2 {
		t.Fatalf("replay err=%v count=%d", err, len(result.Run.Notifications))
	}
	c.turnMu.Lock()
	pending, replaying := len(c.pendingEvents), len(c.replayingEvents)
	c.turnMu.Unlock()
	if pending != 0 || replaying != 0 {
		t.Fatal("failure retained replay maps")
	}
}

func TestNotificationRetentionFailureFlushesPendingPartialEvidence(t *testing.T) {
	c := retentionClient(t, 5, 1<<20)
	s := newExactRunState(c, "thread-1", StartedThreadRun{Start: facadeThreadStartResponse("thread-1", "fixture")})
	if err := c.registerAttachingExactStream(s); err != nil {
		t.Fatal(err)
	}
	if err := c.armTurnAttach("turn-1"); err != nil {
		t.Fatal(err)
	}
	n := tokenUsageNotification("thread-1", "turn-1")
	typed, _ := exactNotification(n)
	c.routeExactNotification(n, typed)
	c.routeExactNotification(n, typed)
	requireRetentionFailure(t, c)
	result, err := (&Stream[StartedThreadRun]{state: s}).Wait(context.Background())
	if !errors.Is(err, ErrNotificationRetention) || len(result.Run.Notifications) != 1 {
		t.Fatal("pending partial evidence was not published")
	}
}

func TestNotificationRetentionThreadAndDiagnosticSeams(t *testing.T) {
	t.Run("thread-before-attach", func(t *testing.T) {
		c := retentionClient(t, 4, 1<<20)
		if err := c.armThreadAttach("thread-1"); err != nil {
			t.Fatal(err)
		}
		typed := protocolv2.NewServerNotificationThreadNameUpdated(protocolv2.ServerNotificationThreadNameUpdated{Params: protocolv2.ThreadNameUpdatedNotification{ThreadID: "thread-1", ThreadName: protocolv2.Value("safe")}})
		n := rpcNotification{method: "thread/name/updated"}
		c.routeExactNotification(n, typed)
		c.routeExactNotification(n, typed)
		requireRetentionFailure(t, c)
		c.turnMu.Lock()
		remaining := len(c.pendingThreadNotifications)
		c.turnMu.Unlock()
		if remaining != 0 {
			t.Fatal("failed thread buffering retained data")
		}
	})
	t.Run("diagnostic", func(t *testing.T) {
		c := retentionClient(t, 100, 1)
		c.routeExactNotificationError(rpcNotification{method: "thread/tokenUsage/updated", params: map[string]any{"turnId": "turn-private", "private": "credential-sentinel"}}, errors.New("decode failed"))
		requireRetentionFailure(t, c)
		c.turnMu.Lock()
		remaining := len(c.pendingDiagnostics)
		c.turnMu.Unlock()
		if remaining != 0 {
			t.Fatal("failed diagnostics retained data")
		}
	})
	t.Run("direct-facade-identities", func(t *testing.T) {
		c := retentionClient(t, 2, 1<<20)
		for _, id := range []string{"a", "b", "c"} {
			ch := make(chan rpcResponse, 1)
			if err := c.admitPending(id, pendingCall{method: protocolv2.MethodThreadStart, response: ch}); err != nil {
				t.Fatal(err)
			}
			c.routeResponse(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": id}}})
			response := <-ch
			if id == "c" && !errors.Is(response.err, ErrNotificationRetention) {
				t.Fatal("direct facade bypassed retention budget")
			}
		}
		requireRetentionFailure(t, c)
	})
}

func TestNotificationRetentionRawAndTypedQueueCountCopies(t *testing.T) {
	for _, dimension := range []string{"count", "bytes"} {
		t.Run(dimension, func(t *testing.T) {
			count, bytes := 1, int64(1<<20)
			if dimension == "bytes" {
				count, bytes = 100, 300
			}
			c := retentionClient(t, count, bytes, ClientOptions{RawNotifications: &RawNotificationSubscription{Methods: []string{"thread/tokenUsage/updated"}, Handler: func(context.Context, RawNotification) error { return nil }}})
			c.routeNotification(tokenUsageNotification("thread-1", "turn-1"))
			requireRetentionFailure(t, c)
		})
	}
}

func TestNotificationRetentionHighVolumeParallelHistoriesAndCursor(t *testing.T) {
	c := retentionClient(t, DefaultNotificationRetentionCount, DefaultNotificationRetentionBytes)
	first := retentionRun(c, "a", "ta")
	second := retentionRun(c, "b", "tb")
	var wg sync.WaitGroup
	for _, s := range []*exactRunState{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, _ := exactNotification(tokenUsageNotification(s.threadID, s.turnIDSnapshot()))
			for i := 0; i < 1280; i++ {
				if err := s.accept(n); err != nil {
					t.Error("default history overflow")
					return
				}
			}
		}()
	}
	wg.Wait()
	cursor := &Stream[StartedThreadRun]{state: first}
	for i := 0; i < 1280; i++ {
		if !cursor.Next(context.Background()) {
			t.Fatal("cursor lost retained history")
		}
	}
	count, bytes := retentionFacts(c.retention)
	if count < 2560 || bytes <= 0 || count > c.retention.limit.MaxCount || bytes > c.retention.limit.MaxBytes {
		t.Fatal("histories not within finite accounting")
	}
	first.cancel(ErrStreamClosed)
	second.cancel(ErrStreamClosed)
	// Closing a cursor preserves readable history and therefore its charge.
	if len(exactRunNotifications(first)) != 1280 {
		t.Fatal("cancellation silently truncated history")
	}
	snapshot, _ := cursor.Result()
	snapshot.Run.Notifications = nil
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if cursor.Next(canceled) {
		t.Fatal("cursor cancellation returned nonexistent history")
	}
	runtime.KeepAlive(first)
	runtime.KeepAlive(second)
}

func TestNotificationRetentionReclaimsUnreachableHistory(t *testing.T) {
	c := retentionClient(t, 100, 1<<20)
	func() {
		s := retentionRun(c, "thread", "turn")
		n, _ := exactNotification(tokenUsageNotification("thread", "turn"))
		if err := s.accept(n); err != nil {
			t.Fatal(err)
		}
		s.finish(nil)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		count, _ := retentionFacts(c.retention)
		if count == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("unreachable history charge not reclaimed")
}

func TestNotificationRetentionInvalidOptionsAndSafeError(t *testing.T) {
	for _, b := range []NotificationRetentionBudget{{MaxCount: -1}, {MaxBytes: -1}} {
		_, err := New(ClientOptions{CWD: t.TempDir(), Command: []string{"must-not-launch"}, NotificationRetention: b})
		if err == nil || strings.Contains(err.Error(), "must-not-launch") {
			t.Fatal("invalid budget not rejected before launch")
		}
	}
}

func TestNotificationRetentionWithConcurrentIsolatedProcessesAndRawDelivery(t *testing.T) {
	parent := environmentDigest(strings.Join(os.Environ(), "\n"))
	var wg sync.WaitGroup
	failures := make(chan string, 2)
	for _, identity := range []string{"left", "right"} {
		home := t.TempDir()
		wg.Add(1)
		go func() {
			defer wg.Done()
			delivered := make(chan string, 4)
			c, err := New(ClientOptions{CWD: home, Command: environmentCommand(), Env: []string{"CODEX_HOME=" + home, "CODEXSDK_ENV_SECRET=private-" + identity, "CODEXSDK_ENV_CORRELATION=" + identity}, NotificationRetention: NotificationRetentionBudget{MaxCount: 8, MaxBytes: 16 << 10}, RawNotifications: &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension}, EnableRawResponseItemExtension: true, Handler: func(_ context.Context, n RawNotification) error {
				if n.ThreadID != identity || n.TurnID != "turn-"+identity {
					return errors.New("correlation failed")
				}
				delivered <- "raw"
				return nil
			}}, ServerNotificationHandler: func(_ context.Context, n protocolv2.ServerNotification) error {
				usage, ok := n.AsThreadTokenUsageUpdated()
				if !ok || usage.Params.ThreadID != identity || usage.Params.TurnID != "turn-"+identity {
					return errors.New("correlation failed")
				}
				delivered <- "typed"
				return nil
			}})
			if err != nil {
				failures <- "construction failed"
				return
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			facts, err := c.call(ctx, "fixture/environment", nil)
			if err != nil {
				failures <- "environment query failed"
				return
			}
			fact, _ := facts["CODEXSDK_ENV_SECRET"].(map[string]any)
			if fact["digest"] != environmentDigest("private-"+identity) {
				failures <- "environment crossed clients"
				return
			}
			for i := 0; i < 128; i++ {
				if _, err = c.call(ctx, "fixture/budget-notifications", nil); err != nil {
					failures <- "within-budget work failed"
					return
				}
				for _, want := range []string{"raw", "typed"} {
					select {
					case got := <-delivered:
						if got != want {
							failures <- "delivery order changed"
							return
						}
					case <-ctx.Done():
						failures <- "delivery missing"
						return
					}
				}
				// A typed response is still usable while raw delivery runs independently.
				if _, err = c.Models().List(ctx, protocolv2.ModelListParams{}); err != nil {
					failures <- "typed facade failed"
					return
				}
			}
			_, err = c.call(ctx, "fixture/budget-notifications", map[string]any{"oversize": true})
			if !errors.Is(err, ErrNotificationRetention) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "payload-sentinel") {
				failures <- "overflow classification unsafe"
				return
			}
			count, bytes := retentionFacts(c.retention)
			if count > 8 || bytes > 16<<10 {
				failures <- "SDK data exceeded budget"
				return
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if environmentDigest(strings.Join(os.Environ(), "\n")) != parent {
		t.Fatal("parent environment changed")
	}
}

func TestNotificationRetentionSlowCallbackOverflowAndFailureCleanup(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	c := retentionClient(t, 2, 1<<20, ClientOptions{ServerNotificationHandler: func(context.Context, protocolv2.ServerNotification) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	n := tokenUsageNotification("thread", "turn")
	c.routeNotification(n)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback not reached")
	}
	// One executing callback and one queued frame fill the retention budget even
	// though the transport queue still has thousands of free entries.
	c.routeNotification(n)
	c.routeNotification(n)
	requireRetentionFailure(t, c)
	count, _ := retentionFacts(c.retention)
	if count > 2 {
		t.Fatal("blocked callback data unbounded")
	}
	releaseOnce.Do(func() { close(release) })
	if err := c.Close(); !errors.Is(err, ErrNotificationRetention) {
		t.Fatal("first cause changed")
	}
	count, bytes := retentionFacts(c.retention)
	if count != 0 || bytes != 0 {
		t.Fatal("callback and queue reservations leaked")
	}
}

func TestNotificationRetentionReplayFailureRacePreservesFirstCause(t *testing.T) {
	c := retentionClient(t, 9, 1<<20)
	s := newExactRunState(c, "thread", StartedThreadRun{Start: facadeThreadStartResponse("thread", "fixture")})
	if err := c.registerAttachingExactStream(s); err != nil {
		t.Fatal(err)
	}
	if err := c.armTurnAttach("turn"); err != nil {
		t.Fatal(err)
	}
	n := tokenUsageNotification("thread", "turn")
	typed, _ := exactNotification(n)
	c.routeExactNotification(n, typed)
	c.routeExactNotification(n, typed)
	published, release := make(chan struct{}), make(chan struct{})
	c.testAfterExactStreamPublished = func() { close(published); <-release }
	attached := make(chan struct{})
	go func() {
		c.attachExactStreamForTurn(s, protocolv2.Turn{ID: "turn", Status: protocolv2.TurnStatusInProgress, Items: []protocolv2.ThreadItem{}})
		close(attached)
	}()
	<-published
	c.failClient(ErrNotificationRetention)
	result, err := (&Stream[StartedThreadRun]{state: s}).Wait(context.Background())
	if !errors.Is(err, ErrNotificationRetention) || len(result.Run.Notifications) != 2 {
		t.Fatal("failure did not flush replay prefix")
	}
	c.failClient(errors.New("secondary transport failure"))
	close(release)
	select {
	case <-attached:
	case <-time.After(time.Second):
		t.Fatal("replay deadlocked with failure")
	}
	requireRetentionFailure(t, c)
}

func TestNotificationRetentionProcessAttachmentWithRequiredRawEvents(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "attached", true: "replayed"}[replay], func(t *testing.T) {
			options := rawOptions(t, "raw-happy", func(context.Context, RawNotification) error { return nil })
			options.Env = []string{"CODEXSDK_FAKE_RECORD=" + tempRecord(t), "CODEX_HOME=" + t.TempDir()}
			options.NotificationRetention = NotificationRetentionBudget{MaxCount: 128, MaxBytes: 64 << 10}
			c, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if replay {
				ready := make(chan struct{})
				var once sync.Once
				c.testBeforeExactTurnAttach = func() {
					select {
					case <-ready:
					case <-c.ctx.Done():
					}
				}
				c.testPendingExactNotification = func(n rpcNotification) {
					if n.method == "turn/completed" {
						once.Do(func() { close(ready) })
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := c.ThreadRunner().Start(ctx, StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
			if err != nil || len(result.Run.Notifications) != 3 || result.Run.Turn.Status != protocolv2.TurnStatusCompleted {
				t.Fatal("bounded process attachment changed normal results")
			}
			count, bytes := retentionFacts(c.retention)
			if count > 128 || bytes > 64<<10 {
				t.Fatal("process attachment exceeded budget")
			}
		})
	}
}

func TestNotificationRetentionLateDuplicatesDoNotRewriteCompletedHistory(t *testing.T) {
	c := retentionClient(t, 4, 1<<20)
	s := retentionRun(c, "thread", "turn")
	n := tokenUsageNotification("thread", "turn")
	typed, _ := exactNotification(n)
	if err := s.accept(typed); err != nil {
		t.Fatal(err)
	}
	s.finish(nil)
	beforeCount, beforeBytes := retentionFacts(c.retention)
	for i := 0; i < 2048; i++ {
		if c.routeExactNotification(n, typed) {
			t.Fatal("late observation attached to completed history")
		}
	}
	afterCount, afterBytes := retentionFacts(c.retention)
	if beforeCount != afterCount || beforeBytes != afterBytes || len(exactRunNotifications(s)) != 1 {
		t.Fatal("late duplicate history accumulated")
	}
	runtime.KeepAlive(s)
}

func TestNotificationRetentionDenseRawChargesGoStorageBeyondWireBytes(t *testing.T) {
	c := retentionClient(t, 1000, 4096, ClientOptions{RawNotifications: &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension}, EnableRawResponseItemExtension: true, Handler: func(context.Context, RawNotification) error { return nil }}})
	dense := make([]any, 128)
	for i := range dense {
		dense[i] = map[string]any{"x": true}
	}
	params := map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"type": "compaction", "encrypted_content": "private-sentinel", "dense": dense}}
	wire, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) >= 4096 {
		t.Fatal("fixture does not distinguish wire from retained bytes")
	}
	c.routeNotification(rpcNotification{method: RawResponseItemCompletedExtension, params: params})
	requireRetentionFailure(t, c)
}
