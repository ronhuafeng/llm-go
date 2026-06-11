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

func Run[I any, O any](ctx context.Context, op Op[I, O], input I, maxIter int) (O, error) {
	var zero O

	if maxIter < 1 {
		return zero, ErrInvalidMaxIter
	}
	if isNilOp(op) {
		return zero, ErrNilOp
	}

	for iter := 1; iter <= maxIter; iter++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		result, err := op.Run(ctx, input)
		if err != nil {
			return zero, err
		}

		settled, err := op.Validate(ctx, input, result)
		if err != nil {
			return zero, err
		}
		if settled {
			return result, nil
		}
	}

	return zero, fmt.Errorf("%w: maxIter=%d", ErrUnsettled, maxIter)
}

type Runner[I any, O any] struct {
	op Op[I, O]
}

func Bind[I any, O any](op Op[I, O]) Runner[I, O] {
	return Runner[I, O]{op: op}
}

func (r Runner[I, O]) Run(ctx context.Context, input I, maxIter int) (O, error) {
	return Run(ctx, r.op, input, maxIter)
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
