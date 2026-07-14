// Package llmschema projects Go expected-output types into provider-neutral
// structured-output schemas, validates provider JSON against those schemas,
// and decodes valid responses back into Go values.
// Schema validation errors publish toolkit-owned violation slices. Successfully
// decoded generic values use ordinary Go value semantics and may contain maps,
// slices, pointers, or other reference fields.
//
// It does not own provider transport, prompt rendering, retry loops, or
// business validation.
//
// Deprecated: This legacy module is frozen at v0.5.0 and receives no further
// feature or security maintenance after cutover. Use
// github.com/ronhuafeng/llm-go/llmkit/llmschema, first released in
// llmkit/v0.6.0. Published legacy versions remain available through the public
// Go proxy; no forwarding or runtime compatibility layer is provided.
package llmschema
