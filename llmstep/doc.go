// Package llmstep runs a single provider-neutral typed structured-output LLM
// step with bounded validation feedback retries.
//
// It combines prompt rendering, llmadapter.ValueDetailed typed calls,
// deterministic typed validation, sanitized retry feedback, stage-specific
// attempt evidence, and max-iteration handling. RunDetailed publishes the
// validator decision exactly as returned in Attempt.Validation and separately
// publishes sanitizer-owned, iteration-stamped Attempt.RetryFeedback. Only
// RetryFeedback is eligible for the next Render call. Sanitization does not
// redact Validation; applications must redact or omit sensitive facts before
// their validator returns them, or deliberately substitute a caller-owned
// threat-model-reviewed keyed pseudonymous fingerprint.
//
// RunDetailed publishes owned snapshots of attempt, validation, and feedback
// slices. Typed outputs inside those snapshots follow ordinary Go value
// semantics and are not generically deep-cloned.
// It is intentionally smaller than a workflow engine: applications still own
// business prompts, provider callers, semantic validators, write gates, and
// policy.
package llmstep
