// Package codexcaller adapts llmadapter structured calls to exact Codex thread
// lifecycle operations.
//
// It owns Codex schema admission, request/result projection, exact-default
// plumbing, and loss-aware neutral evidence projection. It does not own an
// application execution policy. A neutral Caller requires the application to
// provide StartThreadRunRequest.AdmitTurn. The adapter preserves that callback
// unchanged so application-owned policy can inspect decoded effective Codex
// facts after thread/start and before turn/start.
//
// Approval, sandbox, ephemeral, permission, CWD, workspace, and related exact
// request values remain caller-owned Codex inputs. Their decoded effective
// observations remain exact Codex facts. The adapter does not classify any
// combination as safe, confidential, disclosure-safe, or retention-safe, and
// it does not provide a named safety profile or default authorization verdict.
// Effectful Codex use remains available through explicit Exact Run /
// ThreadRunner surfaces under application authority.
//
// Call, CallDetailed, and Stream preserve exact lifecycle errors and partial
// evidence. Stream keeps exact SDK notifications and a typed SDKStream escape
// hatch without projecting away generated facts. Call publishes "codex" only
// as execution-backend identity. Actual model-provider identity remains unknown
// unless exact lower-layer evidence proves it independently; adapter type,
// model name, requested settings, and Codex thread configuration are not
// provider observations. Effective model and usage facts are projected
// independently from attributable exact evidence, so an exact-details snapshot
// failure does not erase them.
//
// The package does not own Go type projection, decoding, validation, retries,
// transport, application authorization, disclosure policy, or business
// semantics.
//
// StrictOutputSchemaFromJSON preserves supported constraints and unknown
// keyword JSON values, but currently narrows the JSON Schema instance language
// by promoting an optional property to required only when its complete schema
// admits null. That schema behavior is tracked separately from execution-policy
// ownership. Unprovable null admission and unsupported references, draft
// identifiers, or vocabulary declarations fail closed with a stable
// SchemaPolicyError kind and JSON Pointer path before a Caller invokes its
// runner. Serialization is not byte-preserving.
package codexcaller
