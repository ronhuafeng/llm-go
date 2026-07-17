# Integrating `llm-go`

This guide is the implementation entry point for coding agents adding
`llm-go` to a Go project. Select the narrowest module that owns the requested
behavior, follow its recommended path, and do not copy types or policy across
module seams.

## Select one path

| Goal | Use | Do not add |
| --- | --- | --- |
| Generate JSON Schema, decode typed JSON, validate output, or retry with validation feedback for any provider | `llmkit` | `codexsdk` or the Codex adapter |
| Make a typed structured-output call through a local Codex app-server | `llmkit` + `llmcaller/codex` + `codexsdk` | A second schema converter, transport wrapper, or copied Codex option model |
| Use exact Codex thread, turn, streaming, notification, or generated protocol operations | `codexsdk` | `llmkit` unless the task also needs provider-neutral structured output |
| Retry a non-LLM operation until its output validates | `llmkit/settle` | Provider or Codex dependencies |

Prefer the highest-level path that satisfies the task:

1. Use `llmadapter.Value` for one typed result.
2. Use `llmadapter.ValueDetailed` when the caller needs call metadata or the
   complete provider result.
3. Use `llmstep.Run` when application validation may produce bounded retry
   feedback.
4. Use `codexcaller.CallDetailed` or `CallStream` only when the application
   needs exact Codex behavior that the provider-neutral result does not expose.
5. Use `codexsdk` directly when the task is a Codex lifecycle or protocol task,
   not a provider-neutral typed-output task.

`internal/tools` is repository tooling, not a consumer library. Never import
it into an application.

## Requirements and installation

All modules require Go 1.23 or newer. The SDK is unofficial and launches a
locally installed `codex app-server` over stdio; ensure `codex` is on `PATH`.

Install only the selected public module. Go resolves its declared
dependencies:

```sh
# Provider-neutral toolkit only
go get github.com/ronhuafeng/llm-go/llmkit@v0.6.0

# Exact Codex SDK only
go get github.com/ronhuafeng/llm-go/codexsdk@v0.6.0

# Structured Codex integration; this brings in llmkit and codexsdk
go get github.com/ronhuafeng/llm-go/llmcaller/codex@v0.5.0
```

Do not import packages through the repository root; it is not a Go module.

## Recommended structured Codex path

Use this path when the application wants a Go value decoded from structured
Codex output. It creates a local SDK client, applies the adapter's named
read-only profile, generates a schema from the result type, runs Codex, and
decodes the final JSON.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

type Answer struct {
	Summary string `json:"summary"`
}

