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
			want: llmadapter.ErrNilRender,
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
