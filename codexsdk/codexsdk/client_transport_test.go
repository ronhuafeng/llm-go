package codexsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInternalJSONRPCRoutesConcurrentResponsesByID(t *testing.T) {
	c := newTransportHarness()
	first := make(chan map[string]any, 1)
	second := make(chan map[string]any, 1)
	errs := make(chan error, 2)

	go func() {
		result, err := c.call(context.Background(), "first/method", map[string]any{"value": "first"})
		if err != nil {
			errs <- err
			return
		}
		first <- result
	}()
	waitForPendingCount(t, c, 1)
	go func() {
		result, err := c.call(context.Background(), "second/method", map[string]any{"value": "second"})
		if err != nil {
			errs <- err
			return
		}
		second <- result
	}()

	waitForPendingCount(t, c, 2)
	c.routeResponse(map[string]any{"id": "go-sdk-2", "result": map[string]any{"seen": "second"}})
	c.routeResponse(map[string]any{"id": "go-sdk-1", "result": map[string]any{"seen": "first"}})

	select {
	case err := <-errs:
		t.Fatalf("unexpected call error: %v", err)
	case result := <-first:
		if result["seen"] != "first" {
			t.Fatalf("first result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first result")
	}
	select {
	case err := <-errs:
		t.Fatalf("unexpected call error: %v", err)
	case result := <-second:
		if result["seen"] != "second" {
			t.Fatalf("second result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second result")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
}

func TestInternalJSONRPCCancellationReleasesPendingWait(t *testing.T) {
	c := newTransportHarness()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		_, err := c.call(ctx, "slow/method", map[string]any{"value": "slow"})
		errCh <- err
	}()

	waitForPendingCount(t, c, 1)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("call error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled call")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}

	c.routeResponse(map[string]any{"id": "go-sdk-1", "result": map[string]any{"late": true}})
	if got := pendingCount(c); got != 0 {
		t.Fatalf("late response recreated pending state; count = %d", got)
	}
}

func TestInternalJSONRPCCloseFailsPendingWaits(t *testing.T) {
	c := newTransportHarness()
	errCh := make(chan error, 1)

	go func() {
		_, err := c.call(context.Background(), "slow/method", map[string]any{"value": "slow"})
		errCh <- err
	}()

	waitForPendingCount(t, c, 1)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrClientClosed) {
			t.Fatalf("call error = %v, want ErrClientClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for close to fail pending call")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
	if _, err := c.call(context.Background(), "after/close", nil); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("post-close call error = %v, want ErrClientClosed", err)
	}
}

func TestInternalJSONRPCProtocolErrorIsPropagated(t *testing.T) {
	c := newTransportHarness()
	errCh := make(chan error, 1)

	go func() {
		_, err := c.call(context.Background(), "failing/method", map[string]any{"value": "fail"})
		errCh <- err
	}()

	waitForPendingCount(t, c, 1)
	c.routeResponse(map[string]any{
		"id": "go-sdk-1",
		"error": map[string]any{
			"code":    -32602,
			"message": "invalid params",
		},
	})

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("call succeeded; want app-server protocol error")
		}
		if !strings.Contains(err.Error(), "app-server error") ||
			!strings.Contains(err.Error(), "-32602") ||
			!strings.Contains(err.Error(), "invalid params") {
			t.Fatalf("protocol error was not propagated with native facts: %v", err)
		}
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) || protocolErr.Method != "failing/method" || protocolErr.Code != -32602 {
			t.Fatalf("protocol error detail = %#v", protocolErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol error")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
}

func TestInternalJSONRPCWriterSerializesConcurrentWrites(t *testing.T) {
	writer := &overlapDetectingWriteCloser{}
	c := newTransportHarness()
	c.stdin = writer

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- c.write(map[string]any{"method": "write/test", "params": map[string]any{"value": "ok"}})
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("write error = %v", err)
		}
	}
	if writer.overlap.Load() {
		t.Fatal("writer observed overlapping stdio writes")
	}
}

func TestInternalJSONRPCRejectsMissingObjectResult(t *testing.T) {
	c := newTransportHarness()
	errCh := make(chan error, 1)

	go func() {
		_, err := c.call(context.Background(), "bad/result", map[string]any{"value": "bad"})
		errCh <- err
	}()

	waitForPendingCount(t, c, 1)
	c.routeResponse(map[string]any{"id": "go-sdk-1", "result": "not-an-object"})

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("call succeeded; want missing object result error")
		}
		if !strings.Contains(err.Error(), "response missing object result") {
			t.Fatalf("missing object result error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for missing object result error")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
}

