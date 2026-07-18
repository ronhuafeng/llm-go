// Package llmstep runs a single provider-neutral typed structured-output LLM
// step with bounded validation feedback retries.
//
// It combines prompt rendering, llmadapter.ValueDetailed typed calls,
// deterministic typed validation, sanitized retry feedback, stage-specific
// attempt evidence, and max-iteration handling. RunDetailed publishes the
// validator decision exactly as returned in Attempt.Validation and separately
// publishes sanitizer-owned, iteration-stamped Attempt.RetryFeedback. Only
// RetryFeedback is eligible for the next Render call, and it is created only
// when that call will occur. A final unsettled attempt returns settle.ErrUnsettled
// without invoking the sanitizer or synthesizing RetryFeedback. Sanitization
// does not redact Validation; applications must redact or omit sensitive facts
// before their validator returns them, or deliberately substitute a caller-owned
// threat-model-reviewed keyed pseudonymous fingerprint.
//
// When Step.Sanitizer is nil, StrictFeedbackSanitizer is the default. It rejects
// every non-empty free-form Summary and accepts only identifier-oriented Codes
// and Locations. Applications that intentionally send free-form feedback must
// provide an explicit sanitizer and own that policy. Neither the default nor a
// custom sanitizer is a DLP system, secret scanner, or privacy guarantee;
// applications remain responsible for redacting validator evidence.
//
// RunDetailed publishes owned snapshots of attempt, validation, and feedback
// slices. Typed outputs inside those snapshots follow ordinary Go value
// semantics and are not generically deep-cloned.
//
// RunDetailed observes context cancellation before Render and after successful
// Render, provider Call, and Validate callbacks. A callback error remains the
// phase error even when the context is also canceled. When a callback succeeds
// but cancellation is observed at its boundary, RunDetailed returns a StepError
// wrapping ctx.Err at that phase and retains completed output and evidence in
// the detailed result. Cancellation after the final observation may race with
// a successful return.
//
// It is intentionally smaller than a workflow engine: applications still own
// business prompts, provider callers, semantic validators, write gates, and
// policy.
package llmstep
