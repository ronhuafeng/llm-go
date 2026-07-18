// Package settle runs provider-neutral operations until their typed output
// satisfies a validator or a bounded attempt count is exhausted.
//
// RunDetailed publishes an owned snapshot of its attempt slice. Candidate and
// result values use ordinary Go value semantics, so reference fields are not
// generically deep-cloned. Callers that require deep immutability must choose
// immutable output types or explicitly clone their values.
//
// RunDetailed observes context cancellation before each attempt and after each
// successful Op.Run and Op.Validate callback. A callback error remains the
// phase error even when the context is also canceled. When a callback succeeds
// but cancellation is observed at its boundary, RunDetailed returns ctx.Err,
// records it at StageRun or StageValidate, and retains any completed candidate
// and validation decision in the detailed result. Cancellation after the final
// observation may race with a successful return.
package settle
