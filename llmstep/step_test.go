package llmstep

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ronhuafeng/llmkit-go/llmadapter"
	"github.com/ronhuafeng/llmkit-go/settle"
)

type fakeCaller struct {
	responses []llmadapter.Response
	requests  []llmadapter.Request
}

type stepCallerFunc func(context.Context, llmadapter.Request) (llmadapter.Response, error)

func (f stepCallerFunc) Call(ctx context.Context, request llmadapter.Request) (llmadapter.Response, error) {
	return f(ctx, request)
}

func (caller *fakeCaller) Call(ctx context.Context, request llmadapter.Request) (llmadapter.Response, error) {
	if err := ctx.Err(); err != nil {
		return llmadapter.Response{}, err
	}
	caller.requests = append(caller.requests, llmadapter.Request{
		Prompt:       request.Prompt,
		OutputSchema: append(json.RawMessage(nil), request.OutputSchema...),
	})
	if len(caller.responses) == 0 {
		return llmadapter.Response{}, nil
	}
	response := caller.responses[0]
	caller.responses = caller.responses[1:]
	return response, nil
}

type stepInput struct {
	Question string
}

type stepOutput struct {
	Status string `json:"status"`
}

func TestRunRendersFirstAttemptWithNoFeedbackAndReturnsSettledOutput(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}}
	var renderFeedbackLens []int

	got, err := Run(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, input stepInput, feedback []Feedback) (string, error) {
			renderFeedbackLens = append(renderFeedbackLens, len(feedback))
			return input.Question, nil
		},
		MaxIter: 1,
	}, stepInput{Question: "ready?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" {
		t.Fatalf("Run output = %#v, want status ok", got)
	}
	if len(renderFeedbackLens) != 1 || renderFeedbackLens[0] != 0 {
		t.Fatalf("render feedback lens = %#v, want [0]", renderFeedbackLens)
	}
	if len(caller.requests) != 1 || caller.requests[0].Prompt != "ready?" {
		t.Fatalf("requests = %#v, want one ready prompt", caller.requests)
	}
}

