# llm-go

Independent Go modules for typed LLM calls and a local Codex App Server client.

| Module | Use it for | Import path |
| --- | --- | --- |
| [`llmkit`](llmkit) | Provider-neutral typed output, validation, and bounded retries. | `github.com/ronhuafeng/llm-go/llmkit` |
| [`codexsdk`](codexsdk) | Direct local Codex App Server JSON-RPC and thread/turn lifecycle. | `github.com/ronhuafeng/llm-go/codexsdk` |
| [`llmcaller/codex`](llmcaller/codex) | Use Codex behind the `llmkit` caller API. | `github.com/ronhuafeng/llm-go/llmcaller/codex` |

Start with the README for the module you use. Public behavior is defined by the
Go API, package documentation, and tests.

Maintainers: [`AGENTS.md`](AGENTS.md), [`CONTRIBUTING.md`](CONTRIBUTING.md).
Engineering rules: [`NORTHSTAR.md`](NORTHSTAR.md).
Operations: [`docs/verify.md`](docs/verify.md),
[`docs/release.md`](docs/release.md),
[`docs/protocol-sync.md`](docs/protocol-sync.md).
Support: [`SUPPORT.md`](SUPPORT.md). Security: [`SECURITY.md`](SECURITY.md).
