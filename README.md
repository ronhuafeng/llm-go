# llm-go

Typed Go modules for structured LLM calls and exact Codex app-server control.

| Module | Use it to | Import path |
| --- | --- | --- |
| [`llmkit`](llmkit) | Generate JSON Schema from Go types, decode and validate structured output, and run bounded retries without a provider SDK. | `github.com/ronhuafeng/llm-go/llmkit` |
| [`codexsdk`](codexsdk) | Control a local Codex app-server through generated protocol types and typed thread/turn APIs. | `github.com/ronhuafeng/llm-go/codexsdk` |
| [`llmcaller/codex`](llmcaller/codex) | Use Codex as an `llmkit` caller while keeping the complete SDK result. | `github.com/ronhuafeng/llm-go/llmcaller/codex` |

Start with the README for the module matching your use case.
Applications must not import `internal/tools`.

Destinations: [`NORTHSTAR.md`](NORTHSTAR.md).
Maintainers: [`AGENTS.md`](AGENTS.md), [`CONTRIBUTING.md`](CONTRIBUTING.md).
Vulnerabilities: [`SECURITY.md`](SECURITY.md).
