package codexsdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func (c *Client) handleExactServerRequest(id any, request protocolv2.ServerRequest) {
	ctx, ok := c.beginHandler()
	if !ok {
		c.rejectExactServerRequestAfterAdmissionClosed(id, request)
		return
	}
	go func() {
		defer c.endHandler()
		c.respondToExactServerRequest(ctx, id, request)
	}()
}

func (c *Client) respondToExactServerRequest(ctx context.Context, id any, request protocolv2.ServerRequest) {
	if c.options.ServerRequestHandler == nil {
		c.failExactServerRequest(id, -32000, unhandledExactServerRequest(request.Kind()))
		return
	}
	response, err := invokeExactServerRequestHandler(ctx, c.options.ServerRequestHandler, request)
	if err != nil {
		if c.closingNormally() {
			return
		}
		c.failExactServerRequest(id, -32000, err)
		return
	}
	if response.kind != request.Kind() || response.value == nil {
		failure := &ExactServerRequestError{Kind: request.Kind(), Reason: fmt.Sprintf("received mismatched or empty response %s", response.kind)}
		c.failExactServerRequest(id, -32602, failure)
		return
	}
	if err := c.writeExactServerRequestResponse(id, request, response); err != nil {
		c.failClient(err)
	}
}

func unhandledExactServerRequest(kind protocolv2.ServerRequestKind) *ExactServerRequestError {
	return &ExactServerRequestError{
		Kind:   kind,
		Reason: "no server request handler is configured",
	}
}

func (c *Client) failExactServerRequest(id any, code int, failure error) {
	cancel, claimed := c.claimClientFailure(failure)
	if claimed {
		// Publish the typed first cause before the peer can react to the
		// fail-closed response with a successful terminal notification.
		c.publishClaimedClientFailure(failure)
	}
	c.writeServerRequestError(id, code, failure)
	if c.testAfterServerRequestFailureResponse != nil {
		c.testAfterServerRequestFailureResponse()
	}
	if claimed {
		if cancel != nil {
			cancel()
		}
		c.startClientFailureTeardown()
	}
}

func (c *Client) rejectExactServerRequestAfterAdmissionClosed(id any, request protocolv2.ServerRequest) {
	c.writeServerRequestError(id, -32000, &ExactServerRequestError{
		Kind:   request.Kind(),
		Reason: "callback admission is closed",
	})
}

func (c *Client) writeExactServerRequestResponse(id any, request protocolv2.ServerRequest, response ServerRequestResponse) error {
	raw, err := json.Marshal(response.value)
	if err != nil {
		failure := fmt.Errorf("codexsdk: encode %s response: %w", request.Kind(), err)
		c.writeServerRequestError(id, -32602, failure)
		return failure
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		failure := fmt.Errorf("codexsdk: decode %s response object: %w", request.Kind(), err)
		c.writeServerRequestError(id, -32602, failure)
		return failure
	}
	return c.write(map[string]any{"id": id, "result": result})
}

func (c *Client) closingNormally() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.normalClosing && c.failure == nil
}

func invokeExactServerRequestHandler(ctx context.Context, handler ServerRequestHandler, request protocolv2.ServerRequest) (response ServerRequestResponse, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: server request handler panic: %v", ErrHandlerFailed, recovered)
		}
	}()
	response, err = handler(ctx, request)
	if err != nil {
		return ServerRequestResponse{}, fmt.Errorf("%w: %w", ErrHandlerFailed, err)
	}
	return response, nil
}
