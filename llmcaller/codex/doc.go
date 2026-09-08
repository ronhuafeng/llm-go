// Package codexcaller adapts llmadapter structured calls to exact Codex thread
// lifecycle operations.
//
// It owns Codex schema policy, request/result projection, exact defaults, and
// named Codex safety profiles. A provider-neutral Caller can be constructed
// only with a named effect-safe profile. That Caller remains an inference
// capability: effect-safe is not disclosure-safe, and read-only is not
// confidential. An allowed read can still expose workspace or input data to
// model and provider processing. Prevention of sensitive disclosure is
// application-owned unless separately proven; CWD, workspace, and input
// selection remain part of the confidentiality boundary. Ephemeral is not a
// provider-retention guarantee. Named-profile request policy is
// enforced before every runner invocation. Effective approval, sandbox, and
// ephemeral facts are admitted from the decoded thread-start Server
// Observation through StartThreadRunRequest.AdmitTurn before turn/start.
// Call, CallDetailed, and the adapter-owned Stream share that fail-closed
// contract. Stream keeps exact SDK notifications, lifecycle observation,
// terminal results, errors, and a typed SDKStream escape hatch without
// projecting away generated facts. A decoded partial start remains
// observable and profile-checked when its required thread identity is
// missing; pre-response failures do not create a synthetic profile mismatch.
// Effectful Codex use stays on explicit Exact Run / ThreadRunner surfaces.
// Call projects each provider-neutral fact from independently isolated
// Codex evidence so an exact-details snapshot failure does not erase
// attributable model or usage observation.
// The package does not own Go type projection, decoding, validation, retries,
// transport, or business semantics.
//
// StrictOutputSchemaFromJSON preserves supported constraints and unknown
// keyword JSON values, but intentionally narrows the JSON Schema instance
// language by promoting an optional property to required only when its complete
// schema admits null. That narrowing preserves ordinary Go decoded values only
// when absence and explicit null decode alike; custom unmarshalers,
// json.RawMessage, presence-sensitive domain meaning, and arbitrary application
// semantic equivalence are outside the guarantee. Unprovable null admission and
// unsupported references, draft identifiers, or vocabulary declarations fail
// closed with a stable SchemaPolicyError kind and JSON Pointer path before a
// Caller invokes its runner. Retaining a boolean schema, unknown keyword, or
// lone dynamic anchor does not guarantee Codex acceptance or unsupported
// assertion and dynamic-resolution semantics. Serialization is not
// byte-preserving.
package codexcaller
