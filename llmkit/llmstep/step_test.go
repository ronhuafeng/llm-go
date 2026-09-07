package llmstep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

type fakeCaller struct {
	responses []llmadapter.Response
	requests  []llmadapter.Request
}

type stepCallerFunc func(context.Context, llmadapter.Request) (llmadapter.Response, error)

func (f stepCallerFunc) Call(ctx context.Context, request llmadapter.Request) (llmadapter.Response, error) {
	return f(ctx, request)
}

func acceptValidate(context.Context, stepInput, stepOutput) (Judgment, error) {
	return Judgment{Accepted: true}, nil
}

func acceptAny[O any](context.Context, stepInput, O) (Judgment, error) {
	return Judgment{Accepted: true}, nil
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

func TestRunRendersFirstAttemptWithNoRepairAndReturnsAcceptedOutput(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}}
	var renderRepairLens []int

	got, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, input stepInput, repair []Repair) (string, error) {
			renderRepairLens = append(renderRepairLens, len(repair))
			return input.Question, nil
		},
		Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
			return Judgment{Accepted: true}, nil
		},
		MaxIter: 1,
	}, stepInput{Question: "ready?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output.Status != "ok" || !got.HasOutput || len(got.Attempts) != 1 {
		t.Fatalf("RunDetailed result = %#v, want accepted status ok with one attempt", got)
	}
	if len(renderRepairLens) != 1 || renderRepairLens[0] != 0 {
		t.Fatalf("render feedback lens = %#v, want [0]", renderRepairLens)
	}
	if len(caller.requests) != 1 || caller.requests[0].Prompt != "ready?" {
		t.Fatalf("requests = %#v, want one ready prompt", caller.requests)
	}
}

func TestRunRejectsNilValidateBeforeRenderOrCall(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}}
	rendered := false
	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(context.Context, stepInput, []Repair) (string, error) {
			rendered = true
			return "prompt", nil
		},
		MaxIter: 2,
	}, stepInput{})
	if !errors.Is(err, ErrNilValidate) {
		t.Fatalf("RunDetailed error = %v, want ErrNilValidate", err)
	}
	if rendered {
		t.Fatal("Render ran for a step with Validate == nil")
	}
	if len(caller.requests) != 0 {
		t.Fatalf("Caller.Call ran %d times, want 0", len(caller.requests))
	}
	if result.HasOutput || len(result.Attempts) != 0 {
		t.Fatalf("nil Validate published attempt evidence: %#v", result)
	}
}

func TestRunDetailedDistinguishesJudgmentStates(t *testing.T) {
	t.Run("rejected judgment", func(t *testing.T) {
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
			Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
			Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
				return Judgment{Findings: []Finding{{Codes: []string{"rejected"}}}}, nil
			},
			MaxIter: 1,
		}, stepInput{})
		if !errors.Is(err, ErrUnsettled) || result.Attempts[0].Judgment == nil || result.Attempts[0].Judgment.Accepted {
			t.Fatalf("rejected path: err=%v judgment=%#v", err, result.Attempts[0].Judgment)
		}
		if result.Attempts[0].NextRepair != nil {
			t.Fatalf("final rejected attempt synthesized repair: %#v", result.Attempts[0].NextRepair)
		}
	})
	t.Run("accepted judgment", func(t *testing.T) {
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}},
			Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
			Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
				return Judgment{Accepted: true, Findings: []Finding{{Codes: []string{"ok"}}}}, nil
			},
			MaxIter: 1,
		}, stepInput{})
		if err != nil || result.Attempts[0].Judgment == nil || !result.Attempts[0].Judgment.Accepted {
			t.Fatalf("accepted path: err=%v judgment=%#v", err, result.Attempts[0].Judgment)
		}
	})
	t.Run("judgment failure", func(t *testing.T) {
		judgeErr := errors.New("judge")
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}},
			Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
			Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
				return Judgment{Findings: []Finding{{Codes: []string{"partial"}}}}, judgeErr
			},
			MaxIter: 1,
		}, stepInput{})
		var stepErr *StepError
		if !errors.As(err, &stepErr) || stepErr.Stage != StageValidate || !errors.Is(err, judgeErr) {
			t.Fatalf("judgment failure path: err=%v", err)
		}
		if result.Attempts[0].Judgment == nil || result.Attempts[0].Judgment.Accepted || result.Attempts[0].Judgment.Findings[0].Codes[0] != "partial" {
			t.Fatalf("judgment failure lost findings: %#v", result.Attempts[0].Judgment)
		}
	})
	t.Run("decode failure has no judgment", func(t *testing.T) {
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller:   &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `not-json`}}},
			Render:   func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
			Validate: acceptValidate,
			MaxIter:  1,
		}, stepInput{})
		var stepErr *StepError
		if !errors.As(err, &stepErr) || stepErr.Stage != StageDecode {
			t.Fatalf("decode failure path: err=%v", err)
		}
		if result.Attempts[0].Judgment != nil {
			t.Fatalf("decode failure published judgment: %#v", result.Attempts[0].Judgment)
		}
	})
}

