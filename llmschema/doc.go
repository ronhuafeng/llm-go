// Package llmschema projects Go expected-output types into provider-neutral
// structured-output schemas, validates provider JSON against those schemas,
// and decodes valid responses back into Go values.
// Schema validation errors publish toolkit-owned violation slices. Successfully
// decoded generic values use ordinary Go value semantics and may contain maps,
// slices, pointers, or other reference fields.
//
// It does not own provider transport, prompt rendering, retry loops, or
// business validation.
package llmschema
