package codexsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
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
		response, ok := exactFailClosedServerRequestResponse(request)
		if !ok {
			failure := &ExactServerRequestError{Kind: request.Kind()}
			c.writeServerRequestError(id, -32000, failure)
			c.failClient(failure)
			return
		}
		if err := c.writeExactServerRequestResponse(id, request, response); err != nil {
			c.failClient(err)
		}
		return
	}
	response, err := invokeExactServerRequestHandler(ctx, c.options.ServerRequestHandler, request)
	if err != nil {
		if c.closingNormally() {
			return
		}
		c.writeServerRequestError(id, -32000, err)
		c.failClient(err)
		return
	}
	if response.kind != request.Kind() || response.value == nil {
		failure := &ExactServerRequestError{Kind: request.Kind(), Reason: fmt.Sprintf("received mismatched or empty response %s", response.kind)}
		c.writeServerRequestError(id, -32602, failure)
		c.failClient(failure)
		return
	}
	if err := c.writeExactServerRequestResponse(id, request, response); err != nil {
		c.failClient(err)
	}
}

func (c *Client) rejectExactServerRequestAfterAdmissionClosed(id any, request protocolv2.ServerRequest) {
	if response, ok := exactFailClosedServerRequestResponse(request); ok {
		_ = c.writeExactServerRequestResponse(id, request, response)
		return
	}
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

func exactFailClosedServerRequestResponse(request protocolv2.ServerRequest) (ServerRequestResponse, bool) {
	switch request.Kind() {
	case protocolv2.ServerRequestKindItemCommandExecutionRequestApproval:
		return CommandExecutionApprovalResponse(protocolv2.CommandExecutionRequestApprovalResponse{Decision: protocolv2.NewCommandExecutionApprovalDecisionDecline()}), true
	case protocolv2.ServerRequestKindItemFileChangeRequestApproval:
		return FileChangeApprovalResponse(protocolv2.FileChangeRequestApprovalResponse{Decision: protocolv2.FileChangeApprovalDecisionDecline}), true
	case protocolv2.ServerRequestKindItemToolRequestUserInput:
		return ToolUserInputResponse(protocolv2.ToolRequestUserInputResponse{Answers: map[string]protocolv2.ToolRequestUserInputAnswer{}}), true
	case protocolv2.ServerRequestKindMCPServerElicitationRequest:
		return MCPElicitationResponse(protocolv2.McpServerElicitationRequestResponse{Action: protocolv2.McpServerElicitationActionDecline}), true
	case protocolv2.ServerRequestKindItemPermissionsRequestApproval:
		return PermissionsApprovalResponse(protocolv2.PermissionsRequestApprovalResponse{Permissions: protocolv2.GrantedPermissionProfile{}}), true
	case protocolv2.ServerRequestKindCurrentTimeRead:
		return CurrentTimeResponse(protocolv2.CurrentTimeReadResponse{CurrentTimeAt: time.Now().UnixMilli()}), true
	case protocolv2.ServerRequestKindApplyPatchApproval:
		return ApplyPatchApprovalResponse(protocolv2.ApplyPatchApprovalResponse{Decision: protocolv2.NewReviewDecisionDenied()}), true
	case protocolv2.ServerRequestKindExecCommandApproval:
		return ExecCommandApprovalResponse(protocolv2.ExecCommandApprovalResponse{Decision: protocolv2.NewReviewDecisionDenied()}), true
	case protocolv2.ServerRequestKindItemToolCall,
		protocolv2.ServerRequestKindAccountChatGPTAuthTokensRefresh,
		protocolv2.ServerRequestKindAttestationGenerate:
		return ServerRequestResponse{}, false
	default:
		return ServerRequestResponse{}, false
	}
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
