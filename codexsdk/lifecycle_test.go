package codexsdk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func lifecycleOptions(t *testing.T, mode string) (ClientOptions, string) {
	t.Helper()
	home := t.TempDir()
	pidFile := filepath.Join(home, "owned-pid")
	return ClientOptions{CWD: home, Command: environmentCommand(), Env: []string{"CODEX_HOME=" + home, "CODEXSDK_ENV_SECRET=private-lifecycle-sentinel", "CODEXSDK_FIXTURE_LIFECYCLE=" + mode, "CODEXSDK_FIXTURE_PID=" + pidFile, "CODEXSDK_ENV_CORRELATION=isolated", "CODEXSDK_FIXTURE_EVENTS=4"}}, pidFile
}
func waitFixturePID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(string(raw))
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("owned child did not publish its identity")
	return 0
}
func requireFixtureReaped(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	} // Native process-reaping proof is Linux-only.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); os.IsNotExist(err) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("SDK-owned process was not reaped within its cleanup bound")
}
func requireOwnedCleanup(t *testing.T, c *Client) {
	t.Helper()
	select {
	case <-c.processDone:
	case <-time.After(5 * time.Second):
		t.Fatal("owned process cleanup blocked on callbacks")
	}
	if c.cmd != nil && c.cmd.ProcessState == nil {
		t.Fatal("owned process exit was not reaped")
	}
}

func TestCloseContextLetsLauncherForwardAndReapChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Interrupt forwarding fixture requires Unix signals")
	}
	for _, expired := range []bool{false, true} {
		t.Run(strconv.FormatBool(expired), func(t *testing.T) {
			options, pidFile := lifecycleOptions(t, "launcher")
			options.Env = append(options.Env, "GORACE=atexit_sleep_ms=0")
			c, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			pid := waitFixturePID(t, pidFile)
			childPID := waitFixturePID(t, pidFile+".child")
			childSettled := false
			t.Cleanup(func() {
				// Also clean up descendants if the regression kills the launcher.
				if !childSettled {
					p, err := os.FindProcess(childPID)
					if err == nil {
						_ = p.Kill()
					}
				}
				_ = c.Close()
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if expired {
				cancel()
			}
			result, err := c.CloseContext(ctx)
			if expired {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("caller cancellation was lost")
				}
			} else if err != nil || !result.ProcessReaped {
				t.Fatal("launcher cleanup failed")
			}
			requireOwnedCleanup(t, c)
			if _, err := os.Stat(pidFile + ".child.settled"); err != nil {
				t.Fatal("process cleanup killed launcher before native child settled")
			}
			requireFixtureReaped(t, pid)
			requireFixtureReaped(t, childPID)
			childSettled = true
		})
	}
}

