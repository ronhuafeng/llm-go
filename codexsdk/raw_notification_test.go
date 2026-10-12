package codexsdk

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func rawOptions(t *testing.T, mode string, handler RawNotificationHandler) ClientOptions {
	t.Helper()
	return ClientOptions{CWD: t.TempDir(), Command: fakeCommand(mode), RawNotifications: &RawNotificationSubscription{Methods: []string{RawResponseItemCompletedExtension, "item/completed", "turn/completed"}, EnableRawResponseItemExtension: true, Handler: handler}}
}

func TestRequiredRawNotificationOrderedAcrossAttachment(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "replay"}[replay], func(t *testing.T) {
			t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
			var mu sync.Mutex
			var methods, markers, typedMethods []string
			handler := func(_ context.Context, n RawNotification) error {
				mu.Lock()
				defer mu.Unlock()
				if n.ThreadID != "thread-1" || n.TurnID != "turn-1" {
					return errors.New("incorrect correlation")
				}
				methods = append(methods, n.Method)
				if n.Method == RawResponseItemCompletedExtension {
					envelope, _ := n.Envelope.AsObject()
					number, _ := envelope["emittedAtMs"].AsNumber()
					if number.String() != "9007199254740993" {
						return errors.New("envelope number lost")
					}
					params, _ := envelope["params"].AsObject()
					item, _ := params["item"].AsObject()
					marker, _ := item["marker"].AsString()
					markers = append(markers, marker)
					v, present := item["id"]
					if present != (marker == "during-attach") || (present && v.Kind() != protocolv2.JSONKindNull) {
						return errors.New("presence lost")
					}
					number, _ = item["number"].AsNumber()
					if number.String() != "9007199254740993" {
						return errors.New("item number lost")
					}
				}
				return nil
			}
			options := rawOptions(t, "raw-happy", handler)
			options.ServerNotificationHandler = func(_ context.Context, n protocolv2.ServerNotification) error {
				mu.Lock()
				typedMethods = append(typedMethods, string(n.Kind()))
				mu.Unlock()
				return nil
			}
			c, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if replay {
				ready := make(chan struct{})
				c.testBeforeExactTurnAttach = func() { <-ready }
				var once sync.Once
				c.testPendingExactNotification = func(n rpcNotification) {
					if n.method == "turn/completed" {
						once.Do(func() { close(ready) })
					}
				}
			}
			result, err := c.ThreadRunner().Start(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
			if err != nil || result.Run.Turn.Status != protocolv2.TurnStatusCompleted {
				t.Fatalf("run failed: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(markers, []string{"before-response", "during-attach", "duplicate", "duplicate"}) {
				t.Fatalf("raw markers/order mismatch: count=%d", len(markers))
			}
			expected := []string{RawResponseItemCompletedExtension, RawResponseItemCompletedExtension, RawResponseItemCompletedExtension, RawResponseItemCompletedExtension, "item/completed", "turn/completed"}
			if !reflect.DeepEqual(typedMethods, []string{"item/completed", "thread/tokenUsage/updated", "turn/completed"}) || len(result.Run.Notifications) != 3 {
				t.Fatal("existing typed delivery changed")
			}
			if !reflect.DeepEqual(methods, expected) {
				t.Fatalf("notification order mismatch: count=%d", len(methods))
			}
		})
	}
}

func TestRequiredRawSubscriptionFailsBeforeLaunch(t *testing.T) {
	cases := []*RawNotificationSubscription{
		{Methods: []string{"unknown-secret-method"}, Handler: func(context.Context, RawNotification) error { return nil }},
		{Methods: []string{RawResponseItemCompletedExtension}, Handler: func(context.Context, RawNotification) error { return nil }},
		{Methods: []string{"turn/start"}, Handler: func(context.Context, RawNotification) error { return nil }},
		{Methods: []string{"turn/completed", "turn/completed"}, Handler: func(context.Context, RawNotification) error { return nil }},
		{Methods: []string{"turn/completed"}},
	}
	for _, sub := range cases {
		options := ClientOptions{CWD: t.TempDir(), Command: []string{"must-not-launch"}, RawNotifications: sub}
		_, err := New(options)
		if !errors.Is(err, ErrRawNotificationUnsupported) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe or incorrect unsupported classification")
		}
	}
}

func TestRequiredRawFailuresPreventSuccessfulTerminal(t *testing.T) {
	for _, mode := range []string{"raw-malformed", "raw-error", "raw-panic"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
			sentinel := errors.New("raw-secret-sentinel")
			handler := func(context.Context, RawNotification) error {
				if mode == "raw-error" {
					return sentinel
				}
				if mode == "raw-panic" {
					panic("raw-secret-sentinel")
				}
				return nil
			}
			options := rawOptions(t, mode, handler)
			options.RawNotifications.Methods = []string{RawResponseItemCompletedExtension}
			c, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := c.ThreadRunner().Start(ctx, StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
			expected := ErrRawNotificationDelivery
			if mode == "raw-malformed" {
				expected = ErrRawNotificationDecode
			}
			if !errors.Is(err, expected) {
				t.Fatalf("incorrect failure: %v", err)
			}
			if strings.Contains(err.Error(), "raw-secret-sentinel") {
				t.Fatal("payload entered diagnostics")
			}
			if mode == "raw-error" && !errors.Is(err, sentinel) {
				t.Fatal("handler cause identity lost")
			}
			_ = result // partial observations remain available; an error prevents acceptance.
			if err = c.Close(); !errors.Is(err, expected) {
				t.Fatalf("first cause changed: %v", err)
			}
		})
	}
}

