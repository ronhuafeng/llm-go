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
