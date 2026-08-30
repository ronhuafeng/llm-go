# Security policy

Security maintenance applies only to the latest supported releases of:

- `github.com/ronhuafeng/llm-go/llmkit`
- `github.com/ronhuafeng/llm-go/codexsdk`
- `github.com/ronhuafeng/llm-go/llmcaller/codex`

Do not open a public issue with exploit details, credentials, transcripts,
private paths, or other sensitive data. Use
[GitHub private vulnerability reporting](https://github.com/ronhuafeng/llm-go/security/advisories/new).

Include the affected module and version, impact, a minimal reproduction, and
any mitigation that can be shared safely.

`codexsdk` is pre-1.0. In scope: transport, JSON-RPC, streams, typed protocol
encode/decode, generated schema mismatches, and checked-in secrets. Out of
scope: OpenAI services, the official Codex CLI, and model quality unrelated to
SDK handling.

The adapter does not read credentials or start processes. Applications own
authentication, the app-server command, the working directory, and approval
handling. See the adapter README for the named read-only profile.
