# codexsdk

A concrete Go client for one local Codex App Server. This project is unofficial
and experimental; it is not an OpenAI product.

Public packages:

- `github.com/ronhuafeng/llm-go/codexsdk` for process/client lifecycle, generated
  facades, thread/turn execution, notifications, and server requests;
- `github.com/ronhuafeng/llm-go/codexsdk/protocolv2` for generated App Server v2
  protocol types and method registry.

Requires Go 1.23 or newer.

```sh
go get github.com/ronhuafeng/llm-go/codexsdk@latest
```

## Main API

`Client` launches and talks to a local `codex app-server`. Generated facades
expose protocol methods without requiring consumers to implement a large SDK
interface.

`ThreadRunner` / Exact Run APIs compose thread and turn lifecycle when callers
need notifications, partial results, or precise Codex details. Admission
callbacks are supplied by the application before model-directed continuation;
the SDK does not choose approval, permission, sandbox, or other application
policy.

Server requests are delivered as generated typed requests. The application
supplies the response value; the SDK does not invent approvals, user input, or
environment facts.

The generated API matches the checked-in protocol baseline. A successful live
request does not prove compatibility with every possible App Server version.

Inbound JSON-RPC frames are limited to 16 MiB including the newline delimiter.

## Examples, verification, and protocol upgrades

[`example_test.go`](example_test.go) is the compile-checked setup example.

```sh
GOWORK=off go test ./...
```

Protocol upgrades use [`../docs/protocol-sync.md`](../docs/protocol-sync.md).
Repository verification is in [`../docs/verify.md`](../docs/verify.md).
Changelog: [`CHANGELOG.md`](CHANGELOG.md).
