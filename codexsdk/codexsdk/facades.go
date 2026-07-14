package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func (c *Client) callProtocol(ctx context.Context, method string, params any, response any) error {
	if c == nil {
		return ErrClientClosed
	}
	if err := c.checkOpen(); err != nil {
		return err
	}
	if err := c.checkProtocolMethodAllowed(method); err != nil {
		return err
	}
	if err := c.checkProtocolParamsAllowed(method, params); err != nil {
		return err
	}
	paramsMap, err := encodeProtocolParams(method, params)
	if err != nil {
		return err
	}
	if _, err := c.callValidated(ctx, method, paramsMap, func(result map[string]any) error {
		return decodeProtocolResponse(method, result, response)
	}); err != nil {
		return err
	}
	return nil
}

func (c *Client) callProtocolNoParams(ctx context.Context, method string, response any) error {
	if c == nil {
		return ErrClientClosed
	}
	if err := c.checkOpen(); err != nil {
		return err
	}
	if err := c.checkProtocolMethodAllowed(method); err != nil {
		return err
	}
	if _, err := c.callValidated(ctx, method, nil, func(result map[string]any) error {
		return decodeProtocolResponse(method, result, response)
	}); err != nil {
		return err
	}
	return nil
}

func (c *Client) checkProtocolMethodAllowed(method string) error {
	info, ok := protocolv2.LookupMethod(method)
	if !ok {
		return fmt.Errorf("codexsdk: unknown app-server method %q", method)
	}
	if info.Stability == protocolv2.MethodStabilityExperimental && !c.experimentalAPIEnabled() {
		return fmt.Errorf("codexsdk: experimental app-server method %q requires ClientCapabilities.ExperimentalAPI", method)
	}
	return nil
}

func (c *Client) checkProtocolParamsAllowed(method string, params any) error {
	if c.experimentalAPIEnabled() {
		return nil
	}
	switch typed := params.(type) {
	case protocolv2.CommandExecParams:
		return rejectCommandExecExperimentalFields(method, typed)
	case *protocolv2.CommandExecParams:
		if typed == nil {
			return nil
		}
		return rejectCommandExecExperimentalFields(method, *typed)
	case protocolv2.ThreadForkParams:
		return rejectThreadForkExperimentalFields(method, typed)
	case *protocolv2.ThreadForkParams:
		if typed == nil {
			return nil
		}
		return rejectThreadForkExperimentalFields(method, *typed)
	case protocolv2.ThreadResumeParams:
		return rejectThreadResumeExperimentalFields(method, typed)
	case *protocolv2.ThreadResumeParams:
		if typed == nil {
			return nil
		}
		return rejectThreadResumeExperimentalFields(method, *typed)
	case protocolv2.ThreadStartParams:
		return rejectThreadStartExperimentalFields(method, typed)
	case *protocolv2.ThreadStartParams:
		if typed == nil {
			return nil
		}
		return rejectThreadStartExperimentalFields(method, *typed)
	case protocolv2.TurnStartParams:
		return rejectTurnStartExperimentalFields(method, typed)
	case *protocolv2.TurnStartParams:
		if typed == nil {
			return nil
		}
		return rejectTurnStartExperimentalFields(method, *typed)
	case protocolv2.TurnSteerParams:
		return rejectTurnSteerExperimentalFields(method, typed)
	case *protocolv2.TurnSteerParams:
		if typed == nil {
			return nil
		}
		return rejectTurnSteerExperimentalFields(method, *typed)
	default:
		return nil
	}
}

func (c *Client) experimentalAPIEnabled() bool {
	if c == nil {
		return false
	}
	capabilities := c.options.Initialize.Capabilities
	return capabilities != nil && capabilities.Value != nil && capabilities.Value.ExperimentalAPI != nil && *capabilities.Value.ExperimentalAPI
}

func experimentalFieldError(method, field string) error {
	return fmt.Errorf("codexsdk: experimental field %s.%s requires ClientCapabilities.ExperimentalAPI", method, field)
}

func rejectCommandExecExperimentalFields(method string, params protocolv2.CommandExecParams) error {
	if params.PermissionProfile != nil {
		return experimentalFieldError(method, "permissionProfile")
	}
	return nil
}

func rejectThreadForkExperimentalFields(method string, params protocolv2.ThreadForkParams) error {
	if params.ExcludeTurns != nil {
		return experimentalFieldError(method, "excludeTurns")
	}
	if params.Path != nil {
		return experimentalFieldError(method, "path")
	}
	if params.Permissions != nil {
		return experimentalFieldError(method, "permissions")
	}
	return nil
}

func rejectThreadResumeExperimentalFields(method string, params protocolv2.ThreadResumeParams) error {
	if params.ExcludeTurns != nil {
		return experimentalFieldError(method, "excludeTurns")
	}
	if params.History != nil {
		return experimentalFieldError(method, "history")
	}
	if params.Path != nil {
		return experimentalFieldError(method, "path")
	}
	if params.Permissions != nil {
		return experimentalFieldError(method, "permissions")
	}
	return nil
}

func rejectThreadStartExperimentalFields(method string, params protocolv2.ThreadStartParams) error {
	if params.DynamicTools != nil {
		return experimentalFieldError(method, "dynamicTools")
	}
	if params.Environments != nil {
		return experimentalFieldError(method, "environments")
	}
	if params.ExperimentalRawEvents != nil {
		return experimentalFieldError(method, "experimentalRawEvents")
	}
	if params.MockExperimentalField != nil {
		return experimentalFieldError(method, "mockExperimentalField")
	}
	if params.Permissions != nil {
		return experimentalFieldError(method, "permissions")
	}
	return nil
}

func rejectTurnStartExperimentalFields(method string, params protocolv2.TurnStartParams) error {
	if params.CollaborationMode != nil {
		return experimentalFieldError(method, "collaborationMode")
	}
	if params.Environments != nil {
		return experimentalFieldError(method, "environments")
	}
	if params.Permissions != nil {
		return experimentalFieldError(method, "permissions")
	}
	if params.ResponsesapiClientMetadata != nil {
		return experimentalFieldError(method, "responsesapiClientMetadata")
	}
	return nil
}

func rejectTurnSteerExperimentalFields(method string, params protocolv2.TurnSteerParams) error {
	if params.ResponsesapiClientMetadata != nil {
		return experimentalFieldError(method, "responsesapiClientMetadata")
	}
	return nil
}

func encodeProtocolParams(method string, params any) (map[string]any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("codexsdk: encode %s params: %w", method, err)
	}
	var paramsMap map[string]any
	if err := json.Unmarshal(raw, &paramsMap); err != nil {
		return nil, fmt.Errorf("codexsdk: encode %s params object: %w", method, err)
	}
	if paramsMap == nil {
		return nil, fmt.Errorf("codexsdk: encode %s params: protocol params must encode to object", method)
	}
	return paramsMap, nil
}

func decodeProtocolResponse(method string, result map[string]any, response any) error {
	if response == nil {
		return errors.New("codexsdk: protocol response target is nil")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("codexsdk: decode %s response: %w", method, err)
	}
	if err := json.Unmarshal(raw, response); err != nil {
		return fmt.Errorf("codexsdk: decode %s response: %w", method, err)
	}
	return nil
}
