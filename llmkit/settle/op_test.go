package settle

import (
	"context"
	"errors"
	"testing"
)

type recordingOp struct {
	results       []string
	settledAfter  int
	runErr        error
	validateErr   error
	runCalls      int
	validateCalls int
}

type cancelingOp struct {
	cancel        context.CancelFunc
	during        Stage
	validateCalls int
}

func (op *cancelingOp) Run(context.Context, string) (string, error) {
	if op.during == StageRun {
		op.cancel()
	}
	return "candidate", nil
}

func (op *cancelingOp) Validate(context.Context, string, string) (bool, error) {
	op.validateCalls++
	if op.during == StageValidate {
		op.cancel()
	}
	return true, nil
}

func (op *recordingOp) Run(ctx context.Context, input string) (string, error) {
	op.runCalls++
	if op.runErr != nil {
		return "", op.runErr
	}
	if len(op.results) == 0 {
		return "", nil
	}
	idx := op.runCalls - 1
	if idx >= len(op.results) {
		idx = len(op.results) - 1
	}
	return op.results[idx], nil
}

func (op *recordingOp) Validate(ctx context.Context, input string, result string) (bool, error) {
	op.validateCalls++
	if op.validateErr != nil {
		return false, op.validateErr
	}
	return op.validateCalls >= op.settledAfter, nil
}

func TestRunReturnsImmediatelyWhenFirstValidateSettles(t *testing.T) {
	op := &recordingOp{
		results:      []string{"first", "second"},
		settledAfter: 1,
	}

	got, err := Run(context.Background(), op, "input", 3)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got != "first" {
		t.Fatalf("Run result = %q, want %q", got, "first")
	}
	if op.runCalls != 1 {
		t.Fatalf("Run calls = %d, want 1", op.runCalls)
	}
	if op.validateCalls != 1 {
		t.Fatalf("Validate calls = %d, want 1", op.validateCalls)
	}
}

func TestRunCallsRunAgainWhenValidateReturnsFalse(t *testing.T) {
	op := &recordingOp{
		results:      []string{"first", "second"},
		settledAfter: 2,
	}

	got, err := Run(context.Background(), op, "input", 3)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got != "second" {
		t.Fatalf("Run result = %q, want %q", got, "second")
	}
	if op.runCalls != 2 {
		t.Fatalf("Run calls = %d, want 2", op.runCalls)
	}
	if op.validateCalls != 2 {
		t.Fatalf("Validate calls = %d, want 2", op.validateCalls)
	}
}

func TestRunReturnsLatestResultWhenLaterIterationSettles(t *testing.T) {
	op := &recordingOp{
		results:      []string{"draft-1", "draft-2", "final"},
		settledAfter: 3,
	}

	got, err := Run(context.Background(), op, "input", 5)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got != "final" {
		t.Fatalf("Run result = %q, want %q", got, "final")
	}
}

func TestRunReturnsErrUnsettledWhenMaxIterReached(t *testing.T) {
	op := &recordingOp{
		results:      []string{"draft-1", "draft-2"},
		settledAfter: 3,
	}

	_, err := Run(context.Background(), op, "input", 2)
	if !errors.Is(err, ErrUnsettled) {
		t.Fatalf("Run error = %v, want errors.Is ErrUnsettled", err)
	}
	if got := err.Error(); got != "settle: output remains unsettled: maxIter=2" {
		t.Fatalf("Run error = %q, want maxIter detail", got)
	}
	if op.runCalls != 2 {
		t.Fatalf("Run calls = %d, want 2", op.runCalls)
	}
	if op.validateCalls != 2 {
		t.Fatalf("Validate calls = %d, want 2", op.validateCalls)
	}
}

func TestRunReturnsErrInvalidMaxIter(t *testing.T) {
	op := &recordingOp{settledAfter: 1}

	_, err := Run(context.Background(), op, "input", 0)
	if !errors.Is(err, ErrInvalidMaxIter) {
		t.Fatalf("Run error = %v, want ErrInvalidMaxIter", err)
	}
}

func TestRunReturnsErrNilOp(t *testing.T) {
	var op Op[string, string]

	_, err := Run(context.Background(), op, "input", 1)
	if !errors.Is(err, ErrNilOp) {
		t.Fatalf("Run error = %v, want ErrNilOp", err)
	}
}

func TestRunReturnsErrNilOpForTypedNil(t *testing.T) {
	var op *recordingOp

	_, err := Run(context.Background(), op, "input", 1)
	if !errors.Is(err, ErrNilOp) {
		t.Fatalf("Run error = %v, want ErrNilOp", err)
	}
}

func TestRunReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	op := &recordingOp{settledAfter: 1}

	_, err := Run(ctx, op, "input", 1)
	if err != context.Canceled {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if op.runCalls != 0 {
		t.Fatalf("Run calls = %d, want 0", op.runCalls)
	}
}

