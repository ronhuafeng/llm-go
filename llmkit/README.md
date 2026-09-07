# llmkit

Provider-neutral typed structured output, with complete stage-owned evidence,
and with no provider SDK. Destination: [NORTHSTAR.md](../NORTHSTAR.md).
Language: [CONTEXT.md](CONTEXT.md).

## Packages

| Package | Purpose | Provider dependencies |
| --- | --- | --- |
| `github.com/ronhuafeng/llm-go/llmkit/llmschema` | Project Go output types to provider-neutral JSON Schema, validate responses, and decode typed values. | Uses JSON Schema projection and validation libraries. |
| `github.com/ronhuafeng/llm-go/llmkit/llmadapter` | Build one typed request, preserve provider-neutral execution evidence, and decode the final value. | Depends on `llmschema`; no concrete provider SDK. |
| `github.com/ronhuafeng/llm-go/llmkit/llmstep` | Run typed judgment and repair retries while preserving every request/call/decode/judgment stage. | Depends on `llmadapter`; no concrete provider SDK. |

The `internal/` tree is not public API.

## Installation

Requires Go 1.23 or newer.

Install the current release with:

```sh
go get github.com/ronhuafeng/llm-go/llmkit@v0.11.0
```

## Quick Start

### llmschema

Use `llmschema` when you need a compiled structured-output contract, the
JSON Schema for an expected output type, or a one-shot decode.

```go
package main

import (
	"fmt"

	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
)

type Verdict struct {
	Status string `json:"status" jsonschema:"short final status"`
	Score  int    `json:"score,omitempty"`
}

func main() {
	contract, err := llmschema.Compile[Verdict]()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(contract.SchemaJSON()))

	value, err := contract.Decode([]byte(`{"status":"pass","score":2}`))
	if err != nil {
		panic(err)
	}
	fmt.Println(value.Status)
}
```

### llmadapter

Use `llmadapter` to keep provider-specific transport behind a narrow interface.
`Caller` is an inference capability: it receives a prompt and output schema
and returns a typed proposition plus evidence. The request cannot authorize
an external effect. An adapter may implement `Caller` only when any
model-directed execution reachable through that implementation is already
effect-free or independently authorized outside the model request.

```go
package main

import (
	"context"
	"fmt"

	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

type staticCaller struct{}

func (staticCaller) Call(ctx context.Context, request llmadapter.Request) (llmadapter.Response, error) {
	// A real caller would send request.Prompt and request.OutputSchema to a provider.
	return llmadapter.Response{FinalResponse: `{"answer":"yes"}`}, nil
}

type Answer struct {
	Answer string `json:"answer"`
}

func main() {
	result, err := llmadapter.Value[Answer](context.Background(), staticCaller{}, "Return yes.")
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Value.Answer)
	// Model stays unknown: the caller did not report one, and the prompt
	// is not promoted into an observation.
	fmt.Println(result.Response.Execution.Model.Present())
}
```

`Value` is the default typed-inference path. It compiles one
`llmschema.Contract` for the request schema and the response decode, then
returns the proposition together with the complete provider-neutral
response on success or failure.

`ValueWithContract` is the explicit reuse path: pass a caller-owned
compiled `Contract` when the same structural schema and validator must be
reused across calls or composition. `llmstep.Run` uses that path
internally. Both functions return the same evidence-bearing `ValueResult`.
Neither path owns semantic judgment, provider dialect, or effect
authority.

Provider-specific exact facts remain available through typed
`ProviderDetails` implementations supplied by adapters.

## Ownership and snapshots

Typed-inference APIs publish isolated snapshots of toolkit-owned state. This
is not a promise that every generic output is deeply immutable.

- Clone toolkit-owned schema bytes and usage before publication.
  Observation values copy by value; unknown stays unknown. Requested
  settings, defaults, and estimates cannot populate an observation.
- Copy llmstep attempt, judgment, and repair slices.
- Generic outputs use ordinary Go value semantics.
- Adapters own `ProviderDetails`: isolated, non-nil, matching provider identity,
  no mutable transport aliases.

### llmstep

Use `llmstep` when one typed structured-output call needs deterministic
judgment and bounded retries with sanitized repair.

`Run` is the only public step path. It records the validator's exact
decision in `Attempt.Judgment` and sanitized, stamped text in
`Attempt.NextRepair`. Only `NextRepair` goes to the next `Render`, and only
when that render will run. A final rejected attempt keeps `NextRepair` nil
and returns `llmstep.ErrExhausted` without invoking the sanitizer. A step
without a deterministic judge returns `llmstep.ErrNilValidate` before
`Render` or a provider call. Decode-only proposition production remains
available through `llmadapter`.

When `Step.Sanitizer` is nil, `StrictRepairSanitizer` rejects non-empty
`Summary` and allows identifier-oriented `Codes` and `Locations`. Put secrets
in neither field. A custom sanitizer is application-owned model-facing policy,
not DLP.

```go
result, err := llmstep.Run(ctx, llmstep.Step[ReviewInput, ReviewResult]{
	Caller:   caller,
	Render:   renderReviewPrompt,
	Validate: validateReviewResult,
	MaxIter:  3,
}, input)
```

`Run` returns the accepted output together with attempt errors,
judgment, repair, provider response evidence, and the latest partial output
after a failure or exhausted retry bound.

`Run` observes context cancellation before work begins and at its
documented callback boundaries after Render, provider Call, and Validate. If
cancellation is observed after one of those phases succeeds, it returns the
context error while retaining completed partial output and phase evidence. A
callback's own error takes precedence over cancellation observed at the same
boundary. Cancellation after the final observation can still race with a
successful return, as with other cooperative Go context APIs.

Changelog: [CHANGELOG.md](CHANGELOG.md). License: [LICENSE](LICENSE).
Notices: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

See [CONTRIBUTING.md](../CONTRIBUTING.md) and [SECURITY.md](../SECURITY.md).
