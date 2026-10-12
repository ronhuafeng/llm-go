package codexsdk

import (
	"context"
	"fmt"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func (c *Client) enqueueNotification(notification protocolv2.ServerNotification, evidence *notificationEvidence, waitForDispatch bool) (<-chan struct{}, error) {
	return c.enqueueAcceptedNotification(acceptedNotification{notification: notification, evidence: evidence}, waitForDispatch)
}

func (c *Client) enqueueAcceptedNotification(accepted acceptedNotification, waitForDispatch bool) (<-chan struct{}, error) {
	c.closeMu.Lock()
	hasHandler := c.hasNotificationHandler()
	evidence := accepted.evidence
	var dispatched chan struct{}
	if evidence != nil {
		dispatched = evidence.dispatched
	} else if hasHandler && waitForDispatch {
		dispatched = make(chan struct{})
	}
	accepted.dispatched = dispatched
	if c.closed {
		c.closeMu.Unlock()
		c.awaitCloseCausePublication()
		accepted.releaseDispatchWaiter()
		return nil, nil
	}
	count, size := 0, int64(0)
	if accepted.notification.Kind() != "" {
		count++
		size += retainedValueBytes(accepted.notification)
	}
	if accepted.raw != nil {
		count++
		size += retainedValueBytes(*accepted.raw)
	}
	retained, err := c.retention.reserve(count, size)
	if err != nil {
		c.closeMu.Unlock()
		c.failClient(err)
		c.awaitCloseCausePublication()
		accepted.releaseDispatchWaiter()
		return nil, err
	}
	accepted.retained = retained
	if hasHandler {
		c.addHandlerWork()
	}
	select {
	case c.notifications <- accepted:
		c.closeMu.Unlock()
		return dispatched, nil
	default:
		accepted.retained.release()
		accepted.retained = nil
		if hasHandler {
			c.endHandlerWork()
		}
		c.closeMu.Unlock()
		c.failClient(ErrNotificationBackpressure)
		c.awaitCloseCausePublication()
		accepted.releaseDispatchWaiter()
		return nil, ErrNotificationBackpressure
	}
}

// releaseDispatchWaiter owns the dispatch fence only until queue admission.
// A successful send transfers that ownership to notificationDispatcher; every
// rejected send must release it here so an exact terminal replay cannot wait
// for handler work that the dispatcher never received.
func (accepted acceptedNotification) releaseDispatchWaiter() {
	if accepted.dispatched != nil {
		close(accepted.dispatched)
	}
	accepted.retained.release()
}

func (c *Client) awaitCloseCausePublication() {
	if c.closeCausePublished != nil {
		<-c.closeCausePublished
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
				if c.testBeforeEvidenceWait != nil {
					c.testBeforeEvidenceWait()
				}
				select {
				case <-accepted.evidence.ready:
				case <-c.ctx.Done():
					c.discardCurrentAndQueuedNotifications(accepted)
					return
				case <-c.dispatchStop:
					c.finishDispatchStop(handler, &accepted)
					return
				}
			}
			if c.ctx.Err() != nil {
				c.discardCurrentAndQueuedNotifications(accepted)
				return
			}
			if c.hasNotificationHandler() {
				if !c.dispatchAcceptedNotification(handler, accepted) {
					return
				}
			} else {
				accepted.releaseDispatchWaiter()
			}
		case <-c.dispatchStop:
			c.finishDispatchStop(handler, nil)
			return
		case <-c.ctx.Done():
			c.discardAcceptedNotifications()
			return
		}
	}
}

func (c *Client) finishDispatchStop(handler ServerNotificationHandler, current *acceptedNotification) {
	if current != nil {
		current.evidence.releaseReady()
		if c.hasNotificationHandler() {
			if !c.dispatchAcceptedNotification(handler, *current) {
				return
			}
		} else {
			current.releaseDispatchWaiter()
		}
	}
	for {
		select {
		case accepted := <-c.notifications:
			if c.hasNotificationHandler() && !c.dispatchAcceptedNotification(handler, accepted) {
				return
			}
			if !c.hasNotificationHandler() {
				accepted.releaseDispatchWaiter()
			}
		default:
			return
		}
	}
}

