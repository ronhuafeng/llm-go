package codexsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

const largeJSONInteger = "9007199254740993"

func TestDefaultMapUnmarshalRoundsIntegerRequestIDs(t *testing.T) {
	line := []byte(`{"id":` + largeJSONInteger + `,"result":{}}`)
	var lost map[string]any
	if err := json.Unmarshal(line, &lost); err != nil {
		t.Fatal(err)
	}
	lostRaw, err := json.Marshal(lost["id"])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(lostRaw, []byte(largeJSONInteger)) {
		t.Fatal("default map unmarshal unexpectedly preserved the integer token")
	}

	var preserved map[string]any
	if err := unmarshalJSONPreserveNumbers(line, &preserved); err != nil {
		t.Fatal(err)
	}
	preservedRaw, err := json.Marshal(preserved["id"])
	if err != nil {
		t.Fatal(err)
	}
	if string(preservedRaw) != largeJSONInteger {
		t.Fatalf("preserved id = %s, want %s", preservedRaw, largeJSONInteger)
	}
}

func TestInboundIntegerRequestIDIsEchoedExactly(t *testing.T) {
	client := newTransportHarness()
	seen := make(chan protocolv2.RequestId, 1)
	client.options.ServerRequestHandler = func(_ context.Context, request protocolv2.ServerRequest) (ServerRequestResponse, error) {
		payload, ok := request.AsAccountChatGPTAuthTokensRefresh()
		if !ok {
			return ServerRequestResponse{}, errors.New("want auth refresh")
		}
		seen <- payload.ID
		return ServerRequestResponse{}, errors.New("stop after observing id")
	}
	line := []byte(`{"id":` + largeJSONInteger + `,"method":"account/chatgptAuthTokens/refresh","params":{"reason":"unauthorized"}}`)
	if err := client.ingestJSONRPCLine(line); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-seen:
		got, ok := id.AsInt64()
		if !ok || got != 9007199254740993 {
			t.Fatalf("typed request id = %#v ok=%v, want int64 %s", id, ok, largeJSONInteger)
		}
	case <-time.After(time.Second):
		t.Fatal("typed server request did not preserve the integer request id")
	}
	waitForWrittenToken(t, client, `"id":`+largeJSONInteger)
	if strings.Contains(writtenJSONRPC(client), "9007199254740992") {
		t.Fatalf("request id was rounded: %s", writtenJSONRPC(client))
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInboundStringAndSmallIntegerRequestIDsStillWork(t *testing.T) {
	for _, line := range []string{
		`{"id":"req-1","method":"account/chatgptAuthTokens/refresh","params":{"reason":"unauthorized"}}`,
		`{"id":42,"method":"account/chatgptAuthTokens/refresh","params":{"reason":"unauthorized"}}`,
	} {
		client := newTransportHarness()
		if err := client.ingestJSONRPCLine([]byte(line)); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, `"req-1"`) {
			waitForWrittenToken(t, client, `"id":"req-1"`)
		} else {
			waitForWrittenToken(t, client, `"id":42`)
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInboundGeneratedIntegerFieldSurvivesServerRequestRouting(t *testing.T) {
	client := newTransportHarness()
	seen := make(chan int64, 1)
	client.options.ServerRequestHandler = func(_ context.Context, request protocolv2.ServerRequest) (ServerRequestResponse, error) {
		payload, ok := request.AsItemCommandExecutionRequestApproval()
		if !ok {
			return ServerRequestResponse{}, errors.New("want command approval")
		}
		seen <- payload.Params.StartedAtMS
		return ServerRequestResponse{}, errors.New("stop after observing startedAtMs")
	}
	line := []byte(`{"id":"approval-1","method":"item/commandExecution/requestApproval","params":{"command":"echo","cwd":"/tmp","itemId":"item-1","reason":"test","startedAtMs":` + largeJSONInteger + `,"threadId":"thread-1","turnId":"turn-1"}}`)
	if err := client.ingestJSONRPCLine(line); err != nil {
		t.Fatal(err)
	}
	select {
	case startedAt := <-seen:
		if startedAt != 9007199254740993 {
			t.Fatalf("startedAtMs = %d, want %s", startedAt, largeJSONInteger)
		}
	case <-time.After(time.Second):
		t.Fatal("typed generated integer field did not survive routing")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInboundJSONValueNumberSurvivesServerRequestRouting(t *testing.T) {
	client := newTransportHarness()
	seen := make(chan json.Number, 1)
	client.options.ServerRequestHandler = func(_ context.Context, request protocolv2.ServerRequest) (ServerRequestResponse, error) {
		payload, ok := request.AsItemToolCall()
		if !ok {
			return ServerRequestResponse{}, errors.New("want item/tool/call")
		}
		object, ok := payload.Params.Arguments.AsObject()
		if !ok {
			return ServerRequestResponse{}, errors.New("want object arguments")
		}
		number, ok := object["n"].AsNumber()
		if !ok {
			return ServerRequestResponse{}, errors.New("want number n")
		}
		seen <- number
		return ServerRequestResponse{}, errors.New("stop after observing number")
	}
	line := []byte(`{"id":"tool-1","method":"item/tool/call","params":{"arguments":{"n":` + largeJSONInteger + `},"callId":"call-tool","threadId":"thread-1","tool":"status","turnId":"turn-1"}}`)
	if err := client.ingestJSONRPCLine(line); err != nil {
		t.Fatal(err)
	}
	select {
	case number := <-seen:
		if number.String() != largeJSONInteger {
			t.Fatalf("JSONValue number = %q, want %s", number, largeJSONInteger)
		}
	case <-time.After(time.Second):
		t.Fatal("typed server request did not preserve the JSON number token")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolErrorDataPreservesNumberToken(t *testing.T) {
	client := newTransportHarness()
	errCh := make(chan error, 1)
	go func() {
		_, err := client.call(context.Background(), "failing/method", map[string]any{"value": "fail"})
		errCh <- err
	}()
	waitForPendingCount(t, client, 1)
	line := []byte(`{"id":"go-sdk-1","error":{"code":-32000,"message":"boom","data":{"n":` + largeJSONInteger + `}}}`)
	if err := client.ingestJSONRPCLine(line); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) || protocolErr.Data == nil {
			t.Fatalf("protocol error = %#v", err)
		}
		object, ok := protocolErr.Data.AsObject()
		if !ok {
			t.Fatalf("data kind = %s", protocolErr.Data.Kind())
		}
		number, ok := object["n"].AsNumber()
		if !ok || number.String() != largeJSONInteger {
			t.Fatalf("data number = %q ok=%v, want %s", number, ok, largeJSONInteger)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol error")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOutboundGeneratedIntegerIsNotRounded(t *testing.T) {
	client := newTransportHarness()
	response := CurrentTimeResponse(protocolv2.CurrentTimeReadResponse{CurrentTimeAt: 9007199254740993})
	if err := client.writeExactServerRequestResponse("time-1", protocolv2.ServerRequest{}, response); err != nil {
		t.Fatal(err)
	}
	written := writtenJSONRPC(client)
	if !strings.Contains(written, `"currentTimeAt":`+largeJSONInteger) {
		t.Fatalf("outbound generated integer was rounded: %s", written)
	}
	if strings.Contains(written, "9007199254740992") {
		t.Fatalf("outbound generated integer used float64 rounding: %s", written)
	}
}

func TestOutboundJSONValueNumberIsNotRounded(t *testing.T) {
	schema := mustOutputSchema(t, `{"type":"object","properties":{"n":{"maximum":`+largeJSONInteger+`,"type":"number"}}}`)
	params := protocolv2.TurnStartParams{
		Input:        []protocolv2.UserInput{},
		OutputSchema: &schema,
	}
	encoded, err := encodeProtocolParams(protocolv2.MethodTurnStart, params)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(largeJSONInteger)) {
		t.Fatalf("outbound JSONValue number was rounded: %s", raw)
	}
	client := newTransportHarness()
	if err := client.write(map[string]any{"id": "go-sdk-1", "method": protocolv2.MethodTurnStart, "params": encoded}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(writtenJSONRPC(client), largeJSONInteger) {
		t.Fatalf("outbound JSON-RPC line rounded JSONValue number: %s", writtenJSONRPC(client))
	}
}

func waitForWrittenToken(t *testing.T, client *Client, token string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(writtenJSONRPC(client), token) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("written JSON-RPC %q does not contain %s", writtenJSONRPC(client), token)
}

func writtenJSONRPC(client *Client) string {
	return strings.TrimSpace(client.stdin.(*recordingWriteCloser).String())
}

func mustOutputSchema(t *testing.T, raw string) protocolv2.OutputSchema {
	t.Helper()
	schema, err := protocolv2.OutputSchemaFromJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}
