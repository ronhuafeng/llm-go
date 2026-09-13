# codexsdk

A concrete Go client for one local Codex App Server. This project is unofficial
and experimental; it is not an OpenAI product.

Public packages:

- `github.com/ronhuafeng/llm-go/codexsdk` for process/client lifecycle, generated
  facades, thread/turn execution, notifications, and server requests;
- `github.com/ronhuafeng/llm-go/codexsdk/protocolv2` for generated App Server v2
  protocol types and method registry.

```sh
go get github.com/ronhuafeng/llm-go/codexsdk@latest
```

## Main API

`Client` launches and talks to a local `codex app-server`. Generated facades
expose protocol methods without requiring consumers to implement a large SDK
interface.

`ThreadRunner` / Exact Run APIs compose thread and turn lifecycle when callers
need notifications, partial results, or precise Codex details. Admission
callbacks and server-request answers come from the application; the SDK does not
choose approval, permission, sandbox, user input, or environment policy.

The generated API follows the checked-in protocol baseline. A successful live
request does not prove compatibility with every App Server version.

Inbound JSON-RPC frames are limited to 16 MiB including the newline delimiter.

## Example

[`example_test.go`](example_test.go) is the compile-checked setup example.

```sh
GOWORK=off go test ./...
```

Protocol upgrades: [`../docs/protocol-sync.md`](../docs/protocol-sync.md).
Supported Go/platform versions: [`../SUPPORT.md`](../SUPPORT.md).
Repository verification: [`../docs/verify.md`](../docs/verify.md).
Changelog: [`CHANGELOG.md`](CHANGELOG.md).
