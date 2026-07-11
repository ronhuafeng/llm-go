package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ronhuafeng/llmkit-go/llmschema"
)

var (
	ErrNilCaller                = errors.New("llmadapter: caller is nil")
	ErrNilRender                = errors.New("llmadapter: render is nil")
	ErrEmptyResponse            = errors.New("llmadapter: final response is empty")
	ErrProviderIdentityMismatch = errors.New("llmadapter: provider identity mismatch")
)

type ProviderDetails interface {
	ProviderName() string
}

type TokenUsage struct {
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
}

type ExecutionEvidence struct {
	ProviderName   string
	EffectiveModel string
	Usage          *TokenUsage
}

type Caller interface {
	Call(ctx context.Context, request Request) (Response, error)
}

type Request struct {
	Prompt       string
	OutputSchema json.RawMessage
}

type Response struct {
	FinalResponse   string
	Execution       ExecutionEvidence
	ProviderDetails ProviderDetails
}

type ValueStage string

const (
	ValueStageRequest ValueStage = "request"
	ValueStageCall    ValueStage = "call"
	ValueStageDecode  ValueStage = "decode"
)

type ValueError struct {
	Stage ValueStage
	Err   error
}

func (e *ValueError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("llmadapter: %s stage: %v", e.Stage, e.Err)
}

func (e *ValueError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type ValueResult[T any] struct {
	Value    T
	Response Response
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
	result, err := ValueDetailed[T](ctx, caller, prompt)
	return result.Value, err
}

func ValueDetailed[T any](ctx context.Context, caller Caller, prompt string) (ValueResult[T], error) {
	var result ValueResult[T]
	if isNil(caller) {
		return result, valueError(ValueStageCall, ErrNilCaller)
	}
	request, err := RequestFor[T](prompt)
	if err != nil {
		return result, valueError(ValueStageRequest, err)
	}
	response, callErr := caller.Call(ctx, cloneRequest(request))
	result.Response = cloneResponse(response)
	identityErr := validateProviderIdentity(response)
	if callErr != nil || identityErr != nil {
		return result, valueError(ValueStageCall, errors.Join(callErr, identityErr))
	}
	result.Value, err = decodeFinalResponse[T](response.FinalResponse)
	if err != nil {
		return result, valueError(ValueStageDecode, err)
	}
	return result, nil
}

// Options configures the legacy settle-compatible adapter operation.
//
// Deprecated: use llmstep.Step or implement settle.Op directly.
type Options[I any, O any] struct {
	Caller   Caller
	Render   func(ctx context.Context, input I) (string, error)
	Validate func(ctx context.Context, input I, output O) (bool, error)
}

// Op is the legacy settle-compatible adapter operation.
//
// Deprecated: use llmstep.Step or implement settle.Op directly.
type Op[I any, O any] struct {
	caller   Caller
	render   func(context.Context, I) (string, error)
	validate func(context.Context, I, O) (bool, error)
}

// Deprecated: use llmstep.Run or settle.Run.
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
		return zero, ErrEmptyResponse
	}
	value, err := llmschema.Decode[T]([]byte(raw))
	if err != nil {
		return zero, err
	}
	return value, nil
}

func valueError(stage ValueStage, err error) error {
	return &ValueError{Stage: stage, Err: err}
}

func validateProviderIdentity(response Response) error {
	if response.ProviderDetails == nil {
		return nil
	}
	value := reflect.ValueOf(response.ProviderDetails)
	if isNilValue(value) {
		return fmt.Errorf("%w: provider details is typed nil", ErrProviderIdentityMismatch)
	}
	if response.Execution.ProviderName != response.ProviderDetails.ProviderName() {
		return fmt.Errorf("%w: execution=%q details=%q", ErrProviderIdentityMismatch, response.Execution.ProviderName, response.ProviderDetails.ProviderName())
	}
	return nil
}

func cloneRequest(request Request) Request {
	request.OutputSchema = append(json.RawMessage(nil), request.OutputSchema...)
	return request
}

func cloneResponse(response Response) Response {
	if response.Execution.Usage != nil {
		usage := *response.Execution.Usage
		response.Execution.Usage = &usage
	}
	return response
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	return isNilValue(reflect.ValueOf(value))
}

func isNilValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
