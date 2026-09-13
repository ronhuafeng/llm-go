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
		c.failExactServerRequest(id, request, -32000, unhandledExactServerRequest(request.Kind()))
		return
	}
	response, err := invokeExactServerRequestHandler(ctx, c.options.ServerRequestHandler, request)
	if err != nil {
		if c.closingNormally() {
			return
		}
		c.failExactServerRequest(id, request, -32000, err)
		return
	}
	if response.kind != request.Kind() || response.value == nil {
		failure := &ExactServerRequestError{Kind: request.Kind(), Reason: fmt.Sprintf("received mismatched or empty response %s", response.kind)}
		c.failExactServerRequest(id, request, -32602, failure)
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

func (c *Client) failExactServerRequest(id any, request protocolv2.ServerRequest, code int, failure error) {
	// Finish correlated runs before the JSON-RPC error is visible to the peer
	// so a later successful terminal cannot race the first cause.
	for _, stream := range c.exactRunsForServerRequest(request) {
		stream.finish(failure)
	}
	if err := c.writeServerRequestError(id, code, failure); err != nil {
		c.failClient(err)
		return
	}
	if c.testAfterServerRequestFailureResponse != nil {
		c.testAfterServerRequestFailureResponse()
	}
}

type serverRequestIdentity struct {
	threadID string
	turnID   string
}

func extractServerRequestIdentity(request protocolv2.ServerRequest) serverRequestIdentity {
	raw, err := json.Marshal(request)
	if err != nil {
		return serverRequestIdentity{}
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return serverRequestIdentity{}
	}
	params, _ := envelope["params"].(map[string]any)
	if params == nil {
		return serverRequestIdentity{}
	}
	identity := serverRequestIdentity{
		threadID: jsonString(params["threadId"]),
		turnID:   jsonString(params["turnId"]),
	}
	if identity.threadID == "" {
		// applyPatchApproval / execCommandApproval store ThreadId as conversationId.
		identity.threadID = jsonString(params["conversationId"])
	}
	return identity
}

func (c *Client) exactRunsForServerRequest(request protocolv2.ServerRequest) []*exactRunState {
	identity := extractServerRequestIdentity(request)
	if identity.threadID == "" && identity.turnID == "" {
		return nil
	}
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	var targets []*exactRunState
	if identity.turnID != "" {
		for stream := range c.exactStreams[identity.turnID] {
			if identity.threadID == "" || stream.threadID == identity.threadID {
				targets = append(targets, stream)
			}
		}
		if identity.threadID != "" {
			liveForTurn := len(c.exactStreams[identity.turnID]) > 0
			for stream := range c.exactAttaching[identity.threadID] {
				snapshot := stream.turnIDSnapshot()
				if snapshot == identity.turnID {
					targets = append(targets, stream)
					continue
				}
				// A request can arrive after thread identity is known and before
				// turn/start publishes the run's turn ID. Attribute it only when
				// this turn has no live owner yet.
				if snapshot == "" && !liveForTurn {
					targets = append(targets, stream)
				}
			}
		}
		return targets
	}
	for _, streams := range c.exactStreams {
		for stream := range streams {
			if stream.threadID == identity.threadID {
				targets = append(targets, stream)
			}
		}
	}
	for stream := range c.exactAttaching[identity.threadID] {
		targets = append(targets, stream)
	}
	return targets
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
		c.failExactServerRequest(id, request, -32602, fmt.Errorf("codexsdk: encode %s response: %w", request.Kind(), err))
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		c.failExactServerRequest(id, request, -32602, fmt.Errorf("codexsdk: decode %s response object: %w", request.Kind(), err))
		return nil
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