func TestNewContextCanceledBeforeLaunch(t *testing.T) {
	options, pidFile := lifecycleOptions(t, "stall-initialize")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := NewContext(ctx, options)
	if c != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("pre-canceled construction did not reject launch")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatal("pre-canceled construction launched a child")
	}
}
func TestNewContextBoundsStalledInitializeAndReapsOwnedProcess(t *testing.T) {
	options, pidFile := lifecycleOptions(t, "stall-initialize")
	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		c   *Client
		err error
	}
	done := make(chan outcome, 1)
	go func() { c, err := NewContext(ctx, options); done <- outcome{c, err} }()
	pid := waitFixturePID(t, pidFile)
	start := time.Now()
	cancel()
	select {
	case result := <-done:
		if result.c != nil || !errors.Is(result.err, context.Canceled) {
			t.Fatal("stalled initialize lost cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("construction ignored caller cancellation")
	}
	if time.Since(start) > time.Second {
		t.Fatal("construction return exceeded caller bound")
	}
	requireFixtureReaped(t, pid)
}
func TestNewContextInitializeDeadlineAndFailureCauses(t *testing.T) {
	for _, mode := range []string{"stall-initialize", "invalid-initialize", "eof-initialize"} {
		t.Run(mode, func(t *testing.T) {
			options, pidFile := lifecycleOptions(t, mode)
			timeout := 3 * time.Second
			if mode == "stall-initialize" {
				timeout = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			c, err := NewContext(ctx, options)
			if c != nil || err == nil || strings.Contains(err.Error(), "private-lifecycle") {
				t.Fatal("initialize failure missing or unsafe")
			}
			if mode == "stall-initialize" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("initialize deadline lost")
			}
			if mode != "stall-initialize" && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrClientClosed)) {
				t.Fatal("initialize/transport failure collapsed into cancellation or close")
			}
			requireFixtureReaped(t, waitFixturePID(t, pidFile))
		})
	}
}
func TestNewContextSuccessfulClientOutlivesStartupObserver(t *testing.T) {
	options, _ := lifecycleOptions(t, "combined")
	ctx, cancel := context.WithCancel(context.Background())
	c, err := NewContext(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	defer c.Close()
	result, err := c.Models().List(context.Background(), protocolv2.ModelListParams{})
	if err != nil || len(result.Data) != 1 || result.Data[0].ID != "fixture-model" || result.Data[0].DisplayName != "Fixture" || result.Data[0].DefaultReasoningEffort != "high" {
		t.Fatal("startup observer cancellation changed typed work")
	}
}
func TestCloseContextReapsUnresponsiveProcessAndSettlesPendingRPC(t *testing.T) {
	options, pidFile := lifecycleOptions(t, "unresponsive")
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	pid := waitFixturePID(t, pidFile)
	done := make(chan error, 1)
	go func() { _, err := c.Models().List(context.Background(), protocolv2.ModelListParams{}); done <- err }()
	waitForPendingCount(t, c, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = c.CloseContext(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unexpected shutdown failure")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrClientClosed) {
			t.Fatal("pending RPC lost the first close cause")
		}
	case <-time.After(time.Second):
		t.Fatal("close did not settle pending RPC")
	}
	requireOwnedCleanup(t, c)
	requireFixtureReaped(t, pid)
}

func TestCloseContextReportsUncooperativeNotificationAndPreventsLateSuccess(t *testing.T) {
	options, pidFile := lifecycleOptions(t, "blocked-work")
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	options.RawNotifications = &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension}, EnableRawResponseItemExtension: true, Handler: func(context.Context, RawNotification) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); _ = c.Close() })
	stream, err := c.ThreadRunner().StartStream(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	pid := waitFixturePID(t, pidFile)
	rpc := make(chan error, 1)
	go func() { _, err := c.Models().List(context.Background(), protocolv2.ModelListParams{}); rpc <- err }()
	waitForPendingCount(t, c, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	result, err := c.CloseContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrCallbackWorkPending) || result.CallbackWorkPending < 1 || !errors.Is(result.Cause, ErrClientClosed) {
		t.Fatal("uncooperative callback was not reported accurately")
	}
	if time.Since(start) > time.Second {
		t.Fatal("uncooperative callback blocked caller return")
	}
	partial, runErr := stream.Wait(context.Background())
	if !errors.Is(runErr, ErrClientClosed) || len(partial.Run.Notifications) != 1 {
		t.Fatal("close lost the admitted pre-RPC partial observation")
	}
	select {
	case err := <-rpc:
		if !errors.Is(err, ErrClientClosed) {
			t.Fatal("pending RPC cause changed")
		}
	case <-time.After(time.Second):
		t.Fatal("pending RPC deadlocked")
	}
	requireOwnedCleanup(t, c)
	requireFixtureReaped(t, pid)
	if c.callbackWork.Load() < 1 {
		t.Fatal("blocked Go callback was reported stopped")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-c.handlersDone:
	case <-time.After(time.Second):
		t.Fatal("released callback did not finish")
	}
	late := protocolv2.NewServerNotificationTurnCompleted(protocolv2.ServerNotificationTurnCompleted{Params: protocolv2.TurnCompletedNotification{ThreadID: stream.state.threadID, Turn: protocolv2.Turn{ID: stream.state.turnIDSnapshot(), Status: protocolv2.TurnStatusCompleted, Items: []protocolv2.ThreadItem{}}}})
	_ = stream.state.accept(late)
	if !errors.Is(stream.Err(), ErrClientClosed) {
		t.Fatal("late callback/terminal became success")
	}
	count, _ := retentionFacts(c.retention)
	if count > options.NotificationRetention.MaxCount && options.NotificationRetention.MaxCount != 0 {
		t.Fatal("closed retention exceeded budget")
	}
}