func TestRunFeedsSanitizedValidationFeedbackIntoNextRender(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	var prompts []string
	var secondFeedback []Feedback

	got, err := Run(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, input stepInput, feedback []Feedback) (string, error) {
			if len(feedback) > 0 {
				secondFeedback = append([]Feedback(nil), feedback...)
			}
			prompt := input.Question
			if len(feedback) > 0 {
				prompt += " " + feedback[0].Codes[0]
			}
			prompts = append(prompts, prompt)
			return prompt, nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (ValidationResult, error) {
			if output.Status == "ok" {
				return ValidationResult{Settled: true}, nil
			}
			return ValidationResult{
				Feedback: []Feedback{{
					Summary: "status must be ok",
					Codes:   []string{"invalid_status"},
				}},
			}, nil
		},
		MaxIter: 2,
	}, stepInput{Question: "ready?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" {
		t.Fatalf("Run output = %#v, want status ok", got)
	}
	if strings.Join(prompts, "|") != "ready?|ready? invalid_status" {
		t.Fatalf("prompts = %#v", prompts)
	}
	if len(secondFeedback) != 1 || secondFeedback[0].Iteration != 1 {
		t.Fatalf("feedback = %#v, want iteration stamped to 1", secondFeedback)
	}
}

func TestRunExhaustedAttemptsWrapsErrUnsettled(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"draft"}`},
	}}

	_, err := Run(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, _ []Feedback) (string, error) {
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, _ stepOutput) (ValidationResult, error) {
			return ValidationResult{Feedback: []Feedback{{Codes: []string{"not_ready"}}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if !errors.Is(err, settle.ErrUnsettled) {
		t.Fatalf("Run error = %v, want errors.Is ErrUnsettled", err)
	}
	if len(caller.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(caller.requests))
	}
}

func TestRunFailsFastOnInvalidConfiguration(t *testing.T) {
	validRender := func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil }
	caller := &fakeCaller{}

	tests := []struct {
		name string
		step Step[stepInput, stepOutput]
		want error
	}{
		{
			name: "invalid max iter",
			step: Step[stepInput, stepOutput]{Caller: caller, Render: validRender},
			want: settle.ErrInvalidMaxIter,
		},
		{
			name: "nil caller",
			step: Step[stepInput, stepOutput]{Render: validRender, MaxIter: 1},
			want: llmadapter.ErrNilCaller,
		},
		{
			name: "nil render",
			step: Step[stepInput, stepOutput]{Caller: caller, MaxIter: 1},
			want: ErrNilRender,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(context.Background(), tt.step, stepInput{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Run error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRunStopsOnDecodeFailureWithoutRetryingAsValidation(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `not-json`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	validateCalls := 0

	_, err := Run(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, _ []Feedback) (string, error) {
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, _ stepOutput) (ValidationResult, error) {
			validateCalls++
			return ValidationResult{Settled: true}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if err == nil {
		t.Fatal("Run accepted invalid JSON")
	}
	if validateCalls != 0 {
		t.Fatalf("validate calls = %d, want 0", validateCalls)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(caller.requests))
	}
}

func TestRunRejectsUnsafeFeedbackBeforeNextRender(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}}
	renderCalls := 0

	_, err := Run(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, _ []Feedback) (string, error) {
			renderCalls++
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, _ stepOutput) (ValidationResult, error) {
			return ValidationResult{Feedback: []Feedback{{Summary: "see https://example.com/secret"}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if !errors.Is(err, ErrUnsafeFeedback) {
		t.Fatalf("Run error = %v, want ErrUnsafeFeedback", err)
	}
	if renderCalls != 1 {
		t.Fatalf("render calls = %d, want 1", renderCalls)
	}
}

func TestRunUsesCustomSanitizer(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	var gotFeedback []Feedback

	_, err := Run(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, feedback []Feedback) (string, error) {
			gotFeedback = append([]Feedback(nil), feedback...)
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (ValidationResult, error) {
			return ValidationResult{Settled: output.Status == "ok", Feedback: []Feedback{{Summary: "raw https://example.com"}}}, nil
		},
		Sanitizer: func(_ []Feedback) ([]Feedback, error) {
			return []Feedback{{Summary: "custom", Codes: []string{"custom_code"}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotFeedback) != 1 || gotFeedback[0].Summary != "custom" {
		t.Fatalf("feedback = %#v, want custom sanitizer output", gotFeedback)
	}
}

func TestRunDetailedExposesAttemptHistory(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	var prompts []string

	got, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, feedback []Feedback) (string, error) {
			if len(feedback) > 0 {
				prompt := "retry " + feedback[0].Codes[0]
				prompts = append(prompts, prompt)
				return prompt, nil
			}
			prompts = append(prompts, "initial")
			return "initial", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (ValidationResult, error) {
			if output.Status == "ok" {
				return ValidationResult{Settled: true}, nil
			}
			return ValidationResult{Feedback: []Feedback{{Codes: []string{"not_ok"}}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output.Status != "ok" {
		t.Fatalf("output = %#v, want ok", got.Output)
	}
	if len(got.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(got.Attempts))
	}
	if strings.Join(prompts, "|") != "initial|retry not_ok" {
		t.Fatalf("rendered prompts = %#v", prompts)
	}
	if got.Attempts[0].Feedback != nil {
		t.Fatalf("first attempt feedback = %#v, want nil", got.Attempts[0].Feedback)
	}
	if len(got.Attempts[1].Feedback) != 1 || got.Attempts[1].Feedback[0].Codes[0] != "not_ok" {
		t.Fatalf("second attempt feedback = %#v, want sanitized retry feedback", got.Attempts[1].Feedback)
	}
	if len(got.Attempts[0].Validation.Feedback) != 1 ||
		got.Attempts[0].Validation.Feedback[0].Iteration != 1 {
		t.Fatalf("attempt validation feedback = %#v, want sanitized history", got.Attempts[0].Validation.Feedback)
	}
}

func TestRunDetailedRecordsRenderFailure(t *testing.T) {
	renderErr := errors.New("render")
	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller:  &fakeCaller{},
		Render:  func(context.Context, stepInput, []Feedback) (string, error) { return "", renderErr },
		MaxIter: 1,
	}, stepInput{})
	assertStepFailure(t, result, err, StageRender, renderErr, false)
}

func TestRunDetailedRecordsRequestFailure(t *testing.T) {
	called := false
	result, err := RunDetailed(context.Background(), Step[stepInput, chan int]{
		Caller: stepCallerFunc(func(context.Context, llmadapter.Request) (llmadapter.Response, error) {
			called = true
			return llmadapter.Response{}, nil
		}),
		Render:  func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
		MaxIter: 1,
	}, stepInput{})
	var stepErr *StepError
	if !errors.As(err, &stepErr) || stepErr.Stage != StageRequest || len(result.Attempts) != 1 {
		t.Fatalf("result = %#v, err = %v; want request failure", result, err)
	}
	if called {
		t.Fatal("caller invoked after request failure")
	}
}

func TestRunDetailedRecordsPartialCallAndDecodeFailures(t *testing.T) {
	providerErr := errors.New("provider")
	callResponse := llmadapter.Response{FinalResponse: `{"status":"partial"}`}
	callResult, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: stepCallerFunc(func(context.Context, llmadapter.Request) (llmadapter.Response, error) {
			return callResponse, providerErr
		}),
		Render:  func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
		MaxIter: 1,
	}, stepInput{})
	assertStepFailure(t, callResult, err, StageCall, providerErr, false)
	if callResult.Attempts[0].Call.Response.FinalResponse != callResponse.FinalResponse {
		t.Fatalf("partial call response = %#v", callResult.Attempts[0].Call.Response)
	}

	decodeResponse := llmadapter.Response{FinalResponse: `{"status":3}`}
	decodeResult, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: stepCallerFunc(func(context.Context, llmadapter.Request) (llmadapter.Response, error) {
			return decodeResponse, nil
		}),
		Render:  func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
		MaxIter: 1,
	}, stepInput{})
	assertStepFailure(t, decodeResult, err, StageDecode, nil, false)
	if decodeResult.Attempts[0].Call.Response.FinalResponse != decodeResponse.FinalResponse {
		t.Fatalf("decode response = %#v", decodeResult.Attempts[0].Call.Response)
	}
}

func TestRunDetailedPreservesOutputOnValidationAndSanitizeFailures(t *testing.T) {
	validationErr := errors.New("validate")
	validationResult, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
		Render: func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
		Validate: func(context.Context, stepInput, stepOutput) (ValidationResult, error) {
			return ValidationResult{Feedback: []Feedback{{Codes: []string{"invalid"}}}}, validationErr
		},
		MaxIter: 1,
	}, stepInput{})
	assertStepFailure(t, validationResult, err, StageValidate, validationErr, true)

	sanitizeResult, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
		Render: func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
		Validate: func(context.Context, stepInput, stepOutput) (ValidationResult, error) {
			return ValidationResult{Feedback: []Feedback{{Summary: "https://unsafe.example"}}}, nil
		},
		MaxIter: 1,
	}, stepInput{})
	assertStepFailure(t, sanitizeResult, err, StageSanitize, ErrUnsafeFeedback, true)
}

func assertStepFailure[O any](t *testing.T, result Result[O], err error, stage Stage, cause error, hasOutput bool) {
	t.Helper()
	var stepErr *StepError
	if !errors.As(err, &stepErr) || stepErr.Stage != stage || stepErr.Iteration != 1 {
		t.Fatalf("error = %v, want iteration 1 stage %s", err, stage)
	}
	if cause != nil && !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause %v", err, cause)
	}
	if result.HasOutput != hasOutput || len(result.Attempts) != 1 || result.Attempts[0].Err == nil {
		t.Fatalf("result = %#v, want one failed attempt with HasOutput=%v", result, hasOutput)
	}
	if !errors.Is(result.Attempts[0].Err, stepErr.Err) {
		t.Fatalf("attempt error = %v, returned error = %v", result.Attempts[0].Err, err)
	}
}
