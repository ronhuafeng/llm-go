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
	// Prompt is copied as a Go string value.
	Prompt string
	// OutputSchema is cloned before Caller.Call is invoked. A caller may mutate
	// its copy during the call but must not retain mutable toolkit-owned request
	// state and mutate it after returning.
	OutputSchema json.RawMessage
}

type Response struct {
	// FinalResponse is copied as a Go string value and is retained on call and
	// decode errors when available.
	FinalResponse string
	// Execution is provider-neutral evidence. ValueDetailed clones Usage before
	// publishing the response.
	Execution ExecutionEvidence
	// ProviderDetails is adapter-owned. Adapters must return an isolated typed
	// value that does not alias mutable runtime state. Typed nil is invalid, and
	// ProviderName must agree with Execution.ProviderName.
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
	// Value follows ordinary Go value semantics. ValueDetailed does not
	// generically deep-clone maps, slices, pointers, or other reference fields.
	Value T
	// Response preserves available call evidence on call and decode failures.
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
