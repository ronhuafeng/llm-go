# Thread Client Defaults Design

Status: historical; API removed in v0.3

This document records the former v0.1 compatibility design. Its declarations
were removed in favor of exact generated params and `ThreadRunner`; see
[the migration mapping](v0.2-migration.md#removed-v01-mapping).

## Summary

Extend `ThreadClientOptions` with optional defaults for approval policy,
approval reviewer, and ephemeral thread creation.

This supports structured-output caller layers such as `llmcaller-codex-go`
without turning `codexsdk-go` into an LLM DSL. The SDK should continue to own
only app-server protocol, thread lifecycle, streaming, and request defaults.

## Motivation

`ThreadClientOptions` already defaults model, working directory, and reasoning
effort. Downstream structured-output callers often also want stable thread
policy defaults:

- read-only calls should use `ApprovalPolicyNever`;
- structured helper calls often should be `Ephemeral: true`;
- reviewer defaults should be consistent across starts, resumes, and forks.

Today those policies must be repeated on every request or in each adapter.
Adding client-level defaults keeps policy configuration close to the
`ThreadClient` that owns thread execution.

## Domain Language

**Thread Client**
: A higher-level SDK facade that starts, resumes, streams, and forks Codex
  app-server threads.

**Thread Default**
: A value configured once on `ThreadClientOptions` and used when the individual
  request leaves the same field unset.

**Request Override**
: A non-zero request field that takes precedence over a thread default.

## Current Boundary

`codexsdk-go` should not know about `llmkit-go`, `llmcaller-codex-go`, prompt
rendering, validators, retries, or business workflows.

It may own:

- protocol request construction;
- defaulting request fields;
- validation of SDK enum values;
- failure before app-server mutation when request state is invalid.

## Proposed API

Add fields to `ThreadClientOptions`:

```go
type ThreadClientOptions struct {
    DefaultModel             string
    DefaultCWD               string
    DefaultEffort            ReasoningEffort
    DefaultApprovalPolicy    ApprovalPolicy
    DefaultApprovalsReviewer ApprovalsReviewer
    DefaultEphemeral         *bool
}
```

Defaulting rules:

- `StartThreadRequest.Model`, `CWD`, `Effort`, `ApprovalPolicy`,
  `ApprovalsReviewer`, and `Ephemeral` override `ThreadClientOptions`
  defaults.
- `ResumeThreadRequest.Model`, `CWD`, `Effort`, `ApprovalPolicy`, and
  `ApprovalsReviewer` override defaults. Resume does not have `Ephemeral`.
- `ForkThreadRequest.Model`, `CWD`, `ApprovalPolicy`, `ApprovalsReviewer`, and
  `Ephemeral` override defaults.
- Zero-value `ThreadClientOptions` preserves current behavior.
- The SDK must not choose `ApprovalPolicyNever` or `Ephemeral: true` by
  default. Applications opt into those defaults explicitly.

## Implementation Notes

Prefer small helper functions over a broad configuration framework:

```go
func defaultApprovalPolicy(request ApprovalPolicy, fallback ApprovalPolicy) ApprovalPolicy
func defaultApprovalsReviewer(request ApprovalsReviewer, fallback ApprovalsReviewer) ApprovalsReviewer
func defaultBoolPointer(request *bool, fallback *bool) *bool
```

Copy pointer defaults before assigning them to protocol params so callers
cannot observe or mutate internal state through shared pointers.

Keep error behavior consistent with existing request-level validation:

- invalid approval policy still fails before `thread/start`, `thread/resume`,
  or `thread/fork`;
- invalid approval reviewer still fails before the app-server call;
- request values override defaults even when defaults are set.

## Non-Goals

- No `llmkit-go` dependency.
- No `llmcaller-codex-go` dependency.
- No structured-output DSL.
- No retry API.
- No read-only convenience constructor in the SDK.
- No app-server process management changes.
- No behavior change for callers that do not set the new defaults.

## Test Plan

Add semantic tests around the public `ThreadClient` contract:

- start applies default approval policy, reviewer, and ephemeral when request
  fields are unset;
- request start fields override defaults;
- resume applies default approval policy and reviewer;
- fork applies default approval policy, reviewer, and ephemeral;
- invalid default approval policy fails before sending protocol calls;
- zero-value options keep existing request output unchanged.

Follow `Agents.test.md`: tests should assert externally meaningful protocol
params and failure behavior, not private helper internals.

## Acceptance Criteria

- `go test ./...` passes.
- `go vet ./...` passes.
- README or package docs document the new defaults.
- Existing examples and tests keep working without setting new fields.
- The change is additive and source-compatible.
- `ThreadClientOptions` remains a simple struct, not a policy framework.

## Downstream Impact

`llmcaller-codex-go` can continue to expose its own
`ReadOnlyEphemeralOptions` helper. When users configure the SDK thread client
with defaults, the caller helper has less per-request state to carry.

The SDK does not need to know whether a thread run is used for `llmstep`,
journal drafting, or any other structured-output task.
