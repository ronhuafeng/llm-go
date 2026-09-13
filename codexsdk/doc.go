// Package codexsdk provides a concrete client for one local Codex App Server.
//
// Client owns process/JSON-RPC lifecycle, generated protocol facades, thread and
// turn execution, notifications, and typed server-request delivery. Exact Run
// APIs retain Codex-specific lifecycle data and partial results when available.
//
// Admission callbacks and server-request answers are application-owned. The SDK
// never invents approval, permission, user input, or other application policy.
// FinalResponse is a convenience over exact run data and does not rewrite the
// server-reported terminal status.
//
// The generated API follows the checked-in protocol baseline; live request
// success is not a whole-protocol compatibility guarantee.
package codexsdk
