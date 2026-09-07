// Package llmadapter adapts a prompt plus an expected Go output type into a
// provider-neutral typed LLM request or value call.
//
// It owns the small provider-neutral Caller contract and the type/schema/decode
// plumbing around prompt plus typed output. Caller is an inference capability:
// Request carries only prompt text and an output schema, and neither field
// grants application effect authority. An adapter may implement Caller only
// when any model-directed execution reachable through that implementation is
// already effect-free or independently authorized outside the model request.
// Prompt wording and decoded model output cannot authorize an external effect.
// Application authorization stays outside llmkit.
//
// Value preserves the provider-neutral response and typed provider
// details on call and decode failures. Concrete provider callers own transport
// and provider-specific schema policy; business code owns semantic acceptance.
//
// Neutral token counts and the effective model use Observation so an
// unreported fact stays unknown and an observed zero or empty value stays
// present. Requested settings, defaults, estimates, heuristics, and inferred
// names cannot populate an observation.
//
// The typed-inference API publishes owned, isolated snapshots of toolkit-owned
// state.
// Value is the default path: it compiles one Contract for the request
// schema and the response decode. ValueWithContract is the explicit reuse
// path for a caller-owned compiled Contract. Both return the same
// evidence-bearing ValueResult. Bounded llmstep retries use
// ValueWithContract. Neither path owns semantic judgment, provider dialect,
// or effect authority.
// Value clones request schema bytes before caller invocation and clones
// neutral token usage before publishing a response. Provider adapters must
// publish ProviderDetails as isolated typed values that do not alias mutable
// runtime state. Generic typed outputs follow ordinary Go value semantics; the
// package does not promise a universal deep copy of arbitrary T.
//
// After a successful Caller.Call and provider-identity check, Value
// observes context cancellation before decoding the typed output. If canceled,
// it returns a call-stage error while preserving the cloned response evidence.
// Caller and identity errors take precedence over cancellation observed at that
// boundary. Cancellation after the final observation may race with decoding
// and a successful return.
//
// A safe adapter constructs details from copied provider state:
//
//	type details struct {
//		Headers map[string]string
//	}
//	func (details) ProviderName() string { return "example" }
//	isolated := details{Headers: maps.Clone(runtimeHeaders)}
//
// Returning details that directly retain runtimeHeaders is unsafe because a
// later transport mutation would change already-published evidence. The clone
// rule belongs to the adapter because llmadapter cannot know provider-specific
// value semantics.
//
// Trace-rich business provider adapters may use RequestFor and still call a
// provider SDK directly when they must preserve provider-specific diagnostics,
// lineage, artifacts, or failure mapping.
package llmadapter
