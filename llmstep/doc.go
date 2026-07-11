// Package llmstep runs a single provider-neutral typed structured-output LLM
// step with bounded validation feedback retries.
//
// It combines prompt rendering, llmadapter.ValueDetailed typed calls,
// deterministic typed validation, sanitized validation feedback, stage-specific
// attempt evidence, and max-iteration handling.
// It is intentionally smaller than a workflow engine: applications still own
// business prompts, provider callers, semantic validators, write gates, and
// policy.
package llmstep
