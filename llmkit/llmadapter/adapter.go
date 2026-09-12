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
	ErrNilCaller               = errors.New("llmadapter: caller is nil")
	ErrMissingResponse         = errors.New("llmadapter: final response is missing")
	ErrEmptyResponse           = errors.New("llmadapter: final response is empty")
	ErrBackendIdentityMismatch = errors.New("llmadapter: backend identity mismatch")
)

// BackendDetails is typed backend-specific evidence published by an adapter.
// BackendName identifies the execution runtime/adapter that owns those details;
// it is not model-provider identity. When details are present, that name must
// be non-empty and equal to ExecutionEvidence.BackendName.
type BackendDetails interface {
	BackendName() string
}

// TokenUsage is observed token accounting for one provider-neutral
// inference/adapter attempt. It is not accounting for one lower-level
// provider RPC, a billing estimate, remaining-budget figure, or reconstructed
// heuristic.
//
// Each dimension is presence-aware. Unknown means the provider did not
// report that count; Observed(0) is an explicit zero.
type TokenUsage struct {
	Input           Observation[int64]
	CachedInput     Observation[int64]
	Output          Observation[int64]
	ReasoningOutput Observation[int64]
}

// ExecutionEvidence is provider-neutral facts attributable to one
// inference/adapter attempt. It is not a claim about one lower-level model or
// provider call. BackendName identifies the execution runtime/adapter when
// known. ProviderName is the actual model-provider identity only when directly
// observed. Backend, provider, and model identities are independent facts and
// are never inferred from one another.
type ExecutionEvidence struct {
	BackendName string
	// ProviderName is the observed model-provider identity. Unknown means the
	// lower layer did not report a provider fact. Adapter names, model names,
	// credentials, endpoints, and requested settings must not populate it.
	ProviderName Observation[string]
	// Model is the provider model identifier that actually served this
	// inference/adapter attempt. Unknown means the lower layer did not prove a
	// unique served model at that scope. Requested, default, thread-start, and
	// response-scoped reroute identifiers stay exact backend details unless
	// they prove attempt-wide serving. It is not inferred from the prompt and
	// is not a capability or pricing lookup key.
	Model Observation[string]
	// Usage is observed token accounting for this inference/adapter attempt.
	// Nil means the lower layer did not report a usage object at that scope.
	// Unknown dimensions inside a present Usage object are unreported
	// measurements, not observed zeros. Thread-total, last-request, and
	// per-upstream-response counts are different scopes and must not be
	// silently interchanged.
	Usage *TokenUsage
}

// ObserveCounts records the four total token dimensions as present, including
// an observed zero. Requested, default, or estimated counts must not be passed
// here.
func (u *TokenUsage) ObserveCounts(input, cachedInput, output, reasoningOutput int64) {
	if u == nil {
		return
	}
	u.Input = Observed(input)
	u.CachedInput = Observed(cachedInput)
	u.Output = Observed(output)
	u.ReasoningOutput = Observed(reasoningOutput)
}

// ObserveProviderName records actual model-provider identity as present,
// including an observed empty string. Adapter/backend names, model identifiers,
// requested providers, endpoints, credentials, and heuristics must not be
// passed here.
func (e *ExecutionEvidence) ObserveProviderName(provider string) {
	if e == nil {
		return
	}
	e.ProviderName = Observed(provider)
}

// ObserveModel records the served model identifier as present, including an
// observed empty string. Requested, default, thread-start, heuristic, inferred,
// or response-scoped names must not be passed here unless they prove serving
// for this attempt.
func (e *ExecutionEvidence) ObserveModel(model string) {
	if e == nil {
		return
	}
	e.Model = Observed(model)
}

