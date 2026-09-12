# codexsdk

Exact control of one local Codex app-server. Destination:
[NORTHSTAR.md](../NORTHSTAR.md). Language: [CONTEXT.md](CONTEXT.md).

This project is unofficial and experimental; it is not an OpenAI product.

Public import paths:

- `github.com/ronhuafeng/llm-go/codexsdk` — client lifecycle, generated typed
  facades, Exact Run, notification streaming, and server-request handling.
- `github.com/ronhuafeng/llm-go/codexsdk/protocolv2` — generated app-server v2
  params, responses, notifications, enums, unions, JSON helpers, and method
  registry.

Requires Go 1.23 or newer. OS support and testing tiers are in
[SUPPORT.md](../SUPPORT.md).

```sh
go get github.com/ronhuafeng/llm-go/codexsdk@latest
```

## Executable example

[`example_test.go`](example_test.go) is the canonical compile-checked client
setup. It shows a locally launched `codex app-server` without making ordinary
unit tests depend on a real CLI or credential.

```sh
GOWORK=off go test ./...
```

Use Exact Run when provider facts matter. `ThreadRunner` preserves decoded
thread-start facts, turn state, notifications, usage, diagnostics, final text,
and partial observation on failure. Admission after decoded `thread/start` or `thread/resume` and before
`turn/start` is consumer-supplied and policy-neutral: the callback inspects
the observation and the exact pending turn request, then rejecting admission
preserves the exact partial run and does not send `turn/start`.

The generated protocol and app-server are the factual authority. The SDK does
not translate Codex facts into provider-neutral LLM semantics and does not own
application judgment or effect authority.

**Exact** means exact to the checked-in [Generated Baseline
Provenance](CONTEXT.md), not "proven compatible with whatever app-server is
running." `initialize` can preserve a [Runtime App-Server
Observation](CONTEXT.md); current protocol fields do not prove [Runtime
Compatibility](CONTEXT.md). [Connection Provenance](CONTEXT.md) keeps those
facts separate. A successful request, turn, or live smoke stays an
observation of the exercised path. It does not upgrade the generated
surface to a compatibility fact.

Inbound app-server JSON-RPC frames are limited to 16 MiB including the newline
delimiter. Oversized or unterminated frames fail the client with sanitized
byte-count/hash diagnostics.

Generator and protocol-sync rules stay owner-local. See
[`Agents.test.md`](Agents.test.md) for SDK test design and the repository
[`codexsdk-sync-upstream`](../.agents/skills/codexsdk-sync-upstream/SKILL.md)
skill for protocol baseline updates.

Changelog: [CHANGELOG.md](CHANGELOG.md). License: [LICENSE](LICENSE).