func TestRunFeedsSanitizedFindingsAsRepairIntoNextRender(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	var prompts []string
	var secondRepair []Repair

	got, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, input stepInput, repair []Repair) (string, error) {
			if len(repair) > 0 {
				secondRepair = append([]Repair(nil), repair...)
			}
			prompt := input.Question
			if len(repair) > 0 {
				prompt += " " + repair[0].Codes[0]
			}
			prompts = append(prompts, prompt)
			return prompt, nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (Judgment, error) {
			if output.Status == "ok" {
				return Judgment{Accepted: true}, nil
			}
			return Judgment{
				Findings: []Finding{{
					Codes: []string{"invalid_status"},
				}},
			}, nil
		},
		MaxIter: 2,
	}, stepInput{Question: "ready?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output.Status != "ok" || !got.HasOutput || len(got.Attempts) != 2 {
		t.Fatalf("RunDetailed result = %#v, want accepted status ok with two attempts", got)
	}
	if strings.Join(prompts, "|") != "ready?|ready? invalid_status" {
		t.Fatalf("prompts = %#v", prompts)
	}
	if len(secondRepair) != 1 || secondRepair[0].Iteration != 1 {
		t.Fatalf("feedback = %#v, want iteration stamped to 1", secondRepair)
	}
}

func TestRunExhaustedAttemptsWrapsErrUnsettled(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"draft"}`},
	}}

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, _ []Repair) (string, error) {
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, _ stepOutput) (Judgment, error) {
			return Judgment{Findings: []Finding{{Codes: []string{"not_ready"}}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if !errors.Is(err, ErrUnsettled) {
		t.Fatalf("RunDetailed error = %v, want errors.Is ErrUnsettled", err)
	}
	if !result.HasOutput || result.Output.Status != "draft" || len(result.Attempts) != 2 {
		t.Fatalf("exhaustion discarded latest proposition or attempts: %#v", result)
	}
	if len(caller.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(caller.requests))
	}
}

func TestRunDetailedFinalUnsettledAttemptSkipsNextRepairSanitization(t *testing.T) {
	validation := Judgment{Findings: []Finding{{
		Summary:   "terminal validator evidence",
		Codes:     []string{"not_ready"},
		Locations: []string{"status"},
	}}}
	sanitizerCalls := 0

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
		Render: func(context.Context, stepInput, []Repair) (string, error) {
			return "prompt", nil
		},
		Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
			return validation, nil
		},
		Sanitizer: func([]Finding) ([]Repair, error) {
			sanitizerCalls++
			return nil, ErrUnsafeRepair
		},
		MaxIter: 1,
	}, stepInput{})

	if !errors.Is(err, ErrUnsettled) {
		t.Fatalf("RunDetailed error = %v, want ErrUnsettled", err)
	}
	if errors.Is(err, ErrUnsafeRepair) {
		t.Fatalf("RunDetailed error = %v, must not expose terminal sanitizer error", err)
	}
	if sanitizerCalls != 0 {
		t.Fatalf("sanitizer calls = %d, want 0", sanitizerCalls)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(result.Attempts))
	}
	attempt := result.Attempts[0]
	if attempt.NextRepair != nil {
		t.Fatalf("NextRepair = %#v, want nil without a retry", attempt.NextRepair)
	}
	if len(attempt.Judgment.Findings) != 1 ||
		attempt.Judgment.Findings[0].Summary != validation.Findings[0].Summary ||
		attempt.Judgment.Findings[0].Codes[0] != "not_ready" ||
		attempt.Judgment.Findings[0].Locations[0] != "status" {
		t.Fatalf("Judgment = %#v, want original validator findings", attempt.Judgment)
	}
}

func TestRunDetailedExhaustionPublishesNextRepairOnlyForRealRetries(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"first"}`},
		{FinalResponse: `{"status":"final"}`},
	}}
	var renderedRepair [][]Repair
	sanitizerCalls := 0

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, repair []Repair) (string, error) {
			renderedRepair = append(renderedRepair, copyRepair(repair))
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (Judgment, error) {
			return Judgment{Findings: []Finding{{
				Summary: "validator " + output.Status,
				Codes:   []string{"raw_" + output.Status},
			}}}, nil
		},
		Sanitizer: func([]Finding) ([]Repair, error) {
			sanitizerCalls++
			return []Repair{{Codes: []string{"safe_retry"}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})

	if !errors.Is(err, ErrUnsettled) {
		t.Fatalf("RunDetailed error = %v, want ErrUnsettled", err)
	}
	if sanitizerCalls != 1 {
		t.Fatalf("sanitizer calls = %d, want 1 for the only real retry", sanitizerCalls)
	}
	if len(renderedRepair) != 2 || renderedRepair[0] != nil ||
		len(renderedRepair[1]) != 1 || renderedRepair[1][0].Codes[0] != "safe_retry" {
		t.Fatalf("rendered feedback = %#v, want only sanitized feedback on retry", renderedRepair)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(result.Attempts))
	}
	if len(result.Attempts[0].NextRepair) != 1 ||
		result.Attempts[0].NextRepair[0].Iteration != 1 ||
		result.Attempts[0].NextRepair[0].Codes[0] != "safe_retry" {
		t.Fatalf("first NextRepair = %#v, want sanitized retry evidence", result.Attempts[0].NextRepair)
	}
	if result.Attempts[1].NextRepair != nil {
		t.Fatalf("final NextRepair = %#v, want nil", result.Attempts[1].NextRepair)
	}
	finalValidation := result.Attempts[1].Judgment.Findings
	if len(finalValidation) != 1 || finalValidation[0].Summary != "validator final" || finalValidation[0].Codes[0] != "raw_final" {
		t.Fatalf("final Validation = %#v, want original validator decision", finalValidation)
	}
}

