# llmkit

Provider-neutral typed structured output, with complete stage-owned evidence,
and with no provider SDK. Destination: [NORTHSTAR.md](../NORTHSTAR.md).
Language: [CONTEXT.md](CONTEXT.md).

## Packages

| Package | Purpose | Provider dependencies |
| --- | --- | --- |
| `github.com/ronhuafeng/llm-go/llmkit/settle` | Run and validate bounded candidates while preserving stage-specific attempt history. | Standard library only. |
| `github.com/ronhuafeng/llm-go/llmkit/llmschema` | Project Go output types to provider-neutral JSON Schema, validate responses, and decode typed values. | Uses JSON Schema projection and validation libraries. |
| `github.com/ronhuafeng/llm-go/llmkit/llmadapter` | Build one typed request, preserve provider-neutral execution evidence, and decode the final value. | Depends on `llmschema`; no concrete provider SDK. |
| `github.com/ronhuafeng/llm-go/llmkit/llmstep` | Run typed validation-feedback retries while preserving every request/call/decode/validation stage. | Depends on `llmadapter` and `settle`; no concrete provider SDK. |

The `internal/` tree is not public API.

## Installation

Requires Go 1.23 or newer.

Install the current release with:

```sh
go get github.com/ronhuafeng/llm-go/llmkit@v0.7.0
```

## Quick Start

### settle

Use `settle.Run` when an operation may need a few bounded attempts before the
output is acceptable.

```go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ronhuafeng/llm-go/llmkit/settle"
)

type op struct {
	attempt int
}

func (o *op) Run(ctx context.Context, input string) (string, error) {
	o.attempt++
	if o.attempt == 1 {
		return "draft", nil
	}
	return input + " final", nil
}

func (o *op) Validate(ctx context.Context, input, result string) (bool, error) {
	return strings.Contains(result, input), nil
}

func main() {
	got, err := settle.Run(context.Background(), &op{}, "ship", 3)
	if err != nil {
		panic(err)
	}
	fmt.Println(got)
}
```

### llmschema

Use `llmschema` when you need the JSON Schema for an expected output type or
need to decode the provider's final structured JSON.

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
	schema, err := llmschema.SchemaJSONFor[Verdict]()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(schema))

	value, err := llmschema.Decode[Verdict]([]byte(`{"status":"pass","score":2}`))
	if err != nil {
		panic(err)
	}
	fmt.Println(value.Status)
}
```

### llmadapter

Use `llmadapter` to keep provider-specific transport behind a narrow interface.
Your provider caller receives a prompt and schema, then returns the final JSON
text to decode.

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
	result, err := llmadapter.ValueDetailed[Answer](context.Background(), staticCaller{}, "Return yes.")
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Value.Answer)
}
```

Use `Value` when only the typed value is needed. `ValueDetailed` is the core
path and also returns the complete provider-neutral response on success or
failure. Provider-specific exact facts remain available through typed
`ProviderDetails` implementations supplied by adapters.

## Ownership and snapshots

Detailed APIs publish isolated snapshots of toolkit-owned state. This is not a
promise that every generic output is deeply immutable.

- Clone toolkit-owned schema bytes and usage before publication.
- Copy settle/llmstep attempt, validation, and feedback slices.
- Generic outputs use ordinary Go value semantics.
- Adapters own `ProviderDetails`: isolated, non-nil, matching provider identity,
  no mutable transport aliases.

### llmstep

Use `llmstep` when one typed structured-output call needs deterministic
validation and bounded retries with sanitized validation feedback.

`RunDetailed` records the validator's exact decision in `Attempt.Validation`
and sanitized, stamped text in `Attempt.RetryFeedback`. Only `RetryFeedback`
goes to the next `Render`, and only when that render will run. A final
unsettled attempt keeps `RetryFeedback` nil and returns `settle.ErrUnsettled`
without invoking the sanitizer.

When `Step.Sanitizer` is nil, `StrictFeedbackSanitizer` rejects non-empty
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

Use `settle.Run` directly when retry state already lives in your operation and
you do not need validation feedback passed back into prompt rendering.

Use `settle.RunDetailed` and `llmstep.RunDetailed` when callers need candidates,
attempt errors, validation feedback, provider response evidence, or the latest
partial output after a failure or exhausted retry bound.

Both detailed retry APIs observe context cancellation before work begins and at
their documented callback boundaries: `settle` checks after Run and Validate;
`llmstep` checks after Render, provider Call, and Validate. If cancellation is
observed after one of those phases succeeds, they return the context error while
retaining completed partial output and phase evidence. A callback's own error
takes precedence over cancellation observed at the same boundary. Cancellation
after the final observation can still race with a successful return, as with
other cooperative Go context APIs.

Changelog: [CHANGELOG.md](CHANGELOG.md). License: [LICENSE](LICENSE).
Notices: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

See [CONTRIBUTING.md](../CONTRIBUTING.md) and [SECURITY.md](../SECURITY.md).
