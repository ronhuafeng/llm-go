# llmkit-go

Provider-neutral Go primitives for typed LLM programming.

> **Important:** This legacy module is frozen at `v0.5.0`. Development has
> moved to `github.com/ronhuafeng/llm-go/llmkit`, whose first release is
> `llmkit/v0.6.0`. This repository receives no further feature or security
> maintenance after the cutover. See the
> [llm-go migration guide](docs/llm-go-migration.md).

`llmkit-go` is a small toolkit for code that wants structured LLM output
without taking a dependency on a specific model provider SDK. It focuses on
four stable boundaries:

- `settle`: bounded stabilization with complete attempt evidence.
- `llmschema`: Go type to JSON Schema projection and schema-enforced decode.
- `llmadapter`: one provider-neutral typed call with execution evidence.
- `llmstep`: typed validation-feedback retries with stage-specific history.

Concrete provider callers live in separate modules. This repository does not
own provider transport, provider credentials, prompt libraries, tracing
backends, or business validation rules.

## Status

`v0.5.0` is the final release from this legacy module path. New consumers
should use `github.com/ronhuafeng/llm-go/llmkit@v0.6.0`. Existing consumers
should follow the [llm-go migration guide](docs/llm-go-migration.md); the
migration changes import paths but adds no forwarding or runtime compatibility
layer.

## Packages

| Package | Purpose | Provider dependencies |
| --- | --- | --- |
| `github.com/ronhuafeng/llmkit-go/settle` | Run and validate bounded candidates while preserving stage-specific attempt history. | Standard library only. |
| `github.com/ronhuafeng/llmkit-go/llmschema` | Project Go output types to provider-neutral JSON Schema, validate responses, and decode typed values. | Uses JSON Schema projection and validation libraries. |
| `github.com/ronhuafeng/llmkit-go/llmadapter` | Build one typed request, preserve provider-neutral execution evidence, and decode the final value. | Depends on `llmschema`; no concrete provider SDK. |
| `github.com/ronhuafeng/llmkit-go/llmstep` | Run typed validation-feedback retries while preserving every request/call/decode/validation stage. | Depends on `llmadapter` and `settle`; no concrete provider SDK. |

The `internal/` tree contains repository tests and is not public API.

## Installation

Requires Go 1.23 or newer.

