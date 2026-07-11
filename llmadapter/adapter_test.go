package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ronhuafeng/llmkit-go/settle"
)

type fakeCaller struct {
	responses []Response
	requests  []Request
	err       error
}

type details struct{ name string }

func (d details) ProviderName() string { return d.name }

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

func TestValueDetailedPreservesCallAndDecodeEvidence(t *testing.T) {
	providerErr := errors.New("provider failed")
	partial := Response{
		FinalResponse: `{"status":"partial"}`,
		Execution: ExecutionEvidence{
			ProviderName: "test",
			Usage:        &TokenUsage{InputTokens: 4},
		},
		ProviderDetails: details{name: "test"},
	}
	result, err := ValueDetailed[map[string]string](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return partial, providerErr
	}), "prompt")
	var valueErr *ValueError
	if !errors.As(err, &valueErr) || valueErr.Stage != ValueStageCall || !errors.Is(err, providerErr) {
		t.Fatalf("call error = %v, want ValueStageCall retaining provider error", err)
	}
	if result.Response.FinalResponse != partial.FinalResponse || result.Response.Execution.Usage.InputTokens != 4 {
		t.Fatalf("partial response = %#v, want %#v", result.Response, partial)
	}

	decodeResponse := partial
	decodeResponse.FinalResponse = `{"status":7}`
	result, err = ValueDetailed[map[string]string](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return decodeResponse, nil
	}), "prompt")
	if !errors.As(err, &valueErr) || valueErr.Stage != ValueStageDecode {
		t.Fatalf("decode error = %v, want ValueStageDecode", err)
	}
	if result.Response.FinalResponse != decodeResponse.FinalResponse {
		t.Fatalf("decode response was not preserved: %#v", result.Response)
	}
}

func TestValueDetailedChecksProviderIdentityWithoutReplacingCallError(t *testing.T) {
	providerErr := errors.New("provider failed")
	response := Response{
		Execution:       ExecutionEvidence{ProviderName: "one"},
		ProviderDetails: details{name: "two"},
	}
	_, err := ValueDetailed[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return response, providerErr
	}), "prompt")
	if !errors.Is(err, providerErr) || !errors.Is(err, ErrProviderIdentityMismatch) {
		t.Fatalf("error = %v, want provider and identity causes", err)
	}
}

func TestValueDetailedRejectsTypedNilProviderDetails(t *testing.T) {
	var typedNil *testPointerDetails
	_, err := ValueDetailed[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return Response{FinalResponse: `true`, Execution: ExecutionEvidence{ProviderName: "test"}, ProviderDetails: typedNil}, nil
	}), "prompt")
	if !errors.Is(err, ErrProviderIdentityMismatch) {
		t.Fatalf("error = %v, want typed nil identity failure", err)
	}
}

type testPointerDetails struct{}

func (*testPointerDetails) ProviderName() string { return "test" }

func TestValueDetailedReturnsRequestStageForSchemaProjectionFailure(t *testing.T) {
	called := false
	_, err := ValueDetailed[chan int](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
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

func TestValueDetailedPublishesUsageSnapshot(t *testing.T) {
	usage := &TokenUsage{InputTokens: 3}
	result, err := ValueDetailed[bool](context.Background(), callerFunc(func(context.Context, Request) (Response, error) {
		return Response{FinalResponse: `true`, Execution: ExecutionEvidence{Usage: usage}}, nil
	}), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	usage.InputTokens = 99
	if result.Response.Execution.Usage.InputTokens != 3 {
		t.Fatalf("published usage changed to %d", result.Response.Execution.Usage.InputTokens)
	}
}

func TestValueProjectsSchemaCallsBackendAndDecodes(t *testing.T) {
	caller := &fakeCaller{responses: []Response{{FinalResponse: `true`}}}

	got, err := Value[bool](context.Background(), caller, "Is Paris the capital of France?")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
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

func TestRequestForProjectsTypedOutputSchema(t *testing.T) {
	type verdict struct {
		Status string `json:"status,omitempty"`
	}

	request, err := RequestFor[verdict]("review")
	if err != nil {
		t.Fatal(err)
	}
	if request.Prompt != "review" {
		t.Fatalf("prompt = %q", request.Prompt)
	}
	if !strings.Contains(string(request.OutputSchema), `"status"`) {
		t.Fatalf("schema should include struct field: %s", request.OutputSchema)
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
	if got.Status != "passed" || got.Passed == nil || !*got.Passed {
		t.Fatalf("decoded verdict = %#v", got)
	}
	if !strings.Contains(string(caller.requests[0].OutputSchema), `"status"`) ||
		!strings.Contains(string(caller.requests[0].OutputSchema), `"passed"`) {
		t.Fatalf("schema should include struct fields: %s", caller.requests[0].OutputSchema)
	}
}

func TestOpCanRunInsideSettle(t *testing.T) {
	type input struct {
		Question string
	}
	type answer struct {
		Passed bool `json:"passed"`
	}
	caller := &fakeCaller{responses: []Response{
		{FinalResponse: `{"passed":false}`},
		{FinalResponse: `{"passed":true}`},
	}}
	op := NewOp[input, answer](Options[input, answer]{
		Caller: caller,
		Render: func(_ context.Context, in input) (string, error) {
			return in.Question, nil
		},
		Validate: func(_ context.Context, _ input, out answer) (bool, error) {
			return out.Passed, nil
		},
	})

	got, err := settle.Run(context.Background(), op, input{Question: "done?"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed {
		t.Fatalf("settled output = %#v", got)
	}
	if len(caller.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(caller.requests))
	}
}

func TestValueFailsClosed(t *testing.T) {
	if _, err := Value[bool](context.Background(), nil, "prompt"); !errors.Is(err, ErrNilCaller) {
		t.Fatalf("nil caller error = %v, want ErrNilCaller", err)
	}
	if _, err := Value[bool](context.Background(), &fakeCaller{}, "prompt"); err == nil ||
		!strings.Contains(err.Error(), "final response is empty") {
		t.Fatalf("empty final response error = %v", err)
	}
	if _, err := Value[bool](context.Background(), &fakeCaller{responses: []Response{{FinalResponse: `not-json`}}}, "prompt"); err == nil {
		t.Fatal("Value accepted invalid JSON")
	}
}
