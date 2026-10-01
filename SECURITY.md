# Security policy

Security reports are accepted for the latest supported releases of
`github.com/ronhuafeng/llm-go`.

## Report privately

Use [GitHub private vulnerability reporting](https://github.com/ronhuafeng/llm-go/security/advisories/new).
Do not publish exploit details, credentials, transcripts, auth files, or private
paths in issues. Include the affected package family, module version, impact,
minimal reproduction, and safely shareable mitigation.

## Responsibility

In scope are defects caused by this repository, including process/stdio and
JSON-RPC handling, protocol encoding/decoding, generated representations,
application-decision handling, and credential or sensitive-data exposure by
repository automation.

The official Codex runtime, model quality, provider services, and provider
account/billing administration have their own owners. Their internal correctness
is not established by an llm-go live scenario. Report uncertain ownership
privately rather than publishing sensitive details.

## Live-test credentials

The accepted [live design](docs/live-codex.md) deliberately configures official
Codex directly with the configured Responses-compatible endpoint and credential.
Native Codex may read that key from its configured environment variable. No extra
local credential proxy or fork-contributor approval system is part of this
single-author setup.

This is a scoped credential decision, not permission to disclose secrets. Use
isolated test state; do not log keys, authorization headers, full environment
dumps, auth/config files containing secrets, or raw private transcripts. Never
upload an entire test `CODEX_HOME` as a diagnostic artifact.

The configured provider controls credential grants and billing policy.
Model-test credentials do not grant repository writes. Keep publication/integration
App credentials separate from code under test, and retain existing authority boundaries in unrelated Agent
and protocol-sync workflows.
