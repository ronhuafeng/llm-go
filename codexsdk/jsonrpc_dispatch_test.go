package codexsdk

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestJSONRPCUnsupportedArrayIDDoesNotAuthorizeRequest(t *testing.T) {
	c := newTransportHarness()
	defer c.Close()
	var calls atomic.Int32
	c.options.ServerRequestHandler = func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
		calls.Add(1)
		return ServerRequestResponse{}, nil
	}
	if err := c.ingestJSONRPCLine([]byte(`{"id":["x"],"method":"account/chatgptAuthTokens/refresh","params":{"reason":"unauthorized"}}`)); err == nil {
		t.Fatal("unsupported request identity accepted")
	}
	if calls.Load() != 0 || writtenJSONRPC(c) != "" {
		t.Fatal("hypothetical array ID acquired request authority")
	}
	if err := c.ingestJSONRPCLine([]byte(`{"id":["x"],"result":{}}`)); err == nil {
		t.Fatal("unsupported response identity accepted")
	}
}
