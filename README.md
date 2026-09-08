# llm-go

Go modules for typed, evidence-preserving LLM inference and exact Codex
app-server control.

| Module | Use it to | Import path |
| --- | --- | --- |
| [`llmkit`](llmkit) | Constrain model output to typed Go values, validate propositions, preserve call and attempt evidence, and run bounded retries without a provider SDK. | `github.com/ronhuafeng/llm-go/llmkit` |
| [`codexsdk`](codexsdk) | Control one local Codex app-server through generated protocol types and exact thread/turn lifecycle APIs. | `github.com/ronhuafeng/llm-go/codexsdk` |
| [`llmcaller/codex`](llmcaller/codex) | Use exact Codex execution behind the `llmkit` caller contract while retaining typed Codex details. | `github.com/ronhuafeng/llm-go/llmcaller/codex` |

Start with the README for the module matching the current use case.

The repository-level authority-to-effect example continues past
`llmstep` judgment: accepted output still needs application-owned
authority, and authorization is not execution success. See
[`internal/tools/integration/example_authority_to_effect_test.go`](internal/tools/integration/example_authority_to_effect_test.go).

Semantics: [`NORTHSTAR.md`](NORTHSTAR.md).
Live invariants: [`DESIGN.md`](DESIGN.md).
Maintainers: [`AGENTS.md`](AGENTS.md), [`CONTRIBUTING.md`](CONTRIBUTING.md).
Vulnerabilities: [`SECURITY.md`](SECURITY.md).
