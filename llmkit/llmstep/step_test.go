package llmstep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
	"github.com/ronhuafeng/llm-go/llmkit/settle"
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

func TestRunDetailedFinalUnsettledAttemptSkipsRetryFeedbackSanitization(t *testing.T) {
	validation := ValidationResult{Feedback: []Feedback{{
		Summary:   "terminal validator evidence",
		Codes:     []string{"not_ready"},
		Locations: []string{"status"},
	}}}
	sanitizerCalls := 0

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
		Render: func(context.Context, stepInput, []Feedback) (string, error) {
			return "prompt", nil
		},
		Validate: func(context.Context, stepInput, stepOutput) (ValidationResult, error) {
			return validation, nil
		},
		Sanitizer: func([]Feedback) ([]Feedback, error) {
			sanitizerCalls++
			return nil, ErrUnsafeFeedback
		},
		MaxIter: 1,
	}, stepInput{})

	if !errors.Is(err, settle.ErrUnsettled) {
		t.Fatalf("RunDetailed error = %v, want ErrUnsettled", err)
	}
	if errors.Is(err, ErrUnsafeFeedback) {
		t.Fatalf("RunDetailed error = %v, must not expose terminal sanitizer error", err)
	}
	if sanitizerCalls != 0 {
		t.Fatalf("sanitizer calls = %d, want 0", sanitizerCalls)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(result.Attempts))
	}
	attempt := result.Attempts[0]
	if attempt.RetryFeedback != nil {
		t.Fatalf("RetryFeedback = %#v, want nil without a retry", attempt.RetryFeedback)
	}
	if len(attempt.Validation.Feedback) != 1 ||
		attempt.Validation.Feedback[0].Summary != validation.Feedback[0].Summary ||
		attempt.Validation.Feedback[0].Codes[0] != "not_ready" ||
		attempt.Validation.Feedback[0].Locations[0] != "status" {
		t.Fatalf("Validation = %#v, want original validator decision", attempt.Validation)
	}
}

