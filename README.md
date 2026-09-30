# llm-go

One Go module for typed LLM calls and for driving an installed Codex runtime
from Go. This project is unofficial; it is not an OpenAI product.

```sh
go get github.com/ronhuafeng/llm-go@latest
```

## Choose the public entry point

| Package family | Responsibility |
| --- | --- |
| [`llmkit`](llmkit) | Provider-neutral typed calls, schema handling, validation, and application-controlled retries. |
| [`codexsdk`](codexsdk) | A Go SDK for a local Codex runtime, using App Server for process, thread, turn, notification, and server-request lifecycle. |
| [`llmcaller/codex`](llmcaller/codex) | The meaning-preserving bridge from the `llmkit` caller API to `codexsdk`. |

Applications that need provider-neutral structured results use the composed
`llmkit` / `llmcaller/codex` path. Applications that need Codex-specific lifecycle
use `codexsdk` directly. There is no additional runtime package at the repository
root, and the families are not separately versioned Go modules.

## Codex is an external runtime

The SDK assumes that the official `codex` executable is already installed.
It launches `codex app-server` and communicates over local stdio; it does not
install Codex, embed Codex Core, or own Core's internal behavior. Applications
own configuration, credentials, approval, sandbox, and side-effect policy.

The selected protocol source is recorded in
[`baseline_metadata.json`](codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json).
The runtime compatibility target is that exact synced Codex release, not
whatever `latest` resolves to. A generated method or type is not evidence that
all of its runtime behavior has been exercised.

## What live verification means

Our live guarantee is scenario-based: realistic integration scenarios and their
assertions define the behaviors actually covered against the selected runtime.
It is not an exhaustive App Server compatibility claim or a separate capability
matrix. Scenarios may be added or explicitly retired as the project evolves.

The accepted first suite has two stories: a structured composed call, and a
persistent thread continued through Resume and Fork. Both use an official
installed Codex, a direct Mini connection, and one real model. Read
[`Live Codex integration`](docs/live-codex.md) for the design, assertions,
configuration, and the explicit implementation transition. The runtime/gate implementation and the remaining scenario transition are
recorded there; actual workflow results own execution evidence.

Deterministic Go, race, and generated-protocol tests remain necessary; live
integration does not replace them.

## Repository guides

Start with the package README and its executable examples. Engineering ownership
is in [`NORTHSTAR.md`](NORTHSTAR.md). Contributor instructions are in
[`CONTRIBUTING.md`](CONTRIBUTING.md) and [`AGENTS.md`](AGENTS.md).

[`Verification`](docs/verify.md) owns acceptance;
[`protocol sync`](docs/protocol-sync.md) owns schema evolution;
[`auto-forward`](docs/auto-forward.md) owns integration;
[`release`](docs/release.md) owns release boundaries.
See [`SUPPORT.md`](SUPPORT.md), [`SECURITY.md`](SECURITY.md), and
[`CHANGELOG.md`](CHANGELOG.md) for support, reporting, and changes.
