package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/wirejson"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
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
	if c.experimentalAPIEnabled() || params == nil {
		return nil
	}
	info, ok := protocolv2.LookupMethod(method)
	if !ok || info.ParamsOrPayloadSchema == "" {
		return nil
	}
	encoded, err := encodeProtocolParams(method, params)
	if err != nil {
		return err
	}
	return rejectExperimentalJSON(method, info.ParamsOrPayloadSchema, encoded)
}

func rejectExperimentalJSON(method, typeName string, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		if disc := experimentalDiscriminatorValue(typed); disc != "" {
			if _, experimental := protocolv2.ExperimentalUnionValues[typeName][disc]; experimental {
				return fmt.Errorf("codexsdk: experimental variant %s.%s requires ClientCapabilities.ExperimentalAPI", method, disc)
			}
		}
		fields := protocolv2.ExperimentalJSONFields[typeName]
		children := protocolv2.ExperimentalChildTypes[typeName]
		for key, child := range typed {
			if _, experimental := fields[key]; experimental {
				return experimentalFieldError(method, key)
			}
			if childType := children[key]; childType != "" {
				if err := rejectExperimentalJSON(method, childType, child); err != nil {
					return err
				}
			}
		}
	case []any:
		for _, child := range typed {
			if err := rejectExperimentalJSON(method, typeName, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func experimentalDiscriminatorValue(fields map[string]any) string {
	for _, key := range []string{"type", "method", "kind", "mode", "handlerType"} {
		if raw, ok := fields[key].(string); ok && raw != "" {
			return raw
		}
	}
	return ""
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
	if err := wirejson.Unmarshal(raw, response, wirejson.ServerObservation); err != nil {
		return fmt.Errorf("codexsdk: decode %s response: %w", method, err)
	}
	return nil
}