func TestRunDetailedRecordsCancellationAfterSuccessfulRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	op := &cancelingOp{cancel: cancel, during: StageRun}

	result, err := RunDetailed(ctx, op, "input", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDetailed error = %v, want context.Canceled", err)
	}
	if !result.HasOutput || result.Output != "candidate" {
		t.Fatalf("result = %#v, want completed candidate", result)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("attempts = %#v, want one run-stage cancellation", result.Attempts)
	}
	attempt := result.Attempts[0]
	if !attempt.HasOutput || attempt.Output != "candidate" || attempt.Stage != StageRun || !errors.Is(attempt.Err, context.Canceled) {
		t.Fatalf("attempt = %#v, want candidate plus run-stage cancellation", attempt)
	}
	if op.validateCalls != 0 {
		t.Fatalf("validate calls = %d, want 0 after observed cancellation", op.validateCalls)
	}
}

func TestRunDetailedRecordsCancellationAfterSuccessfulValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	op := &cancelingOp{cancel: cancel, during: StageValidate}

	result, err := RunDetailed(ctx, op, "input", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDetailed error = %v, want context.Canceled", err)
	}
	if !result.HasOutput || result.Output != "candidate" {
		t.Fatalf("result = %#v, want completed candidate", result)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("attempts = %#v, want one validation-stage cancellation", result.Attempts)
	}
	attempt := result.Attempts[0]
	if !attempt.HasOutput || attempt.Output != "candidate" || !attempt.Settled || attempt.Stage != StageValidate || !errors.Is(attempt.Err, context.Canceled) {
		t.Fatalf("attempt = %#v, want settled evidence plus validation-stage cancellation", attempt)
	}
}

func TestRunReturnsRunError(t *testing.T) {
	runErr := errors.New("run failed")
	op := &recordingOp{
		settledAfter: 1,
		runErr:       runErr,
	}

	_, err := Run(context.Background(), op, "input", 1)
	if err != runErr {
		t.Fatalf("Run error = %v, want runErr", err)
	}
	if op.validateCalls != 0 {
		t.Fatalf("Validate calls = %d, want 0", op.validateCalls)
	}
}

func TestRunReturnsValidateError(t *testing.T) {
	validateErr := errors.New("validate failed")
	op := &recordingOp{
		results:     []string{"draft"},
		validateErr: validateErr,
	}

	_, err := Run(context.Background(), op, "input", 1)
	if err != validateErr {
		t.Fatalf("Run error = %v, want validateErr", err)
	}
	if op.runCalls != 1 {
		t.Fatalf("Run calls = %d, want 1", op.runCalls)
	}
}

func TestRunDetailedPreservesAttemptHistoryAndLatestOutput(t *testing.T) {
	op := &recordingOp{results: []string{"draft-1", "draft-2"}, settledAfter: 3}
	result, err := RunDetailed(context.Background(), op, "input", 2)
	if !errors.Is(err, ErrUnsettled) {
		t.Fatalf("error = %v, want ErrUnsettled", err)
	}
	if !result.HasOutput || result.Output != "draft-2" || len(result.Attempts) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Attempts[0].Output != "draft-1" || result.Attempts[0].Settled || result.Attempts[0].Stage != "" {
		t.Fatalf("first attempt = %#v", result.Attempts[0])
	}
}

func TestRunDetailedDistinguishesRunAndValidationFailure(t *testing.T) {
	runErr := errors.New("run")
	runResult, err := RunDetailed(context.Background(), &recordingOp{runErr: runErr}, "input", 1)
	if !errors.Is(err, runErr) || len(runResult.Attempts) != 1 || runResult.Attempts[0].Stage != StageRun || runResult.Attempts[0].HasOutput {
		t.Fatalf("run failure result = %#v, err = %v", runResult, err)
	}

	validateErr := errors.New("validate")
	validationResult, err := RunDetailed(context.Background(), &recordingOp{results: []string{"candidate"}, validateErr: validateErr}, "input", 1)
	if !errors.Is(err, validateErr) || !validationResult.HasOutput || validationResult.Output != "candidate" {
		t.Fatalf("validation failure result = %#v, err = %v", validationResult, err)
	}
	attempt := validationResult.Attempts[0]
	if attempt.Stage != StageValidate || !attempt.HasOutput || attempt.Output != "candidate" {
		t.Fatalf("validation attempt = %#v", attempt)
	}
}

func TestRunProjectsDetailedOutputOnError(t *testing.T) {
	direct := &recordingOp{results: []string{"candidate"}, validateErr: errors.New("validate")}
	got, err := Run(context.Background(), direct, "input", 1)
	if err == nil || got != "candidate" {
		t.Fatalf("Run output = %q, err = %v; want candidate plus error", got, err)
	}
}

type mapOutputOp struct{}

func (mapOutputOp) Run(context.Context, struct{}) (map[string]string, error) {
	return map[string]string{"status": "draft"}, nil
}

func (mapOutputOp) Validate(context.Context, struct{}, map[string]string) (bool, error) {
	return true, nil
}

func TestRunDetailedGenericOutputsUseOrdinaryGoSemantics(t *testing.T) {
	result, err := RunDetailed(context.Background(), mapOutputOp{}, struct{}{}, 1)
	if err != nil {
		t.Fatal(err)
	}

	result.Output["status"] = "final"
	if result.Attempts[0].Output["status"] != "final" {
		t.Fatal("generic attempt output unexpectedly behaved as a deep clone")
	}
}
