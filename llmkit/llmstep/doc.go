// Package llmstep runs bounded typed LLM calls with deterministic caller
// validation and optional retries.
//
// Result.Output is set only after validation accepts an attempt. Rejected
// attempts may be retained for diagnostics. Step.ProjectRepair lets the
// application choose what validation feedback is sent to a later attempt.
//
// The package does not own provider transport, application policy, or side
// effects.
package llmstep
