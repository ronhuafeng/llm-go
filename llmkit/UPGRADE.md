# Upgrade llmkit

Current module: `github.com/ronhuafeng/llm-go/llmkit`.
Install the current tag with `go get github.com/ronhuafeng/llm-go/llmkit@v0.7.0`.

## From `llmkit-go` v0.5.0 to `llmkit` v0.6.0

Replace the module requirement `github.com/ronhuafeng/llmkit-go` with
`github.com/ronhuafeng/llm-go/llmkit`. Replace public imports:

| Legacy import | Current import |
| --- | --- |
| `github.com/ronhuafeng/llmkit-go/llmadapter` | `github.com/ronhuafeng/llm-go/llmkit/llmadapter` |
| `github.com/ronhuafeng/llmkit-go/llmschema` | `github.com/ronhuafeng/llm-go/llmkit/llmschema` |
| `github.com/ronhuafeng/llmkit-go/llmstep` | `github.com/ronhuafeng/llm-go/llmkit/llmstep` |
| `github.com/ronhuafeng/llmkit-go/settle` | `github.com/ronhuafeng/llm-go/llmkit/settle` |

```sh
go get github.com/ronhuafeng/llm-go/llmkit@v0.6.0
go mod tidy
```

The import path is the only intended breaking surface relative to legacy
`v0.5.0`. There are no forwarding packages.

## From v0.6.0 to v0.7.0

The default `llmstep.StrictFeedbackSanitizer` no longer accepts free-form
`Feedback.Summary` values. When `Step.Sanitizer` is nil, a non-empty summary
on an attempt that can retry returns a sanitize-stage error wrapping
`llmstep.ErrUnsafeFeedback`. Identifier-oriented `Codes` and `Locations`
remain accepted. Final unsettled attempts do not invoke the sanitizer.

Applications that used default summaries must replace them with already-redacted
codes and locations, or configure an explicit custom sanitizer. Custom
sanitizers own that disclosure decision. `Attempt.Validation` remains the
exact validator-owned decision.