func TestMissingRawEventIsApplicationDecision(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	count := 0
	c, err := New(rawOptions(t, "raw-missing", func(_ context.Context, n RawNotification) error {
		if n.Method == RawResponseItemCompletedExtension {
			count++
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.ThreadRunner().Start(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("SDK invented a missing raw event")
	}
	// An application requiring at least one raw item rejects this observation.
}

func TestRawSubscriptionCopiesCallerInput(t *testing.T) {
	options := rawOptions(t, "raw-happy", func(context.Context, RawNotification) error { return nil })
	normalized, err := validateOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	options.RawNotifications.Methods[0] = "changed"
	options.RawNotifications.Handler = nil
	if normalized.RawNotifications.Methods[0] != RawResponseItemCompletedExtension || normalized.RawNotifications.Handler == nil {
		t.Fatal("subscription aliases caller input")
	}
}

func TestRequiredRawQueueOverflowFailsPendingRun(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	started := t.TempDir() + "/started"
	handler := func(ctx context.Context, _ RawNotification) error {
		if err := os.WriteFile(started, []byte("ready"), 0600); err != nil {
			return errors.New("fixture signal failed")
		}
		<-ctx.Done()
		return nil
	}
	options := rawOptions(t, "raw-overflow", handler)
	options.Command = fakeCommand("raw-overflow", started)
	options.RawNotifications.Methods = []string{RawResponseItemCompletedExtension}
	options.NotificationQueueCapacity = 1
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = c.ThreadRunner().Start(ctx, StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
	if !errors.Is(err, ErrNotificationBackpressure) {
		t.Fatalf("overflow not reported: %v", err)
	}
	if err = c.Close(); !errors.Is(err, ErrNotificationBackpressure) {
		t.Fatalf("first cause changed: %v", err)
	}
}

func TestRequiredRawParallelRunsPreserveCorrelation(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	var mu sync.Mutex
	counts := map[string]int{}
	options := rawOptions(t, "raw-happy", func(_ context.Context, n RawNotification) error {
		if n.Method == RawResponseItemCompletedExtension {
			mu.Lock()
			counts[n.ThreadID+"/"+n.TurnID]++
			mu.Unlock()
		}
		return nil
	})
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	results := make(chan StartedThreadRun, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			r, e := c.ThreadRunner().Start(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
			results <- r
			errs <- e
		}()
	}
	for range 2 {
		r := <-results
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		count := counts[r.Start.Thread.ID+"/"+r.Run.Turn.ID]
		mu.Unlock()
		if count != 4 {
			t.Fatalf("correlation mismatch: count=%d", count)
		}
	}
}

func TestRawNotificationSchemaRejectsMalformedItems(t *testing.T) {
	for _, payload := range []string{
		`null`, `{"item":{},"threadId":"t","turnId":"u"}`,
		`{"item":{"type":"compaction","encrypted_content":"secret"},"threadId":null,"turnId":"u"}`,
		`{"item":{"type":"compaction","encrypted_content":123},"threadId":"t","turnId":"u"}`,
		`{"item":{"type":"unsupported-secret"},"threadId":"t","turnId":"u"}`,
	} {
		var params map[string]any
		if err := unmarshalJSONPreserveNumbers([]byte(payload), &params); err != nil {
			t.Fatal("fixture invalid")
		}
		_, err := rawNotification(rpcNotification{method: RawResponseItemCompletedExtension, params: params})
		if !errors.Is(err, ErrRawNotificationDecode) || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe or incorrect decode classification")
		}
	}
}

func TestUnsubscribedUnknownNotificationsRemainIgnored(t *testing.T) {
	c := newTransportHarness()
	defer c.Close()
	c.routeNotification(rpcNotification{method: RawResponseItemCompletedExtension, params: map[string]any{"item": "malformed"}})
	if c.isClosed() {
		t.Fatal("unsubscribed unknown event changed compatibility policy")
	}
}

func TestRequiredKnownRawNotificationStillValidatesTypedPayload(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	options := rawOptions(t, "hang", func(context.Context, RawNotification) error { return errors.New("must not dispatch") })
	options.RawNotifications.Methods = []string{"item/completed"}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.routeNotification(rpcNotification{method: "item/completed", params: map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"type": "unknown-secret"}}})
	if err = c.Close(); !errors.Is(err, ErrRawNotificationDecode) || strings.Contains(err.Error(), "unknown-secret") {
		t.Fatal("required typed validation did not fail safely")
	}
}

func TestRawOnlyObserverFencesExactSuccessfulCompletion(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	options := rawOptions(t, "raw-happy", func(context.Context, RawNotification) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	})
	options.RawNotifications.Methods = []string{RawResponseItemCompletedExtension}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	terminalSeen := make(chan struct{})
	c.testBeforeExactTurnAttach = func() { <-terminalSeen }
	c.testPendingExactNotification = func(n rpcNotification) {
		if n.method == "turn/completed" {
			close(terminalSeen)
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.ThreadRunner().Start(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
		done <- err
	}()
	<-entered
	<-terminalSeen
	select {
	case <-done:
		close(release)
		t.Fatal("terminal passed blocked raw observer")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("terminal did not settle")
	}
}

func TestRawObserverWorksWithDirectTypedFacades(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	var mu sync.Mutex
	count := 0
	options := rawOptions(t, "raw-happy", func(_ context.Context, n RawNotification) error {
		if n.Method == RawResponseItemCompletedExtension {
			mu.Lock()
			count++
			mu.Unlock()
		}
		return nil
	})
	options.RawNotifications.Methods = []string{RawResponseItemCompletedExtension}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	thread, err := c.Threads().Start(context.Background(), protocolv2.ThreadStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := c.Turns().Start(context.Background(), protocolv2.TurnStartParams{ThreadID: thread.Thread.ID, Input: []protocolv2.UserInput{}})
	if err != nil {
		t.Fatal(err)
	}
	if turn.Turn.ID == "" {
		t.Fatal("typed turn response lost")
	}
	// Close drains admitted callbacks. Wait for a final raw callback before closing
	// so the server has emitted every required raw item in this fixture.
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := count
		mu.Unlock()
		if n == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("direct facade raw delivery incomplete")
		}
		time.Sleep(time.Millisecond)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredRawNotificationAfterTerminalRemainsClientScoped(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	observed := make(chan RawNotification, 8)
	options := rawOptions(t, "raw-happy", func(_ context.Context, n RawNotification) error { observed <- n; return nil })
	options.RawNotifications.Methods = []string{RawResponseItemCompletedExtension}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	result, err := c.ThreadRunner().Start(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		<-observed
	}
	c.routeNotification(rpcNotification{method: RawResponseItemCompletedExtension, params: map[string]any{"threadId": result.Start.Thread.ID, "turnId": result.Run.Turn.ID, "item": map[string]any{"type": "other"}}})
	select {
	case n := <-observed:
		if n.ThreadID != result.Start.Thread.ID || n.TurnID != result.Run.Turn.ID {
			t.Fatal("late correlation changed")
		}
	case <-time.After(time.Second):
		t.Fatal("late raw notification silently lost")
	}
	if len(result.Run.Notifications) != 3 {
		t.Fatal("late raw notification rewrote typed terminal snapshot")
	}
}

func TestRawKnownNotificationExtractsNestedIdentity(t *testing.T) {
	n, err := rawNotification(rpcNotification{method: "thread/started", params: map[string]any{"thread": map[string]any{"id": "nested-thread"}}})
	if err != nil || n.ThreadID != "nested-thread" || n.TurnID != "" {
		t.Fatal("nested thread correlation lost")
	}
}

func TestRawFailurePublishesWhileAttachmentOwnsReplayOrder(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	pendingTerminal, attachmentLocked, releaseAttachment := make(chan struct{}), make(chan struct{}), make(chan struct{})
	failure := errors.New("private-raw-callback-cause")
	options := rawOptions(t, "raw-happy", func(context.Context, RawNotification) error { <-attachmentLocked; return failure })
	options.RawNotifications.Methods = []string{RawResponseItemCompletedExtension}
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.testBeforeExactTurnAttach = func() { <-pendingTerminal }
	c.testPendingExactNotification = func(n rpcNotification) {
		if n.method == "turn/completed" {
			close(pendingTerminal)
		}
	}
	c.testAfterExactTurnPublished = func() { close(attachmentLocked); <-releaseAttachment }
	type outcome struct {
		result StartedThreadRun
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		r, e := c.ThreadRunner().Start(context.Background(), StartThreadRunRequest{Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{}}})
		done <- outcome{r, e}
	}()
	<-attachmentLocked
	select {
	case <-c.closeCausePublished:
	case <-time.After(time.Second):
		close(releaseAttachment)
		t.Fatal("raw failure publication waits on attachment replay lock")
	}
	close(releaseAttachment)
	select {
	case got := <-done:
		if !errors.Is(got.err, ErrRawNotificationDelivery) || !errors.Is(got.err, failure) {
			t.Fatalf("first cause lost: %v", got.err)
		}
		if len(got.result.Run.Notifications) != 3 {
			t.Fatal("failure lost accepted pending typed evidence")
		}
	case <-time.After(time.Second):
		t.Fatal("failed pending run did not settle")
	}
	if err = c.Close(); !errors.Is(err, failure) {
		t.Fatal("close changed the first cause")
	}
}
