// Package settle runs provider-neutral operations until their typed output
// satisfies a validator or a bounded attempt count is exhausted.
//
// RunDetailed publishes an owned snapshot of its attempt slice. Candidate and
// result values use ordinary Go value semantics, so reference fields are not
// generically deep-cloned. Callers that require deep immutability must choose
// immutable output types or explicitly clone their values.
//
// Deprecated: This legacy module is frozen at v0.5.0 and receives no further
// feature or security maintenance after cutover. Use
// github.com/ronhuafeng/llm-go/llmkit/settle, first released in
// llmkit/v0.6.0. Published legacy versions remain available through the public
// Go proxy; no forwarding or runtime compatibility layer is provided.
package settle