func TestRunDetailedExhaustionPublishesRetryFeedbackOnlyForRealRetries(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"first"}`},
		{FinalResponse: `{"status":"final"}`},
	}}
	var renderedFeedback [][]Feedback
	sanitizerCalls := 0

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, feedback []Feedback) (string, error) {
			renderedFeedback = append(renderedFeedback, copyFeedback(feedback))
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (ValidationResult, error) {
			return ValidationResult{Feedback: []Feedback{{
				Summary: "validator " + output.Status,
				Codes:   []string{"raw_" + output.Status},
			}}}, nil
		},
		Sanitizer: func(feedback []Feedback) ([]Feedback, error) {
			sanitizerCalls++
			return []Feedback{{Codes: []string{"safe_retry"}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})

	if !errors.Is(err, settle.ErrUnsettled) {
		t.Fatalf("RunDetailed error = %v, want ErrUnsettled", err)
	}
	if sanitizerCalls != 1 {
		t.Fatalf("sanitizer calls = %d, want 1 for the only real retry", sanitizerCalls)
	}
	if len(renderedFeedback) != 2 || renderedFeedback[0] != nil ||
		len(renderedFeedback[1]) != 1 || renderedFeedback[1][0].Codes[0] != "safe_retry" {
		t.Fatalf("rendered feedback = %#v, want only sanitized feedback on retry", renderedFeedback)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(result.Attempts))
	}
	if len(result.Attempts[0].RetryFeedback) != 1 ||
		result.Attempts[0].RetryFeedback[0].Iteration != 1 ||
		result.Attempts[0].RetryFeedback[0].Codes[0] != "safe_retry" {
		t.Fatalf("first RetryFeedback = %#v, want sanitized retry evidence", result.Attempts[0].RetryFeedback)
	}
	if result.Attempts[1].RetryFeedback != nil {
		t.Fatalf("final RetryFeedback = %#v, want nil", result.Attempts[1].RetryFeedback)
	}
	finalValidation := result.Attempts[1].Validation.Feedback
	if len(finalValidation) != 1 || finalValidation[0].Summary != "validator final" || finalValidation[0].Codes[0] != "raw_final" {
		t.Fatalf("final Validation = %#v, want original validator decision", finalValidation)
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

func TestRunDetailedSeparatesValidationFromRetryFeedback(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	validatorDecision := Feedback{
		Summary:   "validator-only detail",
		Codes:     []string{"raw_code"},
		Locations: []string{"private_source"},
	}
	var rendered []Feedback
	var sanitizerOutput []Feedback

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, feedback []Feedback) (string, error) {
			if len(feedback) > 0 {
				rendered = copyFeedback(feedback)
				feedback[0].Codes[0] = "render_mutation"
			}
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (ValidationResult, error) {
			if output.Status == "ok" {
				return ValidationResult{Settled: true}, nil
			}
			return ValidationResult{Feedback: []Feedback{validatorDecision}}, nil
		},
		Sanitizer: func(feedback []Feedback) ([]Feedback, error) {
			feedback[0].Summary = "sanitizer input mutation"
			feedback[0].Codes[0] = "sanitizer_input_mutation"
			sanitizerOutput = []Feedback{{Codes: []string{"safe_code"}}}
			return sanitizerOutput, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(result.Attempts))
	}
	validation := result.Attempts[0].Validation.Feedback
	if len(validation) != 1 || validation[0].Iteration != 0 || validation[0].Summary != validatorDecision.Summary || validation[0].Codes[0] != "raw_code" || validation[0].Locations[0] != "private_source" {
		t.Fatalf("Validation = %#v, want original validator decision", validation)
	}
	retry := result.Attempts[0].RetryFeedback
	if len(retry) != 1 || retry[0].Iteration != 1 || retry[0].Summary != "" || retry[0].Codes[0] != "safe_code" || retry[0].Locations != nil {
		t.Fatalf("RetryFeedback = %#v, want sanitized and stamped feedback", retry)
	}
	if len(rendered) != 1 || rendered[0].Codes[0] != "safe_code" || result.Attempts[1].Feedback[0].Codes[0] != "safe_code" || result.Attempts[0].RetryFeedback[0].Codes[0] != "safe_code" {
		t.Fatalf("render feedback or snapshots aliased: rendered=%#v attempts=%#v", rendered, result.Attempts)
	}
	if sanitizerOutput[0].Iteration != 0 || sanitizerOutput[0].Codes[0] != "safe_code" {
		t.Fatalf("framework mutated sanitizer-owned output: %#v", sanitizerOutput)
	}
}

func TestRunDetailedStampsValidatorRetryFeedbackWithFrameworkIteration(t *testing.T) {
	for _, validatorIteration := range []int{0, -1, 999} {
		t.Run(fmt.Sprintf("validator iteration %d", validatorIteration), func(t *testing.T) {
			caller := &fakeCaller{responses: []llmadapter.Response{
				{FinalResponse: `{"status":"draft"}`},
				{FinalResponse: `{"status":"ok"}`},
			}}
			var rendered []Feedback

			result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
				Caller: caller,
				Render: func(_ context.Context, _ stepInput, feedback []Feedback) (string, error) {
					if len(feedback) > 0 {
						rendered = copyFeedback(feedback)
					}
					return "prompt", nil
				},
				Validate: func(_ context.Context, _ stepInput, output stepOutput) (ValidationResult, error) {
					if output.Status == "ok" {
						return ValidationResult{Settled: true}, nil
					}
					return ValidationResult{Feedback: []Feedback{{Iteration: validatorIteration, Codes: []string{"safe_retry"}}}}, nil
				},
				MaxIter: 2,
			}, stepInput{})
			if err != nil {
				t.Fatal(err)
			}

			if got := result.Attempts[0].Validation.Feedback[0].Iteration; got != validatorIteration {
				t.Fatalf("Validation iteration = %d, want validator-provided %d", got, validatorIteration)
			}
			if len(rendered) != 1 || rendered[0].Iteration != 1 {
				t.Fatalf("rendered retry feedback = %#v, want framework iteration 1", rendered)
			}
			if retry := result.Attempts[0].RetryFeedback; len(retry) != 1 || retry[0].Iteration != 1 {
				t.Fatalf("RetryFeedback = %#v, want framework iteration 1", retry)
			}
		})
	}
}

func TestRunDetailedStampsCustomSanitizerRetryFeedbackWithFrameworkIteration(t *testing.T) {
	for _, sanitizerIteration := range []int{0, -1, 999} {
		t.Run(fmt.Sprintf("sanitizer iteration %d", sanitizerIteration), func(t *testing.T) {
			caller := &fakeCaller{responses: []llmadapter.Response{
				{FinalResponse: `{"status":"draft"}`},
				{FinalResponse: `{"status":"ok"}`},
			}}
			var rendered []Feedback

			result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
				Caller: caller,
				Render: func(_ context.Context, _ stepInput, feedback []Feedback) (string, error) {
					if len(feedback) > 0 {
						rendered = copyFeedback(feedback)
					}
					return "prompt", nil
				},
				Validate: func(_ context.Context, _ stepInput, output stepOutput) (ValidationResult, error) {
					if output.Status == "ok" {
						return ValidationResult{Settled: true}, nil
					}
					return ValidationResult{Feedback: []Feedback{{Iteration: -1, Codes: []string{"validator_detail"}}}}, nil
				},
				Sanitizer: func([]Feedback) ([]Feedback, error) {
					return []Feedback{{Iteration: sanitizerIteration, Codes: []string{"safe_retry"}}}, nil
				},
				MaxIter: 2,
			}, stepInput{})
			if err != nil {
				t.Fatal(err)
			}

			if got := result.Attempts[0].Validation.Feedback[0].Iteration; got != -1 {
				t.Fatalf("Validation iteration = %d, want validator-provided -1", got)
			}
			if len(rendered) != 1 || rendered[0].Iteration != 1 {
				t.Fatalf("rendered retry feedback = %#v, want framework iteration 1", rendered)
			}
			if retry := result.Attempts[0].RetryFeedback; len(retry) != 1 || retry[0].Iteration != 1 {
				t.Fatalf("RetryFeedback = %#v, want framework iteration 1", retry)
			}
		})
	}
}

func TestRunDetailedPreservesValidatorEmptySliceShape(t *testing.T) {
	t.Run("outer feedback", func(t *testing.T) {
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}},
			Render: func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
			Validate: func(context.Context, stepInput, stepOutput) (ValidationResult, error) {
				return ValidationResult{Settled: true, Feedback: make([]Feedback, 0)}, nil
			},
			MaxIter: 1,
		}, stepInput{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Attempts[0].Validation.Feedback == nil || len(result.Attempts[0].Validation.Feedback) != 0 {
			t.Fatalf("Validation feedback = %#v, want non-nil empty slice", result.Attempts[0].Validation.Feedback)
		}
	})

	t.Run("nested feedback", func(t *testing.T) {
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
			Render: func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
			Validate: func(context.Context, stepInput, stepOutput) (ValidationResult, error) {
				return ValidationResult{Feedback: []Feedback{{Codes: make([]string, 0), Locations: make([]string, 0)}}}, nil
			},
			Sanitizer: func([]Feedback) ([]Feedback, error) { return nil, nil },
			MaxIter:   1,
		}, stepInput{})
		if !errors.Is(err, settle.ErrUnsettled) {
			t.Fatalf("error = %v, want ErrUnsettled", err)
		}
		feedback := result.Attempts[0].Validation.Feedback
		if len(feedback) != 1 || feedback[0].Codes == nil || feedback[0].Locations == nil || len(feedback[0].Codes) != 0 || len(feedback[0].Locations) != 0 {
			t.Fatalf("Validation feedback = %#v, want non-nil empty nested slices", feedback)
		}
	})
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
		got.Attempts[0].Validation.Feedback[0].Iteration != 0 {
		t.Fatalf("attempt validation feedback = %#v, want original validator decision", got.Attempts[0].Validation.Feedback)
	}
	if len(got.Attempts[0].RetryFeedback) != 1 || got.Attempts[0].RetryFeedback[0].Iteration != 1 || got.Attempts[0].RetryFeedback[0].Codes[0] != "not_ok" {
		t.Fatalf("attempt retry feedback = %#v, want sanitized and stamped history", got.Attempts[0].RetryFeedback)
	}
}

func TestRunDetailedPublishesIsolatedFeedbackSlices(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}}
	source := []Feedback{{Summary: "not ready", Codes: []string{"not_ready"}, Locations: []string{"status"}}}
	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(context.Context, stepInput, []Feedback) (string, error) { return "prompt", nil },
		Validate: func(context.Context, stepInput, stepOutput) (ValidationResult, error) {
			return ValidationResult{Feedback: source}, nil
		},
		MaxIter: 1,
	}, stepInput{})
	if !errors.Is(err, settle.ErrUnsettled) {
		t.Fatalf("error = %v, want ErrUnsettled", err)
	}

	source[0].Summary = "mutated"
	source[0].Codes[0] = "mutated"
	source[0].Locations[0] = "mutated"
	got := result.Attempts[0].Validation.Feedback[0]
	if got.Summary != "not ready" || got.Codes[0] != "not_ready" || got.Locations[0] != "status" {
		t.Fatalf("published feedback changed with validator source: %#v", got)
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
		MaxIter: 2,
	}, stepInput{})
	assertStepFailure(t, sanitizeResult, err, StageSanitize, ErrUnsafeFeedback, true)
	if sanitizeResult.Attempts[0].Call.Response.FinalResponse != `{"status":"draft"}` {
		t.Fatalf("sanitize failure lost call evidence: %#v", sanitizeResult.Attempts[0].Call)
	}
	validation := sanitizeResult.Attempts[0].Validation.Feedback
	if len(validation) != 1 || validation[0].Summary != "https://unsafe.example" {
		t.Fatalf("sanitize failure lost validator decision: %#v", validation)
	}
	if sanitizeResult.Attempts[0].RetryFeedback != nil {
		t.Fatalf("sanitize failure published retry feedback: %#v", sanitizeResult.Attempts[0].RetryFeedback)
	}
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
