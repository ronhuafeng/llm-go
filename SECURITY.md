# Security policy

Security reports are accepted for the latest supported releases of:

- `github.com/ronhuafeng/llm-go/llmkit`
- `github.com/ronhuafeng/llm-go/codexsdk`
- `github.com/ronhuafeng/llm-go/llmcaller/codex`

## Report privately

Do not open a public issue with exploit details, credentials, transcripts,
private paths, or other sensitive data. Use
[GitHub private vulnerability reporting](https://github.com/ronhuafeng/llm-go/security/advisories/new).

Include the affected module and version, impact, a minimal reproduction, and any
mitigation that can be shared safely.

## Scope

In scope are vulnerabilities caused by this repository's code, including
transport/JSON-RPC handling, protocol encode/decode, generated protocol handling,
and accidental credential or sensitive-data exposure by repository automation.

OpenAI services, the official Codex CLI, and model quality unrelated to this
repository's handling are out of scope. If ownership is unclear, report the issue
privately rather than publishing exploit details.
