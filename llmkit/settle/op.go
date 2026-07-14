package settle

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrInvalidMaxIter = errors.New("settle: maxIter must be at least 1")
	ErrNilOp          = errors.New("settle: op is nil")
	ErrUnsettled      = errors.New("settle: output remains unsettled")
)

type Op[I any, O any] interface {
	Run(ctx context.Context, input I) (O, error)
	Validate(ctx context.Context, input I, result O) (bool, error)
}

type Stage string

const (
	StageRun      Stage = "run"
	StageValidate Stage = "validate"
)

type Attempt[O any] struct {
	Iteration int
	// Output follows ordinary Go value semantics and is not generically cloned.
	Output    O
	HasOutput bool
	Settled   bool
	Stage     Stage
	Err       error
}

type Result[O any] struct {
	// Output follows ordinary Go value semantics and may share reference fields
	// with the output recorded in Attempts.
	Output    O
	HasOutput bool
	// Attempts is an owned slice snapshot. Generic Output values inside attempts
	// are not deep-cloned.
	Attempts []Attempt[O]
}

func Run[I any, O any](ctx context.Context, op Op[I, O], input I, maxIter int) (O, error) {
	result, err := RunDetailed(ctx, op, input, maxIter)
	return result.Output, err
}

func RunDetailed[I any, O any](ctx context.Context, op Op[I, O], input I, maxIter int) (Result[O], error) {
	var result Result[O]

	if maxIter < 1 {
		return result, ErrInvalidMaxIter
	}
	if isNilOp(op) {
		return result, ErrNilOp
	}

	for iter := 1; iter <= maxIter; iter++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		output, err := op.Run(ctx, input)
		if err != nil {
			result.Attempts = append(result.Attempts, Attempt[O]{Iteration: iter, Stage: StageRun, Err: err})
			return snapshot(result), err
		}
		attempt := Attempt[O]{Iteration: iter, Output: output, HasOutput: true}
		result.Output = output
		result.HasOutput = true

		settled, err := op.Validate(ctx, input, output)
		if err != nil {
			attempt.Stage = StageValidate
			attempt.Err = err
			result.Attempts = append(result.Attempts, attempt)
			return snapshot(result), err
		}
		attempt.Settled = settled
		result.Attempts = append(result.Attempts, attempt)
		if settled {
			return snapshot(result), nil
		}
	}

	return snapshot(result), fmt.Errorf("%w: maxIter=%d", ErrUnsettled, maxIter)
}

func snapshot[O any](result Result[O]) Result[O] {
	result.Attempts = append([]Attempt[O](nil), result.Attempts...)
	return result
}

func isNilOp[I any, O any](op Op[I, O]) bool {
	if op == nil {
		return true
	}

	value := reflect.ValueOf(op)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