func TestCloseContextReportsUncooperativeServerRequest(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	options := ClientOptions{CWD: t.TempDir(), Command: fakeCommand("approval"), Env: []string{"CODEXSDK_FAKE_RECORD=" + tempRecord(t)}, ServerRequestHandler: func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
		close(entered)
		<-release
		return ServerRequestResponse{}, errors.New("private-handler-sentinel")
	}}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); _ = c.Close() })
	stream, err := c.ThreadRunner().StartStream(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	result, err := c.CloseContext(ctx)
	if !errors.Is(err, ErrCallbackWorkPending) || result.CallbackWorkPending < 1 || strings.Contains(err.Error(), "private-handler") {
		t.Fatal("server callback unresolved state unsafe")
	}
	requireOwnedCleanup(t, c)
	if !errors.Is(stream.Err(), ErrClientClosed) {
		t.Fatal("server callback close did not settle exact waiter")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-c.handlersDone:
	case <-time.After(time.Second):
		t.Fatal("server callback did not finish after release")
	}
	if c.failure != nil {
		t.Fatal("late server callback replaced normal close cause")
	}
}

func TestLifecycleObserverDeadlineDuringTerminalFenceRemainsLocal(t *testing.T) {
	options, _ := lifecycleOptions(t, "blocked-work")
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	options.RawNotifications = &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension}, EnableRawResponseItemExtension: true, Handler: func(context.Context, RawNotification) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); _ = c.Close() })
	stream, err := c.ThreadRunner().StartStream(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	finished := make(chan error, 1)
	go func() { _, err := c.call(context.Background(), "fixture/finish", nil); finished <- err }()
	deadline := time.Now().Add(time.Second)
	for len(exactRunNotifications(stream.state)) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(exactRunNotifications(stream.state)) != 2 {
		t.Fatal("terminal did not reach the delivery fence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	partial, err := stream.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || partial.Run.Turn.Status != protocolv2.TurnStatusCompleted || partial.Run.FinalResponsePresent {
		t.Fatal("observer deadline changed partial protocol facts")
	}
	if c.isClosed() || stream.state.terminal {
		t.Fatal("observer deadline canceled the shared run")
	}
	releaseOnce.Do(func() { close(release) })
	result, err := stream.Wait(context.Background())
	if err != nil || result.Run.Turn.Status != protocolv2.TurnStatusCompleted {
		t.Fatal("local deadline prevented later shared success")
	}
	if err := <-finished; err != nil {
		t.Fatal("terminal fence prevented subsequent RPC response")
	}
}

func TestLifecycleCombinedIsolatedClientsRawReplayAndHighVolume(t *testing.T) {
	parent := environmentDigest(strings.Join(os.Environ(), "\n"))
	var wg sync.WaitGroup
	failures := make(chan string, 2)
	for _, identity := range []string{"combined-left", "combined-right"} {
		options, pidFile := lifecycleOptions(t, "combined")
		options.Env = append(options.Env, "CODEXSDK_ENV_CORRELATION="+identity, "CODEXSDK_ENV_SECRET=private-"+identity, "CODEXSDK_FIXTURE_EVENTS=1280")
		options.NotificationQueueCapacity = 4096
		options.NotificationRetention = NotificationRetentionBudget{MaxCount: 10000, MaxBytes: 64 << 20}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var rawCount, typedCount int
			var callbackMu sync.Mutex
			options.RawNotifications = &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension}, EnableRawResponseItemExtension: true, Handler: func(_ context.Context, n RawNotification) error {
				if n.ThreadID != identity || n.TurnID != "turn-"+identity {
					return errors.New("correlation changed")
				}
				envelope, _ := n.Envelope.AsObject()
				params, _ := envelope["params"].AsObject()
				item, _ := params["item"].AsObject()
				number, _ := item["number"].AsNumber()
				if number.String() != "9007199254740993" {
					return errors.New("wire number lost")
				}
				callbackMu.Lock()
				marker, _ := item["marker"].AsString()
				want := "before-response"
				if rawCount > 0 {
					want = strconv.Itoa((rawCount - 1) / 2)
				}
				if marker != want {
					callbackMu.Unlock()
					return errors.New("raw transport order changed")
				}
				rawCount++
				callbackMu.Unlock()
				return nil
			}}
			options.ServerNotificationHandler = func(_ context.Context, n protocolv2.ServerNotification) error {
				callbackMu.Lock()
				defer callbackMu.Unlock()
				typedCount++
				if usage, ok := n.AsThreadTokenUsageUpdated(); ok {
					if usage.Params.ThreadID != identity || usage.Params.TurnID != "turn-"+identity || rawCount != typedCount {
						return errors.New("typed correlation or raw/typed order changed")
					}
				}
				return nil
			}
			startup, cancelStartup := context.WithTimeout(context.Background(), 3*time.Second)
			c, err := NewContext(startup, options)
			cancelStartup()
			if err != nil {
				failures <- "combined startup failed"
				return
			}
			defer c.Close()
			// This checks complete high-volume replay, not throughput. Race
			// instrumentation and concurrent repository tests can exhaust a
			// short wall-clock budget even after all 1,282 observations arrive.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			facts, err := c.call(ctx, "fixture/environment", nil)
			if err != nil {
				failures <- "environment observation failed"
				return
			}
			fact, _ := facts["CODEXSDK_ENV_SECRET"].(map[string]any)
			if fact["digest"] != environmentDigest("private-"+identity) {
				failures <- "environment crossed clients"
				return
			}
			models, err := c.Models().List(ctx, protocolv2.ModelListParams{})
			if err != nil || len(models.Data) != 1 || models.Data[0].ID != "fixture-model" || models.Data[0].Description != "typed observation" || !models.Data[0].IsDefault {
				failures <- "ordinary model fields changed"
				return
			}
			pendingTerminal := make(chan struct{})
			c.testBeforeExactTurnAttach = func() {
				select {
				case <-pendingTerminal:
				case <-c.closeCausePublished:
				case <-ctx.Done():
				}
			}
			c.testPendingExactNotification = func(n rpcNotification) {
				if n.method == "turn/completed" {
					close(pendingTerminal)
				}
			}
			result, err := c.ThreadRunner().Start(ctx, StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
			if err != nil || result.Start.Thread.ID != identity || result.Run.Turn.ID != "turn-"+identity || result.Run.Turn.Status != protocolv2.TurnStatusCompleted || len(result.Run.Notifications) != 1282 {
				failures <- fmt.Sprintf("combined replay/history failed: err=%v thread_match=%t turn_match=%t status=%s notifications=%d", err, result.Start.Thread.ID == identity, result.Run.Turn.ID == "turn-"+identity, result.Run.Turn.Status, len(result.Run.Notifications))
				return
			}
			callbackMu.Lock()
			valid := rawCount == 1281 && typedCount == 1282
			callbackMu.Unlock()
			if !valid {
				failures <- "admitted observations missing or repeated"
				return
			}
			count, bytes := retentionFacts(c.retention)
			if count > 10000 || bytes > 64<<20 {
				failures <- "SDK retention not within finite combined limits"
				return
			}
			closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
			closed, err := c.CloseContext(closeCtx)
			cancelClose()
			if err != nil || !closed.ProcessReaped || closed.CallbackWorkPending != 0 {
				failures <- "combined cleanup incomplete"
				return
			}
			requireFixtureReaped(t, waitFixturePID(t, pidFile))
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if environmentDigest(strings.Join(os.Environ(), "\n")) != parent {
		t.Fatal("combined work mutated parent environment")
	}
}

func TestLifecycleCombinedBlockedHandlerOverflowCloseAndTerminalFirstCause(t *testing.T) {
	for _, winner := range []string{"overflow-count", "overflow-bytes", "close"} {
		t.Run(winner, func(t *testing.T) {
			options, pidFile := lifecycleOptions(t, "combined")
			options.Env = append(options.Env, "CODEXSDK_FIXTURE_EVENTS=128")
			options.NotificationQueueCapacity = 1024
			options.NotificationRetention = NotificationRetentionBudget{MaxCount: 16, MaxBytes: 64 << 10}
			if winner == "overflow-bytes" {
				options.NotificationRetention = NotificationRetentionBudget{MaxCount: 10000, MaxBytes: 32 << 10}
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			private := errors.New("private-late-callback-sentinel")
			options.RawNotifications = &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension}, EnableRawResponseItemExtension: true, Handler: func(context.Context, RawNotification) error {
				once.Do(func() { close(entered) })
				<-release
				return private
			}}
			c, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			gateEntered, gateRelease := make(chan struct{}), make(chan struct{})
			var gateOnce, gateReleaseOnce sync.Once
			t.Cleanup(func() {
				gateReleaseOnce.Do(func() { close(gateRelease) })
				releaseOnce.Do(func() { close(release) })
				_ = c.Close()
			})
			c.testPendingExactNotification = func(n rpcNotification) {
				if n.method == "thread/tokenUsage/updated" {
					gateOnce.Do(func() {
						// Establish the blocked callback before allowing the reader to
						// exhaust retention. Otherwise failure may validly discard the
						// queued raw frame before its handler ever starts.
						select {
						case <-entered:
						case <-gateRelease:
							return
						}
						close(gateEntered)
						if winner == "close" {
							<-gateRelease
						}
					})
				}
			}
			c.testBeforeExactTurnAttach = func() { <-c.closeCausePublished }
			type outcome struct {
				stream *Stream[StartedThreadRun]
				err    error
			}
			started := make(chan outcome, 1)
			pendingRPC := make(chan error, 1)
			go func() { _, err := c.call(context.Background(), "fixture/pending", nil); pendingRPC <- err }()
			waitForPendingCount(t, c, 1)

			go func() {
				stream, err := c.ThreadRunner().StartStream(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
				started <- outcome{stream, err}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("combined fixture did not establish its blocked raw callback")
			}
			expected := ErrNotificationRetention
			if winner == "close" {
				select {
				case <-gateEntered:
				case <-time.After(3 * time.Second):
					t.Fatal("combined fixture did not establish pending typed evidence")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
				closed, err := c.CloseContext(ctx)
				cancel()
				if !errors.Is(err, context.DeadlineExceeded) || closed.CallbackWorkPending < 1 {
					t.Fatal("blocked close did not honor deadline")
				}
				expected = ErrClientClosed
				// A later resource/transport failure cannot replace normal close's cause.
				c.failClient(ErrNotificationRetention)
				gateReleaseOnce.Do(func() { close(gateRelease) })
			} else {
				select {
				case <-c.closeCausePublished:
				case <-time.After(3 * time.Second):
					t.Fatal("overflow failed to settle in-flight attachment")
				}
			}
			var got outcome
			select {
			case got = <-started:
			case <-time.After(time.Second):
				t.Fatal("in-flight start/replay remained deadlocked")
			}
			if got.err != nil || got.stream == nil {
				t.Fatal("accepted run lost its partial stream")
			}
			partial, err := got.stream.Wait(context.Background())
			if !errors.Is(err, expected) || len(partial.Run.Notifications) < 1 {
				t.Fatal("combined failure lost its first cause or partial evidence")
			}
			select {
			case rpcErr := <-pendingRPC:
				if !errors.Is(rpcErr, expected) {
					t.Fatal("combined pending RPC lost first cause")
				}
			case <-time.After(time.Second):
				t.Fatal("combined pending RPC did not settle")
			}
			const closers = 8
			var wg sync.WaitGroup
			for range closers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
					defer cancel()
					closed, err := c.CloseContext(ctx)
					if err == nil || !errors.Is(closed.Cause, expected) || strings.Contains(err.Error(), "private-") {
						t.Error("concurrent shutdown cause unsafe or changed")
					}
				}()
			}
			wg.Wait()
			requireOwnedCleanup(t, c)
			requireFixtureReaped(t, waitFixturePID(t, pidFile))
			count, bytes := retentionFacts(c.retention)
			if count > options.NotificationRetention.MaxCount || bytes > options.NotificationRetention.MaxBytes {
				t.Fatal("blocked combined path retained unbounded evidence")
			}
			late := protocolv2.NewServerNotificationTurnCompleted(protocolv2.ServerNotificationTurnCompleted{Params: protocolv2.TurnCompletedNotification{ThreadID: got.stream.state.threadID, Turn: protocolv2.Turn{ID: got.stream.state.turnIDSnapshot(), Status: protocolv2.TurnStatusCompleted, Items: []protocolv2.ThreadItem{}}}})
			_ = got.stream.state.accept(late)
			releaseOnce.Do(func() { close(release) })
			select {
			case <-c.handlersDone:
			case <-time.After(time.Second):
				t.Fatal("released callback did not settle")
			}
			if !errors.Is(got.stream.Err(), expected) || !errors.Is(c.firstCloseReason(nil), expected) {
				t.Fatal("late callback/terminal weakened first cause")
			}
		})
	}
}