func TestRunFailsFastOnInvalidConfiguration(t *testing.T) {
	validRender := func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil }
	caller := &fakeCaller{}

	tests := []struct {
		name string
		step Step[stepInput, stepOutput]
		want error
	}{
		{
			name: "invalid max iter",
			step: Step[stepInput, stepOutput]{Caller: caller, Render: validRender},
			want: ErrInvalidMaxIter,
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
		{
			name: "nil validate",
			step: Step[stepInput, stepOutput]{Caller: caller, Render: validRender, MaxIter: 1},
			want: ErrNilValidate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RunDetailed(context.Background(), tt.step, stepInput{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("RunDetailed error = %v, want %v", err, tt.want)
			}
			if result.HasOutput || len(result.Attempts) != 0 {
				t.Fatalf("invalid configuration published attempt evidence: %#v", result)
			}
		})
	}
}

func TestRunDetailedFailsFastOnTypedNilCaller(t *testing.T) {
	var caller *fakeCaller
	renderCalls := 0

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(context.Context, stepInput, []Repair) (string, error) {
			renderCalls++
			return "prompt", nil
		},
		MaxIter: 1,
	}, stepInput{})

	if !errors.Is(err, llmadapter.ErrNilCaller) {
		t.Fatalf("RunDetailed error = %v, want ErrNilCaller", err)
	}
	if renderCalls != 0 {
		t.Fatalf("render calls = %d, want 0", renderCalls)
	}
	if len(result.Attempts) != 0 {
		t.Fatalf("attempts = %#v, want no manufactured attempt", result.Attempts)
	}
}

