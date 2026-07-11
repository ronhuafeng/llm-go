// Package llmadapter adapts a prompt plus an expected Go output type into a
// provider-neutral typed LLM request or value call.
//
// It owns the small provider-neutral Caller contract and the type/schema/decode
// plumbing around prompt plus typed output. ValueDetailed preserves the
// provider-neutral response and typed provider details on call and decode
// failures. Concrete provider callers own transport and provider-specific
// schema policy; business code owns semantic acceptance.
//
// Trace-rich business provider adapters may use RequestFor and still call a
// provider SDK directly when they must preserve provider-specific diagnostics,
// lineage, artifacts, or failure mapping.
package llmadapter