func TestCloseContextReapsWhileTransportWriterIsBlocked(t *testing.T) {
	options, _ := lifecycleOptions(t, "unresponsive")
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	first, large := make(chan error, 1), make(chan error, 1)
	go func() { _, err := c.Models().List(context.Background(), protocolv2.ModelListParams{}); first <- err }()
	waitForPendingCount(t, c, 1)
	// The child stops reading stdin on model/list; this next frame exceeds pipe
	// capacity and must not let writeMu prevent its owned process from being reaped.
	go func() {
		_, err := c.call(context.Background(), "fixture/large", map[string]any{"private": strings.Repeat("private-writer-sentinel", 100000)})
		large <- err
	}()
	waitForPendingCount(t, c, 2)
	deadline := time.Now().Add(time.Second)
	locked := false
	for time.Now().Before(deadline) {
		if c.writeMu.TryLock() {
			c.writeMu.Unlock()
			time.Sleep(time.Millisecond)
		} else {
			locked = true
			break
		}
	}
	if !locked {
		t.Fatal("large transport write did not enter")
	}
	time.Sleep(25 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.CloseContext(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked writer shutdown failed unexpectedly")
	}
	requireOwnedCleanup(t, c)
	for _, done := range []<-chan error{first, large} {
		select {
		case err := <-done:
			if !errors.Is(err, ErrClientClosed) || strings.Contains(err.Error(), "private-writer") {
				t.Fatal("blocked RPC first cause unsafe or changed")
			}
		case <-time.After(time.Second):
			t.Fatal("blocked writer kept an RPC orphaned")
		}
	}
}

func TestCloseContextReportsRawDeliveryFailureDuringNormalDrain(t *testing.T) {
	options, _ := lifecycleOptions(t, "blocked-work")
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	private := errors.New("private-normal-drain-sentinel")
	options.RawNotifications = &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension}, EnableRawResponseItemExtension: true, Handler: func(context.Context, RawNotification) error {
		once.Do(func() { close(entered) })
		<-release
		return private
	}}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); _ = c.Close() })
	stream, err := c.ThreadRunner().StartStream(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	type outcome struct {
		result ShutdownResult
		err    error
	}
	closed := make(chan outcome, 1)
	go func() { result, err := c.CloseContext(context.Background()); closed <- outcome{result, err} }()
	select {
	case <-c.closeCausePublished:
	case <-time.After(time.Second):
		t.Fatal("normal drain did not publish close cause")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case got := <-closed:
		if !errors.Is(got.err, ErrRawNotificationDelivery) || !errors.Is(got.err, private) || strings.Contains(got.err.Error(), "private-normal") || !got.result.ProcessReaped || got.result.CallbackWorkPending != 0 || !errors.Is(got.result.Cause, ErrClientClosed) {
			t.Fatal("normal drain reported success despite raw delivery failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("normal failure drain did not complete")
	}
	if !errors.Is(stream.Err(), ErrClientClosed) {
		t.Fatal("callback failure replaced the first shared close cause")
	}
}