func TestRunDetailedRecordsCancellationAfterSuccessfulRender(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	caller := &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}}

	result, err := RunDetailed(ctx, Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(context.Context, stepInput, []Repair) (string, error) {
			cancel()
			return "prompt", nil
		},
		Validate: acceptValidate,
		MaxIter:  1,
	}, stepInput{})

	var stepErr *StepError
	if !errors.As(err, &stepErr) || stepErr.Stage != StageRender || !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDetailed error = %v, want render-stage context.Canceled", err)
	}
	if result.HasOutput || len(result.Attempts) != 1 || result.Attempts[0].Err == nil {
		t.Fatalf("result = %#v, want one render-stage failed attempt without output", result)
	}
	if len(caller.requests) != 0 {
		t.Fatalf("caller requests = %d, want 0 after observed cancellation", len(caller.requests))
	}
}

func TestRunDetailedRecordsCancellationAfterSuccessfulProviderCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	response := llmadapter.Response{FinalResponse: `{"status":"ok"}`}

	result, err := RunDetailed(ctx, Step[stepInput, stepOutput]{
		Caller: stepCallerFunc(func(context.Context, llmadapter.Request) (llmadapter.Response, error) {
			cancel()
			return response, nil
		}),
		Render:   func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: acceptValidate,
		MaxIter:  1,
	}, stepInput{})

	var stepErr *StepError
	if !errors.As(err, &stepErr) || stepErr.Stage != StageCall || !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDetailed error = %v, want call-stage context.Canceled", err)
	}
	if result.HasOutput || len(result.Attempts) != 1 {
		t.Fatalf("result = %#v, want one call-stage failed attempt without decoded output", result)
	}
	if got := result.Attempts[0].Call.Response.FinalResponse; got != response.FinalResponse {
		t.Fatalf("partial response = %q, want %q", got, response.FinalResponse)
	}
}

func TestRunDetailedRecordsCancellationAfterSuccessfulValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	validation := Judgment{Accepted: true, Findings: []Finding{{Codes: []string{"accepted"}}}}

	result, err := RunDetailed(ctx, Step[stepInput, stepOutput]{
		Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}},
		Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
			cancel()
			return validation, nil
		},
		MaxIter: 1,
	}, stepInput{})

	var stepErr *StepError
	if !errors.As(err, &stepErr) || stepErr.Stage != StageValidate || !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDetailed error = %v, want validate-stage context.Canceled", err)
	}
	if !result.HasOutput || result.Output.Status != "ok" || len(result.Attempts) != 1 {
		t.Fatalf("result = %#v, want typed output plus one validation-stage failure", result)
	}
	attempt := result.Attempts[0]
	if !attempt.Judgment.Accepted || len(attempt.Judgment.Findings) != 1 || attempt.Judgment.Findings[0].Codes[0] != "accepted" {
		t.Fatalf("Judgment = %#v, want completed accepted judgment", attempt.Judgment)
	}
}

