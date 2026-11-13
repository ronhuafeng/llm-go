package codexsdk

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestGatewayOAuthOptionalParamsWireContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		params  []*protocolv2.Nullable[protocolv2.GatewayOAuthLoginParams]
		present bool
		want    any
	}{
		{name: "omitted"},
		{name: "null", params: []*protocolv2.Nullable[protocolv2.GatewayOAuthLoginParams]{protocolv2.Null[protocolv2.GatewayOAuthLoginParams]()}, present: true},
		{name: "empty", params: []*protocolv2.Nullable[protocolv2.GatewayOAuthLoginParams]{protocolv2.Value(protocolv2.GatewayOAuthLoginParams{})}, present: true, want: map[string]any{}},
		{name: "provider and device auth", params: []*protocolv2.Nullable[protocolv2.GatewayOAuthLoginParams]{protocolv2.Value(protocolv2.GatewayOAuthLoginParams{ModelProvider: protocolv2.Value("gateway"), DeviceAuth: boolPointer(true)})}, present: true, want: map[string]any{"modelProvider": "gateway", "deviceAuth": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTransportHarness()
			defer c.cancel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := c.Accounts().GatewayOAuthLogin(ctx, tc.params...); done <- err }()
			writer := c.stdin.(*recordingWriteCloser)
			id := waitForWrittenRequest(t, writer, protocolv2.MethodAccountGatewayOAuthLogin, 1)
			var envelope map[string]any
			if err := json.Unmarshal(writer.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			params, present := envelope["params"]
			if present != tc.present || !reflect.DeepEqual(params, tc.want) {
				t.Fatalf("params present=%v value=%#v, want present=%v value=%#v", present, params, tc.present, tc.want)
			}
			c.readLoop(strings.NewReader(`{"id":"` + id + `","result":{}}` + "\n"))
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
	c := newTransportHarness()
	defer c.cancel()
	_, err := c.Accounts().GatewayOAuthLogin(context.Background(), nil, nil)
	if err == nil || len(c.stdin.(*recordingWriteCloser).Bytes()) != 0 {
		t.Fatal("multiple parameter values must fail before writing")
	}
	if _, err := encodeProtocolParams(protocolv2.MethodThreadStart, protocolv2.Null[protocolv2.ThreadStartParams]()); err == nil {
		t.Fatal("non-nullable method accepted null params")
	}
}

func boolPointer(value bool) *bool { return &value }
