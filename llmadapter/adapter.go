package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ronhuafeng/llmkit-go/llmschema"
)

var (
	ErrNilCaller = errors.New("llmadapter: caller is nil")
	ErrNilRender = errors.New("llmadapter: render is nil")
)

type Caller interface {
	Call(ctx context.Context, request Request) (Response, error)
}

type Request struct {
	Prompt       string
	OutputSchema json.RawMessage
}

type Response struct {
	FinalResponse string
}

func RequestFor[T any](prompt string) (Request, error) {
	schema, err := llmschema.SchemaJSONFor[T]()
	if err != nil {
		return Request{}, err
	}
	return Request{
		Prompt:       prompt,
		OutputSchema: schema,
	}, nil
}

func Value[T any](ctx context.Context, caller Caller, prompt string) (T, error) {
	var zero T
	if caller == nil {
		return zero, ErrNilCaller
	}
	request, err := RequestFor[T](prompt)
	if err != nil {
		return zero, err
	}
	response, err := caller.Call(ctx, request)
	if err != nil {
		return zero, err
	}
	return decodeFinalResponse[T](response.FinalResponse)
}

type Options[I any, O any] struct {
	Caller   Caller
	Render   func(ctx context.Context, input I) (string, error)
	Validate func(ctx context.Context, input I, output O) (bool, error)
}

type Op[I any, O any] struct {
	caller   Caller
	render   func(context.Context, I) (string, error)
	validate func(context.Context, I, O) (bool, error)
}

func NewOp[I any, O any](options Options[I, O]) Op[I, O] {
	return Op[I, O]{
		caller:   options.Caller,
		render:   options.Render,
		validate: options.Validate,
	}
}

func (op Op[I, O]) Run(ctx context.Context, input I) (O, error) {
	var zero O
	if op.caller == nil {
		return zero, ErrNilCaller
	}
	if op.render == nil {
		return zero, ErrNilRender
	}
	prompt, err := op.render(ctx, input)
	if err != nil {
		return zero, err
	}
	return Value[O](ctx, op.caller, prompt)
}

func (op Op[I, O]) Validate(ctx context.Context, input I, output O) (bool, error) {
	if op.validate == nil {
		return true, nil
	}
	return op.validate(ctx, input, output)
}

func decodeFinalResponse[T any](raw string) (T, error) {
	var zero T
	if strings.TrimSpace(raw) == "" {
		return zero, errors.New("llmadapter: final response is empty")
	}
	value, err := llmschema.DecodeString[T](raw)
	if err != nil {
		return zero, err
	}
	return value, nil
}
