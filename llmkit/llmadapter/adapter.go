package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
)

var (
	ErrNilCaller                = errors.New("llmadapter: caller is nil")
	ErrEmptyResponse            = errors.New("llmadapter: final response is empty")
	ErrProviderIdentityMismatch = errors.New("llmadapter: provider identity mismatch")
)

type ProviderDetails interface {
	ProviderName() string
}

// TokenUsage is observed token accounting for one adapter attempt. It is
// not a billing estimate, remaining-budget figure, or reconstructed
// heuristic.
//
// Input, CachedInput, Output, and ReasoningOutput are presence-aware.
// Unknown means the provider did not report that count; Observed(0) is an
// explicit zero. The int64 fields do not establish presence; they remain
// only so unpublished adapters keep compiling until they migrate.
type TokenUsage struct {
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	Input                 Observation[int64]
	CachedInput           Observation[int64]
	Output                Observation[int64]
	ReasoningOutput       Observation[int64]
}

// ExecutionEvidence is provider-neutral facts attributable to one model
// call. ProviderName is identity: empty means the caller published no
// provider. Model and Usage use Observation / nil so unknown stays
// unknown; requested settings do not fill them.
type ExecutionEvidence struct {
	ProviderName string
	// EffectiveModel does not establish presence. It remains only so
	// unpublished adapters keep compiling until they migrate.
	EffectiveModel string
	// Model is the provider model identifier that actually served the
	// request. Unknown means the provider did not report one. It is not
	// inferred from the prompt and is not a capability or pricing lookup
	// key.
	Model Observation[string]
	// Usage is observed token accounting for this attempt. Nil means the
	// provider did not report a usage object. Unknown dimensions inside a
	// present Usage object are unreported measurements, not observed zeros.
	Usage *TokenUsage
}

// Caller is a provider-neutral inference capability. It asks a model for a
// typed proposition and publishes evidence. It does not grant mutation
// authority. An implementation may satisfy Caller only when any
// model-directed execution reachable through that implementation is already
// effect-free or independently authorized outside the model request. Prompt
// is not authority. Decoded model output cannot authorize an external effect.
type Caller interface {
	Call(ctx context.Context, request Request) (Response, error)
}

type Request struct {
	// Prompt is copied as a Go string value. Prompt is not authority.
	Prompt string
	// OutputSchema is cloned before Caller.Call is invoked. A caller may mutate
	// its copy during the call but must not retain mutable toolkit-owned request
	// state and mutate it after returning. The schema constrains proposition
	// shape only; it does not grant an external effect.
	OutputSchema json.RawMessage
}

type Response struct {
	// FinalResponse is copied as a Go string value and is retained on call and
	// decode errors when available.
	FinalResponse string
	// Execution is provider-neutral evidence. ValueDetailed clones Usage before
	// publishing the response. Observation values copy by value; unknown stays
	// unknown.
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
	if err := ctx.Err(); err != nil {
		return result, valueError(ValueStageCall, err)
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
