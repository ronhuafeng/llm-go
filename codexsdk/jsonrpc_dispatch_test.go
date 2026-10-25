package codexsdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestJSONRPCResponseSelectionIgnoresRawError(t *testing.T) {
	c := newTransportHarness()
	defer c.Close()
	responses := make(chan rpcResponse, 1)
	if err := c.admitPending("go-sdk-1", pendingCall{method: "fixture/read", response: responses}); err != nil {
		t.Fatal(err)
	}
	if err := c.ingestJSONRPCLine([]byte(`{"id":"go-sdk-1","method":false,"result":{"n":9007199254740993},"error":{"code":-1,"message":"ignored"}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-responses:
		if got.err != nil || got.result["n"] != json.Number(largeJSONInteger) {
			t.Fatalf("response = %+v", got)
		}
	default:
		t.Fatal("chosen response was not correlated")
	}
}

func TestJSONRPCPendingIdentityDoesNotCoerceNumericID(t *testing.T) {
	c := newTransportHarness()
	defer c.Close()
	responses := make(chan rpcResponse, 1)
	if err := c.admitPending(largeJSONInteger, pendingCall{method: "fixture/read", response: responses}); err != nil {
		t.Fatal(err)
	}
	if err := c.ingestJSONRPCLine([]byte(`{"id":9007199254740993,"result":{}}`)); err != nil {
		t.Fatal(err)
	}
	if pendingCount(c) != 1 {
		t.Fatal("numeric response acquired a string request identity")
	}
	if err := c.ingestJSONRPCLine([]byte(`{"id":"9007199254740993","result":{"ok":true}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-responses:
		if got.err != nil || got.result["ok"] != true {
			t.Fatalf("response = %+v", got)
		}
	default:
		t.Fatal("exact string identity was not correlated")
	}
}

func TestJSONRPCFallbackNotificationRetainsOriginalObservation(t *testing.T) {
	for _, ignored := range []string{`"id":null`, `"id":{}`, `"id":"go-sdk-1","trace":{"traceparent":2}`} {
		t.Run(ignored, func(t *testing.T) {
			seen := make(chan RawNotification, 1)
			var calls atomic.Int32
			c := retentionClient(t, 100, 1<<20, ClientOptions{
				RawNotifications: &RawNotificationSubscription{Methods: []string{"thread/name/updated"}, Handler: func(_ context.Context, n RawNotification) error { seen <- n; return nil }},
				ServerRequestHandler: func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
					calls.Add(1)
					return ServerRequestResponse{}, errors.New("must not execute")
				},
			})
			defer c.Close()
			responses := make(chan rpcResponse, 1)
			if err := c.admitPending("go-sdk-1", pendingCall{method: "fixture/read", response: responses}); err != nil {
				t.Fatal(err)
			}
			line := `{` + ignored + `,"method":"thread/name/updated","params":{"threadId":"thread-1","threadName":"new"},"result":{"ignored":true},"error":null,"future":{"n":9007199254740993,"exponent":1e400,"present":null}}`
			if err := c.ingestJSONRPCLine([]byte(line)); err != nil {
				t.Fatal(err)
			}
			select {
			case n := <-seen:
				want, err := protocolv2.ParseJSONValue([]byte(line))
				if err != nil {
					t.Fatal(err)
				}
				gotJSON, _ := n.Envelope.MarshalJSON()
				wantJSON, _ := want.MarshalJSON()
				if string(gotJSON) != string(wantJSON) {
					t.Fatalf("original envelope lost: %s", gotJSON)
				}
				if calls.Load() != 0 || pendingCount(c) != 1 || writtenJSONRPC(c) != "" {
					t.Fatal("ignored fields acquired request/correlation authority")
				}
			case <-time.After(time.Second):
				t.Fatal("fallback notification was not observed")
			}
		})
	}
}

func TestJSONRPCRequestProjectionKeepsTypedAdmission(t *testing.T) {
	c := newTransportHarness()
	defer c.Close()
	seen := make(chan protocolv2.ServerRequest, 1)
	c.options.ServerRequestHandler = func(_ context.Context, request protocolv2.ServerRequest) (ServerRequestResponse, error) {
		seen <- request
		return ServerRequestResponse{}, errors.New("application answer")
	}
	// Result/error/trace metadata must not make an otherwise valid typed request
	// fail closed-root validation. Unknown fields inside params remain strict.
	line := `{"id":9007199254740993,"method":"account/chatgptAuthTokens/refresh","params":{"reason":"unauthorized"},"trace":{"traceparent":null,"future":true},"result":{},"error":false,"future":true}`
	if err := c.ingestJSONRPCLine([]byte(line)); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-seen:
		payload, ok := request.AsAccountChatGPTAuthTokensRefresh()
		id, integer := payload.ID.AsInt64()
		if !ok || !integer || id != 9007199254740993 {
			t.Fatal("projected request lost typed identity")
		}
	case <-time.After(time.Second):
		t.Fatal("legitimate envelope evolution did not reach application")
	}
	waitForWrittenToken(t, c, `"id":9007199254740993`)
	before := writtenJSONRPC(c)
	if err := c.ingestJSONRPCLine([]byte(strings.Replace(line, `"reason":"unauthorized"`, `"reason":"unauthorized","future":true`, 1))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.TrimPrefix(writtenJSONRPC(c), before), `"code":-32602`) {
		t.Fatal("nested action payload no longer rejected")
	}
	select {
	case <-seen:
		t.Fatal("malformed payload reached application")
	default:
	}
}

func TestJSONRPCUnsupportedArrayIDDoesNotAuthorizeRequest(t *testing.T) {
	c := newTransportHarness()
	defer c.Close()
	var calls atomic.Int32
	c.options.ServerRequestHandler = func(context.Context, protocolv2.ServerRequest) (ServerRequestResponse, error) {
		calls.Add(1)
		return ServerRequestResponse{}, nil
	}
	if err := c.ingestJSONRPCLine([]byte(`{"id":["x"],"method":"account/chatgptAuthTokens/refresh","params":{"reason":"unauthorized"}}`)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || writtenJSONRPC(c) != "" {
		t.Fatal("hypothetical array ID acquired request authority")
	}
	if err := c.ingestJSONRPCLine([]byte(`{"id":["x"],"result":{}}`)); err == nil {
		t.Fatal("unsupported response identity accepted")
	}
}
