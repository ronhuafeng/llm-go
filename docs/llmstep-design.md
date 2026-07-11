# Typed LLM Step DSL Design

Status: implemented

## Summary

Add a small provider-neutral `llmstep` package to `llmkit-go`.

The package should own the reusable mechanics for a single typed structured
LLM step:

```text
render prompt
-> call llmadapter.ValueDetailed[O]
-> validate typed output
-> sanitize validation feedback
-> retry up to MaxIter
```

It must not become a workflow engine. Business prompts, semantic validators,
provider transport, write gates, and application policy remain outside
`llmkit-go`.

## Motivation

The Codex journal refactor proved that `llmschema`, `llmadapter`, and `settle`
are useful but leave repeated glue in application repositories. The journal
implementation needed a thin helper that combined:

- typed prompt rendering;
- schema-bound provider-neutral calls;
- typed JSON decode;
- deterministic business validation;
- safe validation feedback;
- bounded retry.

That helper is currently project-local. Its shape is not journal-specific: the
journal package supplies only its `DraftRequest`, `JournalTasks`, prompt
renderer, and verifier. The generate/validate/retry shell is reusable across
structured-output tasks.

## Domain Language

**Typed Step**
: One LLM operation with typed input `I`, typed output `O`, a prompt renderer,
  a validator, and a bounded retry policy.

**Feedback**
: Sanitized validation information that may be sent back to the model on a
  retry. It is not raw verifier output.

**Settled Output**
: A typed output accepted by the validator.

**Unsettled Output**
: A run that exhausted `MaxIter` without any validator-accepted output.

## Current Boundaries

Keep the current package responsibilities:

- `llmschema`: Go type to JSON Schema projection and JSON decode.
- `llmadapter`: provider-neutral request/response and one typed call.
- `settle`: general-purpose operation retry loop.
- new `llmstep`: typed structured-output retry with feedback.

Do not put `llmstep` into `llmadapter`. `llmadapter.ValueDetailed[T]` remains
the one-call evidence-preserving core, while `Value[T]` is its simple projection.

## Proposed API

```go
package llmstep

import (
    "context"

    "github.com/ronhuafeng/llmkit-go/llmadapter"
)

type Feedback struct {
    Iteration int      `json:"iteration,omitempty"`
    Summary   string   `json:"summary,omitempty"`
    Codes     []string `json:"codes,omitempty"`
    Locations []string `json:"locations,omitempty"`
}

type ValidationResult struct {
    Settled  bool       `json:"settled"`
    Feedback []Feedback `json:"feedback,omitempty"`
}

type FeedbackSanitizer func([]Feedback) ([]Feedback, error)

type Step[I any, O any] struct {
    Caller    llmadapter.Caller
    Render    func(context.Context, I, []Feedback) (string, error)
    Validate  func(context.Context, I, O) (ValidationResult, error)
    MaxIter   int
    Sanitizer FeedbackSanitizer
}

type Stage string

const (
    StageRender   Stage = "render"
    StageRequest  Stage = "request"
    StageCall     Stage = "call"
    StageDecode   Stage = "decode"
    StageValidate Stage = "validate"
    StageSanitize Stage = "sanitize"
)

type StepError struct {
    Stage     Stage
    Iteration int
    Err       error
}

type Attempt[O any] struct {
    Iteration  int
    Feedback   []Feedback
    Call       llmadapter.ValueResult[O]
    Validation ValidationResult
    Err        error
}

type Result[O any] struct {
    Output    O
    HasOutput bool
    Attempts  []Attempt[O]
}

func Run[I any, O any](ctx context.Context, step Step[I, O], input I) (O, error)
func RunDetailed[I any, O any](ctx context.Context, step Step[I, O], input I) (Result[O], error)
func StrictFeedbackSanitizer(feedback []Feedback) ([]Feedback, error)
```

`Run` is the simple projection. `RunDetailed` is the implementation core for
tests, debugging, and audit surfaces that need provider responses, partial
outputs, stage errors, and attempt history. It deliberately does not retain
rendered prompts; callers that need prompt capture should wrap their renderer.

