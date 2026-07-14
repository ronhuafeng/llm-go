package codexsdk

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func (c *Client) enqueueNotification(notification protocolv2.ServerNotification, evidence *notificationEvidence, waitForDispatch bool) (<-chan struct{}, error) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil, nil
	}
	hasHandler := c.options.ServerNotificationHandler != nil
	var dispatched chan struct{}
	if evidence != nil {
		dispatched = evidence.dispatched
	} else if hasHandler && waitForDispatch {
		dispatched = make(chan struct{})
	}
	accepted := acceptedNotification{notification: notification, evidence: evidence, dispatched: dispatched}
	if hasHandler {
		c.handlerWG.Add(1)
	}
	select {
	case c.notifications <- accepted:
		return dispatched, nil
	default:
		if hasHandler {
			c.handlerWG.Done()
		}
		return nil, ErrNotificationBackpressure
	}
}

func (c *Client) notificationDispatcher() {
	defer close(c.dispatcherDone)
	handler := c.options.ServerNotificationHandler
	for {
		select {
		case accepted := <-c.notifications:
			if c.ctx.Err() != nil {
				c.discardCurrentAndQueuedNotifications(accepted)
				return
			}
			if accepted.evidence != nil {
				select {
				case <-accepted.evidence.ready:
				case <-c.ctx.Done():
					c.discardCurrentAndQueuedNotifications(accepted)
					return
				}
			}
			if c.ctx.Err() != nil {
				c.discardCurrentAndQueuedNotifications(accepted)
				return
			}
			if handler != nil && !c.dispatchAcceptedNotification(handler, accepted) {
				return
			}
		case <-c.dispatchStop:
			for {
				select {
				case accepted := <-c.notifications:
					if handler != nil && !c.dispatchAcceptedNotification(handler, accepted) {
						return
					}
				default:
					return
				}
			}
		case <-c.ctx.Done():
			c.discardAcceptedNotifications()
			return
		}
	}
}

func (c *Client) discardCurrentAndQueuedNotifications(accepted acceptedNotification) {
	c.endNotificationHandler()
	if accepted.dispatched != nil {
		close(accepted.dispatched)
	}
	c.discardAcceptedNotifications()
}

func (c *Client) dispatchAcceptedNotification(handler ServerNotificationHandler, accepted acceptedNotification) bool {
	err := invokeNotificationHandler(c.ctx, handler, accepted.notification)
	c.endNotificationHandler()
	if err == nil {
		if accepted.dispatched != nil {
			close(accepted.dispatched)
		}
		return true
	}
	c.failClient(err)
	c.discardAcceptedNotifications()
	if accepted.dispatched != nil {
		close(accepted.dispatched)
	}
	return false
}

func (c *Client) beginHandler() (context.Context, bool) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil, false
	}
	c.handlerWG.Add(1)
	return c.callbackContext(), true
}

// callbackContext returns the immutable client-scoped callback context.
// Callers that admit new work must still hold closeMu while adding its WG count.
func (c *Client) callbackContext() context.Context {
	if c.handlerCtx != nil {
		return c.handlerCtx
	}
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

func (c *Client) endHandler() {
	c.handlerWG.Done()
}

func (c *Client) endNotificationHandler() {
	if c.options.ServerNotificationHandler != nil {
		c.handlerWG.Done()
	}
}

func (c *Client) discardAcceptedNotifications() {
	if c.options.ServerNotificationHandler == nil {
		return
	}
	for {
		select {
		case accepted := <-c.notifications:
			c.handlerWG.Done()
			if accepted.dispatched != nil {
				close(accepted.dispatched)
			}
		default:
			return
		}
	}
}

func invokeNotificationHandler(ctx context.Context, handler ServerNotificationHandler, notification protocolv2.ServerNotification) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: notification handler panic: %v", ErrHandlerFailed, recovered)
		}
	}()
	if err := handler(ctx, notification); err != nil {
		return fmt.Errorf("%w: %w", ErrHandlerFailed, err)
	}
	return nil
}

func (c *Client) failClient(err error) {
	if err == nil {
		return
	}
	c.closeMu.Lock()
	if c.failure != nil {
		c.closeMu.Unlock()
		return
	}
	c.failure = err
	c.closed = true
	cancel := c.cancel
	c.closeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.failAll(err)
	go func() { _ = c.Close() }()
}

func (c *Client) shutdown() {
	c.closeMu.Lock()
	failure := c.failure
	c.closed = true
	c.normalClosing = failure == nil
	c.closeMu.Unlock()

	if failure == nil {
		c.failAll(ErrClientClosed)
		if c.dispatchStop != nil {
			close(c.dispatchStop)
		}
	} else if c.cancel != nil {
		c.cancel()
	}
	if c.handlerCancel != nil {
		c.handlerCancel()
	}
	if c.dispatcherDone != nil {
		<-c.dispatcherDone
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.handlerWG.Wait()
	c.writeMu.Lock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	c.writeMu.Unlock()
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = c.cmd.Process.Kill()
			<-done
		}
	}
}