```sh
go get github.com/ronhuafeng/llmkit-go@v0.5.0
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

	"github.com/ronhuafeng/llmkit-go/settle"
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

	"github.com/ronhuafeng/llmkit-go/llmschema"
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

	"github.com/ronhuafeng/llmkit-go/llmadapter"
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

Detailed APIs publish owned, isolated snapshots of toolkit-owned state. This is
an aliasing guarantee for state whose representation the toolkit owns, not a
promise that every returned Go value is deeply immutable.

- `llmadapter.Request.OutputSchema` is cloned before caller invocation. Callers
  may use or mutate their copy during `Call`, but must not retain and later
  mutate toolkit-owned request state in a way that affects another call.
- `llmadapter.ValueDetailed` preserves available response evidence on call and
  decode errors and clones `Execution.Usage` before publication.
- Provider adapters own `ProviderDetails` cloning. When details are provided,
  they must be an isolated, non-nil typed value whose provider identity matches
  neutral execution evidence; they must not alias mutable transport or SDK
  state.
- `ValueResult.Value`, `settle.Result.Output`, attempt candidates, and
  `llmstep.Result.Output` follow ordinary Go value semantics. Maps, slices,
  pointers, and custom mutable fields are not generically deep-copied.
- `settle` attempt slices and `llmstep` attempt, validation, feedback, code, and
  location slices are copied before publication.

An adapter should copy provider-owned reference fields while constructing its
details value. For example, `details{Headers: maps.Clone(response.Headers)}` is
safe; `details{Headers: response.Headers}` is unsafe if the transport may later
mutate that map. Applications requiring deeply immutable generic outputs should
use immutable domain types or explicitly clone their values.

Detailed field ownership is summarized below. Scalar strings, booleans,
integers, stages, and errors are copied by normal Go assignment.

| Published field | Ownership and aliasing contract |
| --- | --- |
| `Request.Prompt` | Go string value. |
| `Request.OutputSchema` | Toolkit-owned bytes cloned before `Caller.Call`. |
| `Response.FinalResponse` | Go string value, preserved when available on call/decode failure. |
| `Response.Execution` | Provider-neutral value; its `Usage` pointer is cloned by `ValueDetailed`. |
| `Response.ProviderDetails` | Adapter-owned isolated typed value; the adapter must remove mutable runtime aliases. |
| `ValueResult.Value` | Generic value with ordinary Go semantics; reference fields may alias. |
| `ValueResult.Response` | Available response evidence preserved on call/decode errors with the ownership rules above. |
| `settle.Result.Output` and `Attempt.Output` | Generic values with ordinary Go semantics; the latest result and recorded candidate may share reference fields. |
| `settle.Result.Attempts` | Toolkit-owned slice snapshot; scalar attempt fields and errors use normal Go assignment. |
| `llmstep.Result.Output` and `Attempt.Call.Value` | Generic values with ordinary Go semantics; reference fields may alias. |
| `llmstep.Result.Attempts` | Toolkit-owned slice snapshot. |
| `llmstep.Attempt.Feedback` | Toolkit-owned snapshot of the prior retry feedback supplied to this attempt's `Render`. |
| `llmstep.Attempt.Validation` | Validator decision exactly as returned, including nil-versus-empty slice shape, published as a toolkit-owned isolated snapshot including nested feedback slices. |
| `llmstep.Attempt.RetryFeedback` | Sanitizer-owned, iteration-stamped model-facing feedback published as a toolkit-owned isolated snapshot only when a subsequent retry exists. |
| `llmstep.Attempt.Call.Response` | Response evidence following the `llmadapter.Response` rules above. |

### llmstep

Use `llmstep` when one typed structured-output call needs deterministic
validation and bounded retries with sanitized validation feedback.

`RunDetailed` records the validator's exact decision in `Attempt.Validation`
and records sanitized, stamped feedback separately in `Attempt.RetryFeedback`.
Only `RetryFeedback` is eligible for the next `Render`; it is produced only
when that render will run. A final unsettled attempt keeps `RetryFeedback` nil
and returns an error wrapping `settle.ErrUnsettled`, even if its validation
feedback would fail sanitization. Sanitization never rewrites the validation
record.

Validator decisions are detailed evidence and may be retained, logged, or
serialized by applications. A validator must not return raw credentials,
private content, or other secrets unless that retention is explicitly intended.
When sensitive source material is involved, the application should return an
already-redacted decision (for example, a classification code and abstract
location), or omit the sensitive value. If correlation is required, the
application may substitute a threat-model-reviewed keyed, domain-separated
pseudonymous fingerprint, such as an HMAC whose key is kept outside validator
evidence. A plain hash of a low-entropy or guessable value is not redaction.
Fingerprints remain potentially sensitive and linkable. `llmstep` treats them
as opaque: it does not compute, key, verify, or promise their security. The
sanitizer independently determines whether a redacted or fingerprinted fact may
be sent to the model.

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

## API Compatibility

Public API is limited to exported identifiers in these packages:

- `settle`
- `llmschema`
- `llmadapter`
- `llmstep`

Everything under `internal/` is private. README examples are illustrative and
may change, but they are compiled in tests where practical. Exported package
behavior and the canonical handwritten API allowlist are compatibility surface.

Before v1.0.0, this project follows SemVer with a conservative pre-v1 policy:
patch releases should be bug fixes only, minor releases may add API, and any
known breaking API change must be called out in `CHANGELOG.md` and the release
notes. After v1.0.0, breaking public API changes require a new major version.

Version 0.3 removes the helpers deprecated in v0.2. See
[Migrating to v0.3](docs/v0.3-migration.md) for the complete symbol mapping.

Version 0.4 separates exact validator decisions from sanitized model-facing
retry feedback. See [Migrating to v0.4](docs/v0.4-migration.md) for the field
semantics and sensitive-feedback boundary.

Version 0.5 limits sanitization to feedback that will reach a real retry; final
unsettled attempts now return `settle.ErrUnsettled` directly. See
[Migrating to v0.5](docs/v0.5-migration.md) for the corrected terminal error
semantics.

## Versioning

Releases are tagged as standard Go module tags. The final legacy release is:

```sh
VERSION=v0.5.0
git tag -a "$VERSION" -m "llmkit-go $VERSION"
git push origin "$VERSION"
```

See [docs/release.md](docs/release.md) for the release checklist.

## Testing

Run the same checks used by CI:

```sh
gofmt -w $(find . -name '*.go' -not -path './vendor/*')
test -z "$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))"
go vet ./...
go test ./...
```

If this repository is inside a larger local `go.work`, use `GOWORK=off` to
verify it as a standalone module:

```sh
GOWORK=off go test ./...
```

## Security

This frozen repository receives no security maintenance after cutover. For
sensitive reports, continue to use this repository's
[private vulnerability reporting](https://github.com/ronhuafeng/llmkit-go/security/advisories/new)
until the successor repository publishes its own confidential intake policy.
This intake path does not make published legacy versions supported. See
[SECURITY.md](SECURITY.md).

## License and Dependency Provenance

`llmkit-go` is released under the MIT License. See [LICENSE](LICENSE).

Dependency provenance is tracked in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
and should be reviewed before each release.

## Contributing

This repository is frozen. Migration, release-record, and archive corrections
may still be considered, but feature and security work belongs in
[`ronhuafeng/llm-go`](https://github.com/ronhuafeng/llm-go). See
[CONTRIBUTING.md](CONTRIBUTING.md) for the narrow legacy scope.

## Related Modules

The successor toolkit remains provider-neutral at
`github.com/ronhuafeng/llm-go/llmkit`. Provider-specific callers, SDK wrappers,
application policy, and business validation live in separate modules; see the
[llm-go migration guide](docs/llm-go-migration.md) for exact paths.
