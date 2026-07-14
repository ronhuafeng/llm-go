package codexsdk

import "github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"

func CommandExecutionApprovalResponse(value protocolv2.CommandExecutionRequestApprovalResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindItemCommandExecutionRequestApproval, value)
}

func FileChangeApprovalResponse(value protocolv2.FileChangeRequestApprovalResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindItemFileChangeRequestApproval, value)
}

func ToolUserInputResponse(value protocolv2.ToolRequestUserInputResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindItemToolRequestUserInput, value)
}

func MCPElicitationResponse(value protocolv2.McpServerElicitationRequestResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindMCPServerElicitationRequest, value)
}

func PermissionsApprovalResponse(value protocolv2.PermissionsRequestApprovalResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindItemPermissionsRequestApproval, value)
}

func DynamicToolResponse(value protocolv2.DynamicToolCallResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindItemToolCall, value)
}

func ChatGPTAuthRefreshResponse(value protocolv2.ChatgptAuthTokensRefreshResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindAccountChatGPTAuthTokensRefresh, value)
}

func AttestationResponse(value protocolv2.AttestationGenerateResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindAttestationGenerate, value)
}

func CurrentTimeResponse(value protocolv2.CurrentTimeReadResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindCurrentTimeRead, value)
}

func ApplyPatchApprovalResponse(value protocolv2.ApplyPatchApprovalResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindApplyPatchApproval, value)
}

func ExecCommandApprovalResponse(value protocolv2.ExecCommandApprovalResponse) ServerRequestResponse {
	return serverResponse(protocolv2.ServerRequestKindExecCommandApproval, value)
}

func serverResponse(kind protocolv2.ServerRequestKind, value any) ServerRequestResponse {
	return ServerRequestResponse{kind: kind, value: value}
}
