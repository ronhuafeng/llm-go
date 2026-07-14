package codexsdk

import (
	"fmt"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

type ProtocolError struct {
	RequestID protocolv2.RequestId
	Method    string
	Code      int64
	Message   string
	Data      *protocolv2.JSONValue
	Err       error
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("codexsdk: app-server error from %s code=%d message=%q: %v", e.Method, e.Code, e.Message, e.Err)
}

func (e *ProtocolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
