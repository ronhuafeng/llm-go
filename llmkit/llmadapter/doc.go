// Package llmadapter defines provider-neutral typed LLM call contracts.
//
// Caller performs one inference call. Value and ValueWithContract validate and
// decode its response with llmschema. Optional response fields remain unset when
// the adapter did not obtain them, and backend-specific details stay typed and
// adapter-owned.
//
// This package does not own provider transport, business acceptance, retry
// policy, application authorization, or external side effects.
package llmadapter
