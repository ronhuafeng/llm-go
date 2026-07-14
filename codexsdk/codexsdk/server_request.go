package codexsdk

import (
	"encoding/json"
	"fmt"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

func (c *Client) handleServerRequest(message map[string]any) {
	id := message["id"]
	typed, err := decodeProtocolServerRequest(message)
	if err != nil {
		failure := fmt.Errorf("codexsdk: decode ServerRequest request_id=%s: %w", requestIDString(id), err)
		c.writeServerRequestError(id, -32602, failure)
		c.failClient(failure)
		return
	}
	if c.isClosed() {
		c.rejectExactServerRequestAfterAdmissionClosed(id, typed)
		return
	}
	c.handleExactServerRequest(id, typed)
}

func decodeProtocolServerRequest(message map[string]any) (protocolv2.ServerRequest, error) {
	raw, err := json.Marshal(message)
	if err != nil {
		return protocolv2.ServerRequest{}, fmt.Errorf("codexsdk: encode server request for validation: %w", err)
	}
	var typed protocolv2.ServerRequest
	if err := json.Unmarshal(raw, &typed); err != nil {
		return protocolv2.ServerRequest{}, err
	}
	return typed, nil
}

func validateProtocolServerRequest(message map[string]any) error {
	_, err := decodeProtocolServerRequest(message)
	return err
}

func requestIDString(id any) string {
	if id == nil {
		return ""
	}
	return fmt.Sprint(id)
}

func (c *Client) writeServerRequestError(id any, code int, err error) {
	_ = c.write(map[string]any{"id": id, "error": map[string]any{"code": code, "message": err.Error()}})
}
