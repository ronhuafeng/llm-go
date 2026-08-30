# Security policy

## Supported versions

Security maintenance applies only to the latest supported releases of the
three modules published from this repository:

- `github.com/ronhuafeng/llm-go/llmkit`
- `github.com/ronhuafeng/llm-go/codexsdk`
- `github.com/ronhuafeng/llm-go/llmcaller/codex`

The archived legacy repositories and module paths are immutable migration
history and receive no feature or security fixes.

## Report a vulnerability privately

Do not open a public issue with exploit details, credentials, transcripts,
private paths, or other sensitive data. Use this repository's
[GitHub private vulnerability reporting](https://github.com/ronhuafeng/llm-go/security/advisories/new)
to send the report directly to the maintainers.

Include the affected module and version, impact, a minimal reproduction, and
any suggested mitigation that can be shared safely. Remove tokens, credentials,
private prompts, and unrelated user data before submitting. The maintainers
will coordinate validation, remediation, and disclosure through the private
advisory.

## Module notes

`codexsdk` is pre-1.0. In scope: transport, JSON-RPC, streams, typed protocol
encode/decode, generated schema mismatches, and checked-in secrets. Out of
scope: OpenAI services, the official Codex CLI, and model quality unrelated to
SDK handling.

The adapter does not read credentials or start processes. It uses a
consumer-owned `ThreadRunner`, normally from `codexsdk.Client.ThreadRunner()`.
Applications own authentication, the app-server command, the working directory,
and approval handling. For least privilege, pass the smallest practical working
directory and build options with `ReadOnlyEphemeralOptions(client.ThreadRunner())`
so the adapter sends exact `protocolv2` read-only sandbox and never-approve
values, then checks the Effective profile.
