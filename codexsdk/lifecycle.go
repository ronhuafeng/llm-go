package codexsdk

import (
	"context"
	"errors"
	"os"
	"time"
)

const (
	DefaultStartupTimeout  = 15 * time.Second
	DefaultShutdownTimeout = 5 * time.Second
	callbackShutdownGrace  = 2 * time.Second
	processShutdownGrace   = 2 * time.Second
)

var ErrCallbackWorkPending = errors.New("codexsdk: callback work remains unresolved")

// ShutdownResult distinguishes owned-process cleanup from admitted callback work.
// CallbackWorkPending includes executing callbacks and queued callback entries.
// Cause is the first client failure, or ErrClientClosed for normal closure.
type ShutdownResult struct {
	ProcessReaped       bool
	CallbackWorkPending int64
	Cause               error
}

// ShutdownError reports incomplete teardown or a client/callback failure without
// formatting caller-owned errors. Unwrap preserves cause and deadline identities.
type ShutdownError struct {
	Result ShutdownResult
	Err    error
}

func (e *ShutdownError) Error() string { return "codexsdk: shutdown did not complete successfully" }
func (e *ShutdownError) Unwrap() []error {
	if e == nil {
		return nil
	}
	causes := []error{}
	if e.Err != nil {
		causes = append(causes, e.Err)
	}
	if e.Result.Cause != nil {
		causes = append(causes, e.Result.Cause)
	}
	if e.Result.CallbackWorkPending > 0 {
		causes = append(causes, ErrCallbackWorkPending)
	}
	return causes
}

// CloseContext closes shared client admission and bounds this caller's wait.
// Owned process cleanup continues independently if ctx expires. A Go callback
// ignoring cancellation remains pending until it returns; it is never killed.
func (c *Client) CloseContext(ctx context.Context) (ShutdownResult, error) {
	if ctx == nil {
		return ShutdownResult{}, errors.New("codexsdk: shutdown context is required")
	}
	if c == nil || c.ctx == nil {
		return ShutdownResult{ProcessReaped: true}, nil
	}
	c.beginShutdown()
	select {
	case <-c.shutdownDone:
		result := c.shutdownSnapshot()
		c.closeMu.Lock()
		failure, callbackFailure := c.failure, c.callbackFailure
		c.closeMu.Unlock()
		if result.CallbackWorkPending > 0 {
			return result, &ShutdownError{Result: result, Err: ErrCallbackWorkPending}
		}
		if failure != nil {
			return result, failure
		}
		if callbackFailure != nil {
			return result, &ShutdownError{Result: result, Err: callbackFailure}
		}
		return result, nil
	case <-ctx.Done():
		if c.cancel != nil {
			c.cancel()
		}
		if c.handlerCancel != nil {
			c.handlerCancel()
		}
		result := c.shutdownSnapshot()
		return result, &ShutdownError{Result: result, Err: ctx.Err()}
	}
}

func (c *Client) beginShutdown() {
	c.shutdownOnce.Do(func() {
		c.closeMu.Lock()
		c.closed = true
		c.normalClosing = c.failure == nil
		if c.closeReason == nil {
			c.closeReason = ErrClientClosed
		}
		c.shutdownDone = make(chan struct{})
		c.handlersDone = make(chan struct{})
		c.processDone = make(chan struct{})
		c.closeMu.Unlock()
		go c.shutdown()
	})
}
func (c *Client) firstCloseReason(fallback error) error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closeReason != nil {
		return c.closeReason
	}
	if c.failure != nil {
		return c.failure
	}
	return fallback
}
func (c *Client) shutdownSnapshot() ShutdownResult {
	result := ShutdownResult{CallbackWorkPending: c.callbackWork.Load(), Cause: c.firstCloseReason(ErrClientClosed)}
	select {
	case <-c.processDone:
		result.ProcessReaped = true
	default:
	}
	return result
}
func (c *Client) addHandlerWork() { c.handlerWG.Add(1); c.callbackWork.Add(1) }
func (c *Client) endHandlerWork() { c.callbackWork.Add(-1); c.handlerWG.Done() }

func (c *Client) cleanupProcess() {
	defer close(c.processDone)
	if c.startupDone != nil {
		<-c.startupDone
	}
	// Normal closure permits already admitted callbacks and protocol responses to
	// finish, but neither callback work nor writeMu can hold process cleanup forever.
	timer := time.NewTimer(callbackShutdownGrace)
	select {
	case <-c.handlersDone:
	case <-c.ctx.Done():
	case <-timer.C:
	}
	timer.Stop()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
	}
	// os/exec pipes support Close concurrent with a blocked Write. Holding writeMu
	// here would let an unresponsive child prevent its own termination.
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		done := make(chan struct{})
		go func() { _ = c.cmd.Wait(); close(done) }()
		timer := time.NewTimer(processShutdownGrace)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			_ = c.cmd.Process.Kill()
			<-done
		}
	}
}
