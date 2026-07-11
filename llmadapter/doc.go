// Package llmadapter adapts a prompt plus an expected Go output type into a
// provider-neutral typed LLM request or value call.
//
// It owns the small provider-neutral Caller contract and the type/schema/decode
// plumbing around prompt plus typed output. ValueDetailed preserves the
// provider-neutral response and typed provider details on call and decode
// failures. Concrete provider callers own transport and provider-specific
// schema policy; business code owns semantic acceptance.
//
// Detailed APIs publish owned, isolated snapshots of toolkit-owned state.
// ValueDetailed clones request schema bytes before caller invocation and clones
// neutral token usage before publishing a response. Provider adapters must
// publish ProviderDetails as isolated typed values that do not alias mutable
// runtime state. Generic typed outputs follow ordinary Go value semantics; the
// package does not promise a universal deep copy of arbitrary T.
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
