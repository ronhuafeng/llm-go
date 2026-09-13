// Package llmschema compiles Go output types to provider-neutral JSON Schema,
// validates JSON responses, and decodes valid responses back into Go values.
// Contract is the reusable compiled form; SchemaJSONFor and Decode are one-shot
// helpers.
//
// It does not own provider transport, retries, or business validation.
package llmschema
