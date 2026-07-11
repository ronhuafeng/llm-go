// Package settle runs provider-neutral operations until their typed output
// satisfies a validator or a bounded attempt count is exhausted.
//
// RunDetailed publishes an owned snapshot of its attempt slice. Candidate and
// result values use ordinary Go value semantics, so reference fields are not
// generically deep-cloned. Callers that require deep immutability must choose
// immutable output types or explicitly clone their values.
package settle
