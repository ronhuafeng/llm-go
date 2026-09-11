package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
)

type fakeCaller struct {
	responses []Response
	requests  []Request
	err       error
}

type details struct{ name string }

func (d details) BackendName() string { return d.name }

type callerFunc func(context.Context, Request) (Response, error)

func (f callerFunc) Call(ctx context.Context, request Request) (Response, error) {
	return f(ctx, request)
}

func (caller *fakeCaller) Call(ctx context.Context, request Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	caller.requests = append(caller.requests, Request{
		Prompt:       request.Prompt,
		OutputSchema: append(json.RawMessage(nil), request.OutputSchema...),
	})
	if caller.err != nil {
		return Response{}, caller.err
	}
	if len(caller.responses) == 0 {
		return Response{}, nil
	}
	response := caller.responses[0]
	caller.responses = caller.responses[1:]
	return response, nil
}

func TestValuePreservesCallAndDecodeEvidence(t *testing.T) {
	providerErr := errors.New("provider failed")
	partial := Response{
		FinalResponse: `{"status":"partial"}`,
		Execution: ExecutionEvidence{
			BackendName: "test-backend",
			Usage:       &TokenUsage{Input: Observed[int64](4)},
		},
		BackendDetails: details{name: "test-backend"},
	}
	result, err := Value[map[string]string](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return partial, providerErr
	}), "prompt")
	var valueErr *ValueError
	if !errors.As(err, &valueErr) || valueErr.Stage != ValueStageCall || !errors.Is(err, providerErr) {
		t.Fatalf("call error = %v, want ValueStageCall retaining provider error", err)
	}
	if result.Response.FinalResponse != partial.FinalResponse || result.Response.Execution.Usage.Input != Observed[int64](4) {
		t.Fatalf("partial response = %#v, want %#v", result.Response, partial)
	}

	decodeResponse := partial
	decodeResponse.FinalResponse = `{"status":7}`
	result, err = Value[map[string]string](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return decodeResponse, nil
	}), "prompt")
	if !errors.As(err, &valueErr) || valueErr.Stage != ValueStageDecode {
		t.Fatalf("decode error = %v, want ValueStageDecode", err)
	}
	if result.Response.FinalResponse != decodeResponse.FinalResponse {
		t.Fatalf("decode response was not preserved: %#v", result.Response)
	}
}

func TestValueChecksBackendIdentityWithoutReplacingCallError(t *testing.T) {
	providerErr := errors.New("provider failed")
	response := Response{
		Execution:      ExecutionEvidence{BackendName: "one"},
		BackendDetails: details{name: "two"},
	}
	_, err := Value[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return response, providerErr
	}), "prompt")
	if !errors.Is(err, providerErr) || !errors.Is(err, ErrBackendIdentityMismatch) {
		t.Fatalf("error = %v, want provider and backend-identity causes", err)
	}
}

func TestValueRejectsTypedNilBackendDetails(t *testing.T) {
	var typedNil *testPointerDetails
	_, err := Value[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return Response{FinalResponse: `true`, Execution: ExecutionEvidence{BackendName: "test"}, BackendDetails: typedNil}, nil
	}), "prompt")
	if !errors.Is(err, ErrBackendIdentityMismatch) {
		t.Fatalf("error = %v, want typed nil backend identity failure", err)
	}
}

type testPointerDetails struct{}

func (*testPointerDetails) BackendName() string { return "test" }

func TestExecutionIdentityKeepsBackendAndProviderIndependent(t *testing.T) {
	evidence := ExecutionEvidence{BackendName: "codex"}
	if _, ok := evidence.ProviderName.Value(); ok {
		t.Fatal("backend identity manufactured provider identity")
	}
	evidence.ObserveProviderName("observed-provider")
	provider, ok := evidence.ProviderName.Value()
	if !ok || provider != "observed-provider" {
		t.Fatalf("provider = (%q, %t), want observed provider", provider, ok)
	}
	if evidence.BackendName != "codex" {
		t.Fatalf("backend = %q, want codex", evidence.BackendName)
	}
}

func TestValueReturnsRequestStageForSchemaProjectionFailure(t *testing.T) {
	called := false
	_, err := Value[chan int](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		called = true
		return Response{}, nil
	}), "prompt")
	var valueErr *ValueError
	if !errors.As(err, &valueErr) || valueErr.Stage != ValueStageRequest {
		t.Fatalf("error = %v, want ValueStageRequest", err)
	}
	if called {
		t.Fatal("caller invoked after request projection failure")
	}
}

func TestValuePublishesUsageSnapshot(t *testing.T) {
	usage := &TokenUsage{Input: Observed[int64](3)}
	result, err := Value[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return Response{FinalResponse: `true`, Execution: ExecutionEvidence{Usage: usage}}, nil
	}), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	usage.Input = Observed[int64](99)
	if result.Response.Execution.Usage.Input != Observed[int64](3) {
		t.Fatalf("published usage changed to %#v", result.Response.Execution.Usage)
	}
}

