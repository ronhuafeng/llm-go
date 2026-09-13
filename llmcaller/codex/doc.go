// Package codexcaller adapts llmadapter calls to codexsdk thread/turn execution.
//
// The adapter owns Codex request assembly, output-schema conversion, and mapping
// Codex results into provider-neutral llmadapter fields. It preserves exact
// Codex details when the neutral API cannot represent them.
//
// Caller-supplied admission decides whether execution may continue. The adapter
// does not define application approval, sandbox, permission, disclosure, or
// side-effect policy. Schema conversion either preserves the caller's accepted
// JSON values or fails before execution.
package codexcaller
