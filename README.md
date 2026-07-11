# llmkit-go

Provider-neutral Go primitives for typed LLM programming.

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

This project is usable but still pre-v1. The public API is intentionally small;
see [API compatibility](#api-compatibility) before depending on it from a
library with a strict compatibility policy.

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
go get github.com/ronhuafeng/llmkit-go@latest
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

### llmstep

Use `llmstep` when one typed structured-output call needs deterministic
validation and bounded retries with sanitized validation feedback.

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

## Versioning

Releases should be tagged as standard Go module tags. For a future release,
replace the version below with the next intended version:

```sh
VERSION=v0.2.0
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

Do not report vulnerabilities by opening a public issue with exploit details.
Use GitHub private vulnerability reporting:

<https://github.com/ronhuafeng/llmkit-go/security/advisories/new>

See [SECURITY.md](SECURITY.md) for supported versions and disclosure handling.

## License and Dependency Provenance

`llmkit-go` is released under the MIT License. See [LICENSE](LICENSE).

Dependency provenance is tracked in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
and should be reviewed before each release.

## Contributing

Issues and pull requests are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md)
and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) first.

## Related Modules

This repository is intentionally provider-neutral. Provider-specific callers,
SDK wrappers, application policy, and business validation should live in
separate modules that depend on this one.
