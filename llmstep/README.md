# llmstep

`llmstep` runs one provider-neutral typed structured-output LLM step:

```text
render prompt -> llmadapter.Value[O] -> validate typed output -> retry with safe feedback
```

Use `llmstep` when a single typed LLM call needs deterministic validation and
bounded feedback retries.

Use `llmadapter.Value` directly when one typed provider-neutral call is enough
and there is no validation feedback loop.

Use `settle.Run` directly when retry state already lives in your operation and
you do not need structured validation feedback passed into prompt rendering.

The package does not include provider transport, prompt templates, business
validators, tool calling, streaming, write gates, or multi-step orchestration.