func TestValueProvidesIsolatedRequestSchemaPerCall(t *testing.T) {
	var firstSchema json.RawMessage
	calls := 0
	caller := callerFunc(func(_ context.Context, request Request) (Response, error) {
		calls++
		if calls == 1 {
			firstSchema = request.OutputSchema
			request.OutputSchema[0] = '!'
		} else if request.OutputSchema[0] == '!' {
			t.Fatal("second call reused caller-mutated schema bytes")
		}
		return Response{FinalResponse: `true`}, nil
	})

	if _, err := Value[bool](context.Background(), caller, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := Value[bool](context.Background(), caller, "second"); err != nil {
		t.Fatal(err)
	}
	if len(firstSchema) == 0 || firstSchema[0] != '!' {
		t.Fatal("caller did not receive mutable schema copy")
	}
}

func TestValueGenericValueUsesOrdinaryGoSemantics(t *testing.T) {
	type output struct {
		Labels map[string]string `json:"labels"`
	}
	result, err := Value[output](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return Response{FinalResponse: `{"labels":{"status":"draft"}}`}, nil
	}), "prompt")
	if err != nil {
		t.Fatal(err)
	}

	valueCopy := result.Value
	valueCopy.Labels["status"] = "final"
	if result.Value.Labels["status"] != "final" {
		t.Fatal("generic value unexpectedly behaved as a deep clone")
	}
}

func TestValueProjectsSchemaCallsBackendAndDecodes(t *testing.T) {
	caller := &fakeCaller{responses: []Response{{FinalResponse: `true`}}}

	got, err := Value[bool](context.Background(), caller, "Is Paris the capital of France?")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Value {
		t.Fatal("Value returned false, want true")
	}
	if len(caller.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(caller.requests))
	}
	if caller.requests[0].Prompt != "Is Paris the capital of France?" {
		t.Fatalf("prompt = %q", caller.requests[0].Prompt)
	}
	if !strings.Contains(string(caller.requests[0].OutputSchema), `"boolean"`) {
		t.Fatalf("schema should describe bool output: %s", caller.requests[0].OutputSchema)
	}
}

func TestValueWithContractUsesOwnedSchemaAndDecode(t *testing.T) {
	type verdict struct {
		Status string `json:"status"`
	}
	contract, err := llmschema.Compile[verdict]()
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{responses: []Response{{FinalResponse: `{"status":"ok"}`}}}
	got, err := ValueWithContract[verdict](context.Background(), caller, "review", contract)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value.Status != "ok" {
		t.Fatalf("decoded = %#v", got.Value)
	}
	if string(caller.requests[0].OutputSchema) != string(contract.SchemaJSON()) {
		t.Fatalf("request schema = %s, want contract %s", caller.requests[0].OutputSchema, contract.SchemaJSON())
	}
}

func TestValueWithContractRejectsZeroContractBeforeCall(t *testing.T) {
	caller := &fakeCaller{responses: []Response{{FinalResponse: `true`}}}
	var contract llmschema.Contract[bool]
	result, err := ValueWithContract[bool](context.Background(), caller, "prompt", contract)
	if !errors.Is(err, llmschema.ErrUncompiledContract) {
		t.Fatalf("error = %v, want ErrUncompiledContract", err)
	}
	if len(caller.requests) != 0 {
		t.Fatalf("zero contract invoked caller: %#v", caller.requests)
	}
	if result.Response.FinalResponse != "" {
		t.Fatalf("zero contract published evidence: %#v", result.Response)
	}
}

func TestValueSupportsStructOutput(t *testing.T) {
	type verdict struct {
		Status string `json:"status,omitempty"`
		Passed *bool  `json:"passed,omitempty"`
	}
	caller := &fakeCaller{responses: []Response{{FinalResponse: `{"status":"passed","passed":true}`}}}

	got, err := Value[verdict](context.Background(), caller, "review")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value.Status != "passed" || got.Value.Passed == nil || !*got.Value.Passed {
		t.Fatalf("decoded verdict = %#v", got.Value)
	}
	if !strings.Contains(string(caller.requests[0].OutputSchema), `"status"`) ||
		!strings.Contains(string(caller.requests[0].OutputSchema), `"passed"`) {
		t.Fatalf("schema should include struct fields: %s", caller.requests[0].OutputSchema)
	}
}

func TestValueFailsClosed(t *testing.T) {
	empty := Response{FinalResponse: `not-json`, Execution: ExecutionEvidence{BackendName: "test"}}
	if result, err := Value[bool](context.Background(), nil, "prompt"); !errors.Is(err, ErrNilCaller) {
		t.Fatalf("nil caller error = %v, want ErrNilCaller", err)
	} else if result.Response.FinalResponse != "" {
		t.Fatalf("nil caller response = %#v, want empty evidence", result.Response)
	}
	if result, err := Value[bool](context.Background(), &fakeCaller{}, "prompt"); err == nil ||
		!strings.Contains(err.Error(), "final response is empty") {
		t.Fatalf("empty final response error = %v", err)
	} else if result.Response.FinalResponse != "" {
		t.Fatalf("empty caller response = %#v", result.Response)
	}
	if result, err := Value[bool](context.Background(), &fakeCaller{responses: []Response{empty}}, "prompt"); err == nil {
		t.Fatal("Value accepted invalid JSON")
	} else if result.Response.FinalResponse != empty.FinalResponse || result.Response.Execution.BackendName != "test" {
		t.Fatalf("decode failure discarded call evidence: %#v", result.Response)
	}
}