func TestRunStopsOnDecodeFailureWithoutRetryingAsValidation(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `not-json`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	validateCalls := 0

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, _ []Repair) (string, error) {
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, _ stepOutput) (Judgment, error) {
			validateCalls++
			return Judgment{Accepted: true}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if err == nil {
		t.Fatal("RunDetailed accepted invalid JSON")
	}
	if result.Attempts[0].Call.Response.FinalResponse != "not-json" {
		t.Fatalf("decode failure discarded call evidence: %#v", result)
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

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, _ []Repair) (string, error) {
			renderCalls++
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, _ stepOutput) (Judgment, error) {
			return Judgment{Findings: []Finding{{Summary: "see https://example.com/secret"}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if !errors.Is(err, ErrUnsafeRepair) {
		t.Fatalf("RunDetailed error = %v, want ErrUnsafeRepair", err)
	}
	if !result.HasOutput || result.Output.Status != "draft" || result.Attempts[0].Judgment == nil {
		t.Fatalf("unsafe repair discarded proposition or judgment: %#v", result)
	}
	if renderCalls != 1 {
		t.Fatalf("render calls = %d, want 1", renderCalls)
	}
}

func TestStrictRepairSanitizerRejectsFreeFormSummaries(t *testing.T) {
	tests := map[string]string{
		"AWS access key":      "AKIAIOSFODNN7EXAMPLE",
		"GitHub token":        "ghp_1234567890abcdefghijklmnopqrstuvwxyz",
		"PEM header":          "-----BEGIN PRIVATE KEY-----",
		"database connection": "postgres://db.internal/app",
		"email address":       "alice@example.com",
		"phone number":        "+1 (415) 555-0132",
		"customer identifier": "customer-48291",
		"business fragment":   "premium renewal approved",
		"source fragment":     "invoiceTotal := rate * units",
	}

	for name, summary := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := StrictRepairSanitizer([]Finding{{Summary: summary}})
			if !errors.Is(err, ErrUnsafeRepair) {
				t.Fatalf("StrictRepairSanitizer(%q) error = %v, want ErrUnsafeRepair", summary, err)
			}
		})
	}
}

