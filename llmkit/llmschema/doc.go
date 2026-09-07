// Package llmschema projects Go expected-output types into provider-neutral
// structured-output schemas, validates provider JSON against those schemas,
// and decodes valid responses back into Go values.
// Contract is the compiled form of one typed schema: SchemaJSON and Decode
// share that owned projection. The zero contract is uncompiled and does
// not guess a schema. SchemaJSONFor and Decode remain one-shot helpers.
// Schema validation errors publish toolkit-owned violation slices. Successfully
// decoded generic values use ordinary Go value semantics and may contain maps,
// slices, pointers, or other reference fields.
//
// It does not own provider transport, prompt rendering, retry loops, or
// business validation.
package llmschema