func (c *Client) discardCurrentAndQueuedNotifications(accepted acceptedNotification) {
	c.endNotificationHandler()
	accepted.releaseDispatchWaiter()
	c.discardAcceptedNotifications()
}

func (c *Client) dispatchAcceptedNotification(handler ServerNotificationHandler, accepted acceptedNotification) bool {
	defer c.endNotificationHandler()
	var err error
	if accepted.raw != nil {
		err = invokeRawNotificationHandler(c.ctx, c.options.RawNotifications.Handler, *accepted.raw)
	}
	if err == nil && handler != nil && accepted.notification.Kind() != "" {
		err = invokeNotificationHandler(c.ctx, handler, accepted.notification)
	}
	if err == nil {
		accepted.releaseDispatchWaiter()
		return true
	}
	c.closeMu.Lock()
	if c.normalClosing && c.callbackFailure == nil {
		c.callbackFailure = err
	}
	c.closeMu.Unlock()
	c.failClient(err)
	c.discardAcceptedNotifications()
	accepted.releaseDispatchWaiter()
	return false
}

func (c *Client) beginHandler() (context.Context, bool) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil, false
	}
	c.addHandlerWork()
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
	c.endHandlerWork()
}

func (c *Client) endNotificationHandler() {
	if c.hasNotificationHandler() {
		c.endHandlerWork()
	}
}

func (c *Client) discardAcceptedNotifications() {
	for {
		select {
		case accepted := <-c.notifications:
			if c.hasNotificationHandler() {
				c.endHandlerWork()
			}
			accepted.releaseDispatchWaiter()
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
	cancel, claimed := c.claimClientFailure(err)
	if !claimed {
		return
	}
	if cancel != nil {
		cancel()
	}
	c.publishClaimedClientFailure(err)
	c.startClientFailureTeardown()
}

// claimClientFailure closes callback admission and gives one caller ownership
// of first-cause publication and teardown.
func (c *Client) claimClientFailure(err error) (context.CancelFunc, bool) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.claimFailureLocked(err)
}
func (c *Client) claimStartupFailure(err error) (context.CancelFunc, bool) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if !c.constructing {
		return nil, false
	}
	return c.claimFailureLocked(err)
}
func (c *Client) claimFailureLocked(err error) (context.CancelFunc, bool) {
	if err == nil || c.closed {
		return nil, false
	}
	c.failure = err
	c.closeReason = err
	c.closed = true
	return c.cancel, true
}

func (c *Client) publishClaimedClientFailure(err error) {
	c.failAll(err)
	c.publishCloseCause()
}

func (c *Client) startClientFailureTeardown() {
	c.beginShutdown()
}

func (c *Client) publishCloseCause() {
	c.closeCauseOnce.Do(func() {
		if c.closeCausePublished != nil {
			close(c.closeCausePublished)
		}
		if c.testAfterCloseCausePublished != nil {
			c.testAfterCloseCausePublished()
		}
	})
}

func (c *Client) shutdown() {
	c.closeMu.Lock()
	failure := c.failure
	c.closeMu.Unlock()
	// Owned process cleanup starts independently of evidence and callback joins.
	go c.cleanupProcess()
	if failure == nil {
		c.failAll(ErrClientClosed)
		c.publishCloseCause()
		if c.dispatchStop != nil {
			close(c.dispatchStop)
		}
	} else if c.cancel != nil {
		c.cancel()
	}
	if c.handlerCancel != nil {
		c.handlerCancel()
	}
	go func() {
		if c.dispatcherDone != nil {
			<-c.dispatcherDone
		}
		c.handlerWG.Wait()
		close(c.handlersDone)
	}()
	// The grace is finite even for CloseContext with an unbounded context.
	timer := time.NewTimer(callbackShutdownGrace)
	defer timer.Stop()
	select {
	case <-c.handlersDone:
	case <-timer.C:
	case <-c.ctx.Done():
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.discardAcceptedNotifications()
	<-c.processDone
	// Uncooperative callbacks remain unresolved. Do not wait for them here.
	close(c.shutdownDone)
}
