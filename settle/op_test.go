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

func TestBindRunMatchesRun(t *testing.T) {
	directOp := &recordingOp{
		results:      []string{"draft", "final"},
		settledAfter: 2,
	}
	boundOp := &recordingOp{
		results:      []string{"draft", "final"},
		settledAfter: 2,
	}

	directResult, directErr := Run(context.Background(), directOp, "input", 2)
	boundResult, boundErr := Bind[string, string](boundOp).Run(context.Background(), "input", 2)

	if directErr != nil {
		t.Fatalf("direct Run returned error: %v", directErr)
	}
	if boundErr != nil {
		t.Fatalf("bound Run returned error: %v", boundErr)
	}
	if boundResult != directResult {
		t.Fatalf("bound result = %q, want %q", boundResult, directResult)
	}
	if boundOp.runCalls != directOp.runCalls {
		t.Fatalf("bound Run calls = %d, want %d", boundOp.runCalls, directOp.runCalls)
	}
	if boundOp.validateCalls != directOp.validateCalls {
		t.Fatalf("bound Validate calls = %d, want %d", boundOp.validateCalls, directOp.validateCalls)
	}
}