func TestStrictRepairSanitizerAllowsStructuredFields(t *testing.T) {
	got, err := StrictRepairSanitizer([]Finding{{
		Summary:   "   ",
		Codes:     []string{" invalid_status "},
		Locations: []string{" result.status "},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Iteration != 0 || got[0].Summary != "" || got[0].Codes[0] != "invalid_status" || got[0].Locations[0] != "result.status" {
		t.Fatalf("StrictRepairSanitizer result = %#v, want trimmed structured fields", got)
	}
}

func TestRunUsesCustomSanitizer(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	var gotRepair []Repair

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, repair []Repair) (string, error) {
			gotRepair = append([]Repair(nil), repair...)
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (Judgment, error) {
			return Judgment{Accepted: output.Status == "ok", Findings: []Finding{{Summary: "raw https://example.com"}}}, nil
		},
		Sanitizer: func(_ []Finding) ([]Repair, error) {
			return []Repair{{Summary: "custom", Codes: []string{"custom_code"}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasOutput || result.Output.Status != "ok" || len(result.Attempts) != 2 {
		t.Fatalf("custom sanitizer result discarded attempts: %#v", result)
	}
	if len(gotRepair) != 1 || gotRepair[0].Summary != "custom" {
		t.Fatalf("feedback = %#v, want custom sanitizer output", gotRepair)
	}
}

func TestRunDetailedSeparatesValidationFromNextRepair(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{
		{FinalResponse: `{"status":"draft"}`},
		{FinalResponse: `{"status":"ok"}`},
	}}
	validatorDecision := Finding{
		Summary:   "validator-only detail",
		Codes:     []string{"raw_code"},
		Locations: []string{"private_source"},
	}
	var rendered []Repair
	var sanitizerOutput []Repair

	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(_ context.Context, _ stepInput, repair []Repair) (string, error) {
			if len(repair) > 0 {
				rendered = copyRepair(repair)
				repair[0].Codes[0] = "render_mutation"
			}
			return "prompt", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (Judgment, error) {
			if output.Status == "ok" {
				return Judgment{Accepted: true}, nil
			}
			return Judgment{Findings: []Finding{validatorDecision}}, nil
		},
		Sanitizer: func(findings []Finding) ([]Repair, error) {
			findings[0].Summary = "sanitizer input mutation"
			findings[0].Codes[0] = "sanitizer_input_mutation"
			sanitizerOutput = []Repair{{Summary: "model-safe detail", Codes: []string{"safe_code"}}}
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
	validation := result.Attempts[0].Judgment.Findings
	if len(validation) != 1 || validation[0].Summary != validatorDecision.Summary || validation[0].Codes[0] != "raw_code" || validation[0].Locations[0] != "private_source" {
		t.Fatalf("Judgment findings = %#v, want original validator findings", validation)
	}
	retry := result.Attempts[0].NextRepair
	if len(retry) != 1 || retry[0].Iteration != 1 || retry[0].Summary != "model-safe detail" || retry[0].Codes[0] != "safe_code" || retry[0].Locations != nil {
		t.Fatalf("NextRepair = %#v, want sanitized and stamped feedback", retry)
	}
	if len(rendered) != 1 || rendered[0].Summary != "model-safe detail" || rendered[0].Codes[0] != "safe_code" || result.Attempts[1].Repair[0].Summary != "model-safe detail" || result.Attempts[1].Repair[0].Codes[0] != "safe_code" || result.Attempts[0].NextRepair[0].Summary != "model-safe detail" || result.Attempts[0].NextRepair[0].Codes[0] != "safe_code" {
		t.Fatalf("render feedback or snapshots aliased: rendered=%#v attempts=%#v", rendered, result.Attempts)
	}
	if sanitizerOutput[0].Iteration != 0 || sanitizerOutput[0].Summary != "model-safe detail" || sanitizerOutput[0].Codes[0] != "safe_code" {
		t.Fatalf("framework mutated sanitizer-owned output: %#v", sanitizerOutput)
	}
}

func TestRunDetailedStampsValidatorNextRepairWithFrameworkIteration(t *testing.T) {
	for _, validatorIteration := range []int{0, -1, 999} {
		t.Run(fmt.Sprintf("validator iteration %d", validatorIteration), func(t *testing.T) {
			caller := &fakeCaller{responses: []llmadapter.Response{
				{FinalResponse: `{"status":"draft"}`},
				{FinalResponse: `{"status":"ok"}`},
			}}
			var rendered []Repair

			result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
				Caller: caller,
				Render: func(_ context.Context, _ stepInput, repair []Repair) (string, error) {
					if len(repair) > 0 {
						rendered = copyRepair(repair)
					}
					return "prompt", nil
				},
				Validate: func(_ context.Context, _ stepInput, output stepOutput) (Judgment, error) {
					if output.Status == "ok" {
						return Judgment{Accepted: true}, nil
					}
					return Judgment{Findings: []Finding{{Codes: []string{"safe_retry"}}}}, nil
				},
				MaxIter: 2,
			}, stepInput{})
			if err != nil {
				t.Fatal(err)
			}

			if got := result.Attempts[0].Judgment.Findings[0].Codes[0]; got != "safe_retry" {
				t.Fatalf("Judgment findings = %#v, want original validator codes", result.Attempts[0].Judgment.Findings)
			}
			if len(rendered) != 1 || rendered[0].Iteration != 1 {
				t.Fatalf("rendered retry feedback = %#v, want framework iteration 1", rendered)
			}
			if retry := result.Attempts[0].NextRepair; len(retry) != 1 || retry[0].Iteration != 1 {
				t.Fatalf("NextRepair = %#v, want framework iteration 1", retry)
			}
		})
	}
}

func TestRunDetailedStampsCustomSanitizerNextRepairWithFrameworkIteration(t *testing.T) {
	for _, sanitizerIteration := range []int{0, -1, 999} {
		t.Run(fmt.Sprintf("sanitizer iteration %d", sanitizerIteration), func(t *testing.T) {
			caller := &fakeCaller{responses: []llmadapter.Response{
				{FinalResponse: `{"status":"draft"}`},
				{FinalResponse: `{"status":"ok"}`},
			}}
			var rendered []Repair

			result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
				Caller: caller,
				Render: func(_ context.Context, _ stepInput, repair []Repair) (string, error) {
					if len(repair) > 0 {
						rendered = copyRepair(repair)
					}
					return "prompt", nil
				},
				Validate: func(_ context.Context, _ stepInput, output stepOutput) (Judgment, error) {
					if output.Status == "ok" {
						return Judgment{Accepted: true}, nil
					}
					return Judgment{Findings: []Finding{{Codes: []string{"validator_detail"}}}}, nil
				},
				Sanitizer: func([]Finding) ([]Repair, error) {
					return []Repair{{Iteration: sanitizerIteration, Codes: []string{"safe_retry"}}}, nil
				},
				MaxIter: 2,
			}, stepInput{})
			if err != nil {
				t.Fatal(err)
			}

			if got := result.Attempts[0].Judgment.Findings[0].Codes[0]; got != "validator_detail" {
				t.Fatalf("Judgment findings = %#v, want original validator codes", result.Attempts[0].Judgment.Findings)
			}
			if len(rendered) != 1 || rendered[0].Iteration != 1 {
				t.Fatalf("rendered retry feedback = %#v, want framework iteration 1", rendered)
			}
			if retry := result.Attempts[0].NextRepair; len(retry) != 1 || retry[0].Iteration != 1 {
				t.Fatalf("NextRepair = %#v, want framework iteration 1", retry)
			}
		})
	}
}

func TestRunDetailedPreservesValidatorEmptySliceShape(t *testing.T) {
	t.Run("outer feedback", func(t *testing.T) {
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"ok"}`}}},
			Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
			Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
				return Judgment{Accepted: true, Findings: make([]Finding, 0)}, nil
			},
			MaxIter: 1,
		}, stepInput{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Attempts[0].Judgment.Findings == nil || len(result.Attempts[0].Judgment.Findings) != 0 {
			t.Fatalf("Validation feedback = %#v, want non-nil empty slice", result.Attempts[0].Judgment.Findings)
		}
	})

	t.Run("nested feedback", func(t *testing.T) {
		result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
			Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
			Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
			Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
				return Judgment{Findings: []Finding{{Codes: make([]string, 0), Locations: make([]string, 0)}}}, nil
			},
			Sanitizer: func([]Finding) ([]Repair, error) { return nil, nil },
			MaxIter:   1,
		}, stepInput{})
		if !errors.Is(err, ErrUnsettled) {
			t.Fatalf("error = %v, want ErrUnsettled", err)
		}
		findings := result.Attempts[0].Judgment.Findings
		if len(findings) != 1 || findings[0].Codes == nil || findings[0].Locations == nil || len(findings[0].Codes) != 0 || len(findings[0].Locations) != 0 {
			t.Fatalf("Judgment findings = %#v, want non-nil empty nested slices", findings)
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
		Render: func(_ context.Context, _ stepInput, repair []Repair) (string, error) {
			if len(repair) > 0 {
				prompt := "retry " + repair[0].Codes[0]
				prompts = append(prompts, prompt)
				return prompt, nil
			}
			prompts = append(prompts, "initial")
			return "initial", nil
		},
		Validate: func(_ context.Context, _ stepInput, output stepOutput) (Judgment, error) {
			if output.Status == "ok" {
				return Judgment{Accepted: true}, nil
			}
			return Judgment{Findings: []Finding{{Codes: []string{"not_ok"}}}}, nil
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
	if got.Attempts[0].Repair != nil {
		t.Fatalf("first attempt feedback = %#v, want nil", got.Attempts[0].Repair)
	}
	if len(got.Attempts[1].Repair) != 1 || got.Attempts[1].Repair[0].Codes[0] != "not_ok" {
		t.Fatalf("second attempt feedback = %#v, want sanitized retry feedback", got.Attempts[1].Repair)
	}
	if len(got.Attempts[0].Judgment.Findings) != 1 || got.Attempts[0].Judgment.Findings[0].Codes[0] != "not_ok" {
		t.Fatalf("attempt judgment findings = %#v, want original validator findings", got.Attempts[0].Judgment.Findings)
	}
	if len(got.Attempts[0].NextRepair) != 1 || got.Attempts[0].NextRepair[0].Iteration != 1 || got.Attempts[0].NextRepair[0].Codes[0] != "not_ok" {
		t.Fatalf("attempt retry feedback = %#v, want sanitized and stamped history", got.Attempts[0].NextRepair)
	}
}

func TestRunDetailedPublishesIsolatedFeedbackSlices(t *testing.T) {
	caller := &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}}
	source := []Finding{{Summary: "not ready", Codes: []string{"not_ready"}, Locations: []string{"status"}}}
	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: caller,
		Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
			return Judgment{Findings: source}, nil
		},
		MaxIter: 1,
	}, stepInput{})
	if !errors.Is(err, ErrUnsettled) {
		t.Fatalf("error = %v, want ErrUnsettled", err)
	}

	source[0].Summary = "mutated"
	source[0].Codes[0] = "mutated"
	source[0].Locations[0] = "mutated"
	got := result.Attempts[0].Judgment.Findings[0]
	if got.Summary != "not ready" || got.Codes[0] != "not_ready" || got.Locations[0] != "status" {
		t.Fatalf("published feedback changed with validator source: %#v", got)
	}
}

func TestRunDetailedRecordsRenderFailure(t *testing.T) {
	renderErr := errors.New("render")
	result, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller:   &fakeCaller{},
		Render:   func(context.Context, stepInput, []Repair) (string, error) { return "", renderErr },
		Validate: acceptValidate,
		MaxIter:  1,
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
		Render:   func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: acceptAny[chan int],
		MaxIter:  1,
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
		Render:   func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: acceptValidate,
		MaxIter:  1,
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
		Render:   func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: acceptValidate,
		MaxIter:  1,
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
		Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
			return Judgment{Findings: []Finding{{Codes: []string{"invalid"}}}}, validationErr
		},
		MaxIter: 1,
	}, stepInput{})
	assertStepFailure(t, validationResult, err, StageValidate, validationErr, true)

	sanitizeResult, err := RunDetailed(context.Background(), Step[stepInput, stepOutput]{
		Caller: &fakeCaller{responses: []llmadapter.Response{{FinalResponse: `{"status":"draft"}`}}},
		Render: func(context.Context, stepInput, []Repair) (string, error) { return "prompt", nil },
		Validate: func(context.Context, stepInput, stepOutput) (Judgment, error) {
			return Judgment{Findings: []Finding{{Summary: "https://unsafe.example"}}}, nil
		},
		MaxIter: 2,
	}, stepInput{})
	assertStepFailure(t, sanitizeResult, err, StageSanitize, ErrUnsafeRepair, true)
	if sanitizeResult.Attempts[0].Call.Response.FinalResponse != `{"status":"draft"}` {
		t.Fatalf("sanitize failure lost call evidence: %#v", sanitizeResult.Attempts[0].Call)
	}
	validation := sanitizeResult.Attempts[0].Judgment.Findings
	if len(validation) != 1 || validation[0].Summary != "https://unsafe.example" {
		t.Fatalf("sanitize failure lost validator decision: %#v", validation)
	}
	if sanitizeResult.Attempts[0].NextRepair != nil {
		t.Fatalf("sanitize failure published retry feedback: %#v", sanitizeResult.Attempts[0].NextRepair)
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
