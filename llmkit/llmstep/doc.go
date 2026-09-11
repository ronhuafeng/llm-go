// Package llmstep runs a single provider-neutral typed structured-output LLM
// step with bounded judgment and repair retries.
//
// It combines prompt rendering, typed llmadapter inference, deterministic
// judgment, application-projected repair, stage-specific attempt evidence, and
// max-iteration handling. Run compiles one llmschema.Contract for the output
// type and reuses it across attempts through ValueWithContract.
//
// Run publishes the validator decision exactly as returned in
// Attempt.Judgment and separately publishes application-projected,
// iteration-stamped Attempt.NextRepair. Only NextRepair is eligible for the
// next Render call, and it is created only when that call will occur. A final
// rejected attempt returns ErrExhausted without invoking the projection or
// synthesizing NextRepair. A step with no deterministic judge returns
// ErrNilValidate before Render or a provider call.
//
// Raw validator findings are never automatically model-facing. When a rejected
// attempt can retry, the application must provide Step.Sanitizer as the
// explicit projection from findings to repair input. If it is nil, Run fails
// closed with ErrMissingRepairProjection. The toolkit does not define secrets,
// sensitive strings, safe URLs, credential patterns, redaction, pseudonymization,
// or other disclosure/content policy, and it does not reinterpret projection
// output. Applications own those decisions.
//
// Run publishes owned snapshots of attempt, judgment, and repair slices. Typed
// outputs inside those snapshots follow ordinary Go value semantics and are not
// generically deep-cloned.
//
// Run observes context cancellation before Render and after successful Render,
// provider Call, and Validate callbacks. A callback error remains the phase
// error even when the context is also canceled. When a callback succeeds but
// cancellation is observed at its boundary, Run returns a StepError wrapping
// ctx.Err at that phase and retains completed output and evidence in the
// result. Cancellation after the final observation may race with a successful
// return.
//
// It is intentionally smaller than a workflow engine: applications still own
// business prompts, provider callers, semantic judges, repair disclosure,
// write gates, and policy.
package llmstep