## Behavior

1. Fail fast when `MaxIter < 1`, `Caller == nil`, or `Render == nil`.
2. Render with an empty feedback slice on the first attempt.
3. Call `llmadapter.ValueDetailed[O]`, so schema generation, provider response
   evidence, and decode remain owned by `llmadapter` and `llmschema`.
4. If `Validate` is nil, treat the output as settled.
5. If validation returns `Settled: true`, return the typed output.
6. If validation returns `Settled: false`, sanitize feedback before the next
   render.
7. If the sanitizer rejects feedback, stop with a typed error.
8. If all attempts fail validation, return an error wrapping
   `settle.ErrUnsettled`.

The framework should not modify the business input `I` or output `O`.

## Feedback Safety

The default sanitizer should be conservative:

- allow short summaries, code-like tokens, and location-like identifiers;
- reject strings that look like URLs, private paths, authorization headers,
  bearer tokens, API keys, passwords, or secrets;
- drop empty feedback items;
- stamp the iteration when not already set;
- copy slices before returning them.

Applications may pass their own sanitizer when they have a narrower safe
identifier vocabulary. For example, a journal validator can allow only `T###`,
`U###`, `M#####`, known section names, and UUID session ids.

## Non-Goals

- No provider-specific code.
- No Codex dependency.
- No prompt templates.
- No business verifier.
- No semantic review.
- No write gate.
- No multi-step workflow orchestration.
- No tool calling or streaming abstraction.

## Compatibility

This is additive. Existing packages and APIs remain valid.

`llmadapter.Op` was retained and deprecated during v0.2, then removed in v0.3.
`llmstep` is the path when a typed LLM call needs validation feedback across
retries; generic stabilization uses an application-owned `settle.Op` directly.

## Test Plan

Add focused tests for externally visible behavior:

- first render receives no feedback;
- failed validation feeds sanitized feedback into the next render;
- settled output returns the typed value;
- exhausted attempts return an error wrapping `settle.ErrUnsettled`;
- nil caller, nil renderer, and invalid max iteration fail fast;
- invalid JSON or decode failure stops without retrying as validation;
- unsafe feedback is rejected before it reaches the renderer;
- custom sanitizer can replace default sanitizer behavior;
- `RunDetailed` exposes attempt count and sanitized feedback history.

Tests should use a fake `llmadapter.Caller` and small synthetic typed structs.

## Acceptance Criteria

- New `llmstep` package exists with README or package docs.
- `go test ./...` passes.
- `go vet ./...` passes.
- No new concrete provider dependency is added to `llmkit-go`.
- The v0.2 implementation was additive; the deprecated compatibility helpers
  were subsequently removed for v0.3 as documented in `v0.3-migration.md`.
- README documents when to use `llmstep` versus `llmadapter.Value` and
  `settle.Run`.
- The implementation contains no journal, Codex, or application-specific
  concepts.

## Example

```go
type ReviewInput struct {
    Patch string `json:"patch"`
}

type ReviewResult struct {
    Verdict string `json:"verdict"`
}

out, err := llmstep.Run(ctx, llmstep.Step[ReviewInput, ReviewResult]{
    Caller: caller,
    Render: func(ctx context.Context, input ReviewInput, feedback []llmstep.Feedback) (string, error) {
        return renderReviewPrompt(input, feedback), nil
    },
    Validate: func(ctx context.Context, input ReviewInput, output ReviewResult) (llmstep.ValidationResult, error) {
        if output.Verdict == "pass" || output.Verdict == "fail" {
            return llmstep.ValidationResult{Settled: true}, nil
        }
        return llmstep.ValidationResult{
            Settled: false,
            Feedback: []llmstep.Feedback{{
                Summary: "verdict must be pass or fail",
                Codes: []string{"invalid_verdict"},
            }},
        }, nil
    },
    MaxIter: 3,
}, ReviewInput{Patch: patch})
```
