# llmstep

`llmstep` runs one provider-neutral typed structured-output LLM step:

```text
render prompt -> llmadapter.ValueDetailed[O] -> validate typed output -> retry with safe feedback
```

Use `llmstep` when a single typed LLM call needs deterministic validation and
bounded feedback retries.

Use `llmadapter.Value` when only the typed projection is needed, and
`llmadapter.ValueDetailed` when the provider response evidence must be retained.

Use `settle.Run` directly when retry state already lives in your operation and
you do not need structured validation feedback passed into prompt rendering.

`RunDetailed` preserves every validator decision in `Attempt.Validation`.
For an unsettled attempt, it creates sanitized `Attempt.RetryFeedback` only
when another attempt will run. The final unsettled attempt therefore returns
`settle.ErrUnsettled` without invoking the sanitizer and without synthesizing
retry feedback.

The package does not include provider transport, prompt templates, business
validators, tool calling, streaming, write gates, or multi-step orchestration.