// Caller is a provider-neutral inference capability. It asks a model for a
// typed proposition and publishes evidence. It does not grant mutation
// authority. An implementation may satisfy Caller only when any model-directed
// execution reachable through that implementation is already effect-free or
// independently authorized outside the model request. Prompt is not authority.
// Decoded model output cannot authorize an external effect.
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
	// FinalResponse is the observed final-response text for this attempt.
	// Unknown means no final response was observed. Observed("") is a present
	// empty value and is not absence. Call and decode errors retain this
	// presence-aware evidence.
	FinalResponse Observation[string]
	// Execution is provider-neutral evidence. Value clones Usage before
	// publishing the response. Observation values copy by value; unknown stays
	// unknown.
	Execution ExecutionEvidence
	// BackendDetails is adapter-owned. Adapters must return an isolated typed
	// value that does not alias mutable runtime state. Typed nil is invalid, and
	// BackendName must agree with Execution.BackendName. Backend details do not
	// establish model-provider identity.
	BackendDetails BackendDetails
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
	// Value follows ordinary Go value semantics. The Value call does not
	// generically deep-clone maps, slices, pointers, or other reference fields.
	Value T
	// Response preserves available call evidence on call and decode failures.
	Response Response
}

// Value is the default typed-inference path. It compiles one Contract and
// returns the same evidence-bearing ValueResult as ValueWithContract.
func Value[T any](ctx context.Context, caller Caller, prompt string) (ValueResult[T], error) {
	contract, err := llmschema.Compile[T]()
	if err != nil {
		return ValueResult[T]{}, valueError(ValueStageRequest, err)
	}
	return ValueWithContract(ctx, caller, prompt, contract)
}

// ValueWithContract is the explicit reuse path: one compiled contract supplies
// the request schema and the response decode. The zero contract fails closed
// before Caller.Call.
func ValueWithContract[T any](ctx context.Context, caller Caller, prompt string, contract llmschema.Contract[T]) (ValueResult[T], error) {
	var result ValueResult[T]
	if isNil(caller) {
		return result, valueError(ValueStageCall, ErrNilCaller)
	}
	if !contract.Compiled() {
		return result, valueError(ValueStageRequest, llmschema.ErrUncompiledContract)
	}
	request := Request{
		Prompt:       prompt,
		OutputSchema: contract.SchemaJSON(),
	}
	response, callErr := caller.Call(ctx, cloneRequest(request))
	result.Response = cloneResponse(response)
	identityErr := validateBackendIdentity(response)
	if callErr != nil || identityErr != nil {
		return result, valueError(ValueStageCall, errors.Join(callErr, identityErr))
	}
	if err := ctx.Err(); err != nil {
		return result, valueError(ValueStageCall, err)
	}
	value, err := decodeFinalResponse(response.FinalResponse, contract)
	if err != nil {
		return result, valueError(ValueStageDecode, err)
	}
	result.Value = value
	return result, nil
}

func decodeFinalResponse[T any](final Observation[string], contract llmschema.Contract[T]) (T, error) {
	var zero T
	raw, ok := final.Value()
	if !ok {
		return zero, ErrMissingResponse
	}
	if strings.TrimSpace(raw) == "" {
		return zero, ErrEmptyResponse
	}
	return contract.Decode([]byte(raw))
}

func valueError(stage ValueStage, err error) error {
	return &ValueError{Stage: stage, Err: err}
}

func validateBackendIdentity(response Response) error {
	if response.BackendDetails == nil {
		return nil
	}
	value := reflect.ValueOf(response.BackendDetails)
	if isNilValue(value) {
		return fmt.Errorf("%w: backend details is typed nil", ErrBackendIdentityMismatch)
	}
	executionName := strings.TrimSpace(response.Execution.BackendName)
	detailsName := strings.TrimSpace(response.BackendDetails.BackendName())
	if executionName == "" || detailsName == "" {
		return fmt.Errorf("%w: empty backend identity execution=%q details=%q", ErrBackendIdentityMismatch, response.Execution.BackendName, response.BackendDetails.BackendName())
	}
	if executionName != detailsName {
		return fmt.Errorf("%w: execution=%q details=%q", ErrBackendIdentityMismatch, response.Execution.BackendName, response.BackendDetails.BackendName())
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
