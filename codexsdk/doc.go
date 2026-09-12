// Package codexsdk provides a concrete Client for interacting with a Codex
// app-server. New returns the only connected form, *Client. The safe, inert
// zero value closes successfully and returns ErrClientClosed from operations.
// Generated facade accessors return exported concrete opaque values. Consumers
// should define narrow interfaces around only the operations they use.
//
// Client owns process transport, client lifecycle, generated protocol
// facades, exact thread/turn composition, and typed callback delivery. Exact
// run notifications retain ingestion order across stream attachment: pending
// notifications are accepted before later live notifications for the same run.
// A successfully decoded lifecycle response remains observable as exact
// partial evidence when a required thread or turn identity is missing.
// ErrMissingThreadID and ErrMissingTurnID fail closed before the next lifecycle
// stage or live run registration; these malformed responses do not close the
// Client.
// StartThreadRunRequest.AdmitTurn and ResumeThreadRunRequest.AdmitTurn inspect
// the decoded lifecycle Server Observation and the exact pending turn/start
// params before turn/start. Rejection finishes the Exact Run fail-closed with
// the exact partial result and ErrTurnAdmissionRejected, and does not send
// turn/start. The callback is caller-owned and is not a provider-neutral policy.
// Exact run results retain complete immutable notification history independent
// of observation. Wait observes completion without consuming notifications;
// Next advances a cursor over the same ordered history. The configurable
// global notification-handler queue remains bounded, and its overflow closes
// the client with ErrNotificationBackpressure.
// FinalResponse is a convenience projection over the exact terminal Turn.
// FinalResponsePresent distinguishes no observed final-answer item from an
// observed final-answer item whose text is empty. The server-reported terminal
// status remains authoritative and is not rewritten by that projection.
// Exact run history follows generated-schema identity: turn-scoped facts attach
// only to the matching turn; thread-scoped facts attach to every run currently
// active or attaching for that thread and are not retained for a later run;
// client/global facts never enter per-run evidence. Every validated generated
// notification is still enqueued for the global handler, in ingestion order,
// after its justified per-run append completes. A terminal exact notification
// cannot complete its affected stream until that notification's global handler
// invocation has returned and any handler failure is published as the client
// first cause. If shutdown, failure, or bounded backpressure rejects handler
// work before queue ownership transfers, its dispatch fence is released as
// discarded without removing already accepted exact-run evidence.
// Exact server-request responses are application-owned. Client delivers exact
// generated requests and encodes caller-supplied typed responses, but it does
// not choose approvals, user answers, permissions, elicitation outcomes, or
// environment facts. A missing ServerRequestHandler fails with a typed exact
// server-request cause and JSON-RPC error rather than a synthesized successful
// response. A request arriving after callback admission closes receives only a
// protocol error; shutdown does not manufacture an application decision.
// Exact server-request failures close callback admission and publish their
// typed first cause to active runs before the fail-closed protocol response can
// prompt a terminal notification; transport teardown starts only after that
// response is written.
// Client shutdown atomically closes callback admission and joins callbacks
// accepted before that boundary before releasing transport resources.
//
// Generated Baseline Provenance is exact to the checked-in protocol baseline.
// Runtime App-Server Observation preserves initialize identity when reported.
// Runtime Compatibility remains unknown unless the protocol reports a usable
// compatibility fact. Connection Provenance keeps those facts separate. A
// successful request is not whole-surface compatibility.
//
// It does not provide provider-neutral LLM abstractions, business validation,
// workflow policy, or application safety profiles.
package codexsdk
