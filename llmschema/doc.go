// Package llmschema projects Go expected-output types into provider-neutral
// structured-output schemas, validates provider JSON against those schemas,
// and decodes valid responses back into Go values.
//
// It does not own provider transport, prompt rendering, retry loops, or
// business validation.
package llmschema