func TestInternalJSONRPCReadLoopMalformedLineFailsPendingWaits(t *testing.T) {
	c := newTransportHarness()
	errCh := make(chan error, 1)

	go func() {
		_, err := c.call(context.Background(), "read/malformed", map[string]any{"value": "malformed"})
		errCh <- err
	}()

	waitForPendingCount(t, c, 1)
	c.readLoop(strings.NewReader("{not-json stdout_secret transcript\n"))

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("call succeeded; want malformed stdout error")
		}
		if !strings.Contains(err.Error(), "invalid app-server JSON-RPC line bytes=") ||
			!strings.Contains(err.Error(), "sha256=") {
			t.Fatalf("malformed stdout error missing sanitized facts: %v", err)
		}
		for _, forbidden := range []string{"stdout_secret", "transcript", "{not-json"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("malformed stdout error leaked raw content %q: %v", forbidden, err)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for malformed stdout error")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
}

func TestInternalJSONRPCReadLoopInvalidEnvelopeFailsPendingWaits(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		message string
	}{
		{
			name:    "ambiguous request response",
			line:    `{"id":"go-sdk-1","method":"secret/method","result":{"value":"stdout_secret"}}` + "\n",
			message: "expected exactly one JSON-RPC envelope shape",
		},
		{
			name:    "duplicate top-level key",
			line:    `{"id":"go-sdk-1","id":"go-sdk-1","result":{"ok":true}}` + "\n",
			message: "duplicate object key",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTransportHarness()
			errCh := make(chan error, 1)

			go func() {
				_, err := c.call(context.Background(), "read/envelope", map[string]any{"value": "envelope"})
				errCh <- err
			}()

			waitForPendingCount(t, c, 1)
			c.readLoop(strings.NewReader(tc.line))

			select {
			case err := <-errCh:
				if err == nil {
					t.Fatal("call succeeded; want invalid envelope error")
				}
				if !strings.Contains(err.Error(), "invalid app-server JSON-RPC line bytes=") ||
					!strings.Contains(err.Error(), "sha256=") ||
					!strings.Contains(err.Error(), tc.message) {
					t.Fatalf("invalid envelope error = %v", err)
				}
				for _, forbidden := range []string{"stdout_secret", "secret/method"} {
					if strings.Contains(err.Error(), forbidden) {
						t.Fatalf("invalid envelope error leaked raw content %q: %v", forbidden, err)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for invalid envelope error")
			}
			if got := pendingCount(c); got != 0 {
				t.Fatalf("pending count = %d, want 0", got)
			}
		})
	}
}

func TestInternalJSONRPCReadLoopRoutesTypedResponseByID(t *testing.T) {
	c := newTransportHarness()
	errCh := make(chan error, 1)
	resultCh := make(chan map[string]any, 1)

	go func() {
		result, err := c.call(context.Background(), "read/response", map[string]any{"value": "response"})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	waitForPendingCount(t, c, 1)
	c.readLoop(strings.NewReader(`{"id":"go-sdk-1","result":{"seen":"typed"}}` + "\n"))

	select {
	case err := <-errCh:
		t.Fatalf("unexpected call error: %v", err)
	case result := <-resultCh:
		if result["seen"] != "typed" {
			t.Fatalf("response result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for typed response")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
}

func TestInternalJSONRPCReadLoopEOFFailsPendingWaits(t *testing.T) {
	c := newTransportHarness()
	errCh := make(chan error, 1)

	go func() {
		_, err := c.call(context.Background(), "read/eof", map[string]any{"value": "eof"})
		errCh <- err
	}()

	waitForPendingCount(t, c, 1)
	c.readLoop(strings.NewReader(""))

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("call succeeded; want stdout close error")
		}
		if !strings.Contains(err.Error(), "app-server closed stdout") {
			t.Fatalf("stdout close error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stdout close error")
	}
	if got := pendingCount(c); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
}

func newTransportHarness() *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		ctx:            ctx,
		cancel:         cancel,
		stdin:          &recordingWriteCloser{},
		pendingEvents:  map[string][]rpcNotification{},
		exactStreams:   map[string]map[*exactRunState]struct{}{},
		exactAttaching: map[string]map[*exactRunState]struct{}{},
		readerDone:     make(chan struct{}),
	}
}

type overlapDetectingWriteCloser struct {
	active  atomic.Int32
	overlap atomic.Bool
}

func (w *overlapDetectingWriteCloser) Write(p []byte) (int, error) {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	time.Sleep(time.Millisecond)
	w.active.Add(-1)
	return len(p), nil
}

func (w *overlapDetectingWriteCloser) Close() error {
	return nil
}

func waitForPendingCount(t *testing.T, c *Client, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := pendingCount(c); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pending count = %d, want %d", pendingCount(c), want)
}

func pendingCount(c *Client) int {
	count := 0
	c.pending.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func recordedJSONMessage(t *testing.T, writer *recordingWriteCloser) map[string]any {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw := bytes.TrimSpace(writer.Bytes())
		if len(raw) > 0 {
			var message map[string]any
			if err := json.Unmarshal(raw, &message); err != nil {
				t.Fatalf("recorded JSON-RPC message = %q: %v", writer.String(), err)
			}
			return message
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for recorded JSON-RPC message")
	return nil
}