func main() {
	ctx := context.Background()
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     workspace,
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	options := codexcaller.ReadOnlyEphemeralOptions(client.ThreadRunner())
	options.Defaults.Thread.Model = protocolv2.Value("gpt-5")
	options.Defaults.Thread.CWD = protocolv2.Value(workspace)

	caller, err := codexcaller.New(options)
	if err != nil {
		log.Fatal(err)
	}

	answer, err := llmadapter.Value[Answer](ctx, caller, "Summarize this repository.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer.Summary)
}
```

`ReadOnlyEphemeralOptions` enforces ephemeral threads, read-only sandboxing,
and never-approve policy at thread and turn scope. Keep this profile unless the
task explicitly requires different Codex permissions. The adapter deliberately
has no permissive fallback profile.

The application owns the context lifetime, app-server command, model, working
directory, authentication environment, and any workspace exposure.

## Typed result versus complete result

Use `Value` when only a successful decoded value matters:

```go
answer, err := llmadapter.Value[Answer](ctx, caller, prompt)
```

Use `ValueDetailed` when the application must retain the neutral response or
the exact Codex run. Inspect the returned result even when `err` is non-nil;
the underlying operation may have produced a partial response before failing.

```go
result, err := llmadapter.ValueDetailed[Answer](ctx, caller, prompt)

if details, ok := result.Response.ProviderDetails.(codexcaller.Details); ok {
	run := details.Run
	_ = run.Start.Thread.ID
	_ = run.Run.Turn.ID
	_ = run.Run.Notifications
}
if err != nil {
	return err
}

answer := result.Value
_ = answer
```

Do not flatten `codexcaller.Details.Run` into a new generic metadata structure.
Use neutral fields for provider-independent decisions and the typed SDK result
for Codex-specific decisions.

## Validation-feedback retries

Use `llmstep` when a typed result may be syntactically valid JSON but still
fail an application rule. `Render` owns prompt construction, `Validate` owns
the application decision, and `MaxIter` is a hard bound.

```go
type ReviewInput struct {
	Question string
}

type ReviewResult struct {
	Verdict string `json:"verdict"`
}

result, err := llmstep.Run(ctx, llmstep.Step[ReviewInput, ReviewResult]{
	Caller: caller,
	Render: func(ctx context.Context, input ReviewInput, feedback []llmstep.Feedback) (string, error) {
		if len(feedback) == 0 {
			return input.Question, nil
		}
		return input.Question + "\nCorrect: " + feedback[0].Summary, nil
	},
	Validate: func(ctx context.Context, input ReviewInput, output ReviewResult) (llmstep.ValidationResult, error) {
		if output.Verdict == "pass" || output.Verdict == "fail" {
			return llmstep.ValidationResult{Settled: true}, nil
		}
		return llmstep.ValidationResult{Feedback: []llmstep.Feedback{{
			Summary: "verdict must be pass or fail",
			Codes:   []string{"invalid_verdict"},
		}}}, nil
	},
	MaxIter: 3,
}, ReviewInput{Question: "Review this change."})
```

The default sanitizer rejects unsafe retry feedback. Keep feedback short,
structured, and derived from trusted application validation. Use
`RunDetailed` only when the caller needs the complete attempt history.

Use `settle.Run` instead when retries are not LLM calls. Implement its two
method `Op` interface (`Run` and `Validate`) and set a finite maximum attempt
count.

## Direct Codex SDK path

Use `codexsdk` directly for exact lifecycle operations or generated app-server
methods. Do not introduce application copies of `protocolv2` structs.

```go
client, err := codexsdk.New(codexsdk.ClientOptions{
	CWD:     workspace,
	Command: []string{"codex", "app-server", "--listen", "stdio://"},
})
if err != nil {
	return err
}
defer client.Close()

run, err := client.ThreadRunner().Start(ctx, codexsdk.StartThreadRunRequest{
	Thread: protocolv2.ThreadStartParams{
		Ephemeral: protocolv2.Value(true),
		Model:     protocolv2.Value("gpt-5"),
	},
	Turn: protocolv2.TurnStartParams{Input: []protocolv2.UserInput{
		protocolv2.NewUserInputText(protocolv2.UserInputText{Text: prompt}),
	}},
})
if err != nil {
	// Inspect run before returning if partial lifecycle data matters.
	return err
}
fmt.Println(run.Run.FinalResponse)
```

Use the generated client facades such as `client.Models()`, `client.Threads()`,
and `client.Turns()` for individual protocol methods. Use `ThreadRunner` when a
task needs the composed start/resume plus turn lifecycle.

For streaming:

- call `StartStream` or `ResumeStream`;
- `defer stream.Close()` after a non-nil stream is returned;
- use `Next` and `Notification` for ordered notifications;
- use `Wait` for the terminal or latest partial result;
- use `Close` to cancel the shared run;
- do not assume canceling a `Wait` context cancels the run.

Direct SDK use does not apply the adapter's read-only profile. Set sandbox,
approval, ephemeral, model, and working-directory fields explicitly according
to the application's policy.

## Provider-neutral toolkit path

`llmkit` contains four public packages:

| Package | Use it for |
| --- | --- |
| `llmschema` | Generate JSON Schema from a Go result type or decode schema-validated JSON. |
| `llmadapter` | Make one provider-neutral structured call through the small `Caller` interface. |
| `llmstep` | Render, call, decode, validate, sanitize feedback, and retry a typed LLM step. |
| `settle` | Retry and validate a non-provider operation with a hard attempt limit. |

For a non-Codex provider, implement only `llmadapter.Caller`:

```go
type Caller interface {
	Call(context.Context, llmadapter.Request) (llmadapter.Response, error)
}
```

The implementation must send `Request.Prompt` and `Request.OutputSchema`, then
return the provider's final JSON text in `Response.FinalResponse`. Do not put
transport credentials, provider clients, or prompt-library policy into
`llmkit`; keep them in the provider adapter.

Use `llmschema.SchemaJSONFor[T]` when another transport needs the generated
schema and `llmschema.Decode[T]` when decoding is intentionally separate from
the call. Otherwise prefer `llmadapter.Value[T]`, which performs both steps.

## Output-schema rules for Codex

Let `llmadapter` generate the schema from the result type. Do not hand-edit the
schema merely to make Codex accept it.

When designing result structs:

- prefer required fields when absence has no application meaning;
- use pointer, slice, or another accurately nullable type when `null` is valid;
- do not assume `omitempty` makes a non-nullable scalar acceptable to Codex;
- do not rely on custom `UnmarshalJSON`, `json.RawMessage`, or presence-sensitive
  domain types to make omission and `null` equivalent;
- treat unsupported drafts, external or dynamic references, cyclic references,
  unresolved references, and unsupported vocabularies as errors before the
  Codex runner starts.

Schemas without `$schema` use JSON Schema Draft 2020-12, except the supported
tuple-form compatibility case documented by the adapter. Do not add a second
schema-normalization path in application code.

## Error handling

- Always check errors with `errors.Is` or `errors.As`; do not match error text.
- `llmadapter.ValueError` identifies request, call, or decode failure stages.
- `llmstep.StepError` identifies the iteration and render, request, call,
  decode, validate, or sanitize stage.
- `codexcaller.SchemaPolicyError` identifies a stable schema-policy kind and
  JSON Pointer path.
- `codexcaller.ErrEffectiveProfile` means the effective Codex result violated
  the named safety profile.
- SDK and adapter detailed operations may return a partial typed result and an
  error together. Preserve both when the caller needs diagnostics or recovery.
- Context cancellation bounds the operation supplied that context; close the
  client or stream when ownership ends.

## Testing integrations

Test through the same narrow interface used by production code:

- fake `llmadapter.Caller` to test typed application logic and `llmstep`;
- fake the adapter's two-method `ThreadRunner` to test Codex request mapping
  without starting a process;
- use a real `codexsdk.Client` only in an explicitly configured integration or
  smoke test;
- keep generated `protocolv2` values in test fixtures instead of replacing
  them with look-alike structs;
- cover success, malformed structured output, provider failure with a partial
  result, context cancellation, and validation exhaustion when applicable.

Run the consumer project's normal checks after integration:

```sh
go test ./...
go vet ./...
```

## Completion checklist

Before considering an integration complete, confirm that:

- the code imports only the modules required by the selected path;
- structured outputs use concrete Go result types;
- every retry has a finite bound;
- detailed and streaming operations preserve partial results when relevant;
- Codex permissions, sandbox, workspace, model, and process lifetime are
  explicit;
- tests replace the narrow caller or runner interface, not generated protocol
  types;
- no application-owned facade duplicates `llmkit`, `codexcaller`, or
  `protocolv2` behavior;
- `go test ./...` and `go vet ./...` pass.

For deeper module-specific behavior, use the public module documentation:

- [`llmkit`](../llmkit/README.md)
- [`codexsdk`](../codexsdk/README.md)
- [`llmcaller/codex`](../llmcaller/codex/README.md)
