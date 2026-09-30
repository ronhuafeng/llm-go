# codexsdk

A Go SDK for driving an installed local Codex runtime through App Server.
This project is unofficial and experimental; it is not an OpenAI product.

```sh
go get github.com/ronhuafeng/llm-go@latest
```

## Runtime boundary

Install and configure the official `codex` executable before creating a client.
The SDK launches the configured command, normally
`codex app-server --listen stdio://`, and owns that child process and its stdio
connection. It does not install Codex, compile its Rust source, embed Core, or
reimplement the model/provider runtime.

`ClientOptions` supplies the working directory, command, initialization, and
application callbacks. Applications own credentials and execution policy.
Ordinary native Codex provider configuration is sufficient; the SDK does not
invent HTTP identity headers or require an llm-go proxy.

## Public API

`github.com/ronhuafeng/llm-go/codexsdk` provides client/process lifecycle,
generated typed facades, notifications, server requests, and `ThreadRunner`.
`github.com/ronhuafeng/llm-go/codexsdk/protocolv2` provides the generated App Server
v2 types and method registry.

`ThreadRunner.Start` and `Resume`, and their stream variants, compose thread
and turn operations. Direct facades expose Codex-specific operations such as
Fork. Applications provide admission decisions and server-request answers;
the SDK does not choose approval, permission, sandbox, user-input, or environment
policy for them.

Stream and result APIs preserve protocol observations and partial failure
information. In particular, final-answer absence and a present empty answer are
different facts. Do not infer a global non-empty-output requirement from an
individual live test that asks for a response.

Inbound newline-delimited JSON-RPC frames are limited to 16 MiB including the
newline delimiter.

## Compatibility and evidence

The generated surface follows the selected source in
[`baseline_metadata.json`](internal/protocolschema/appserver/v2/baseline_metadata.json).
The supported live runtime target is the exact official Codex release identified
there, not an independently selected latest or known-good version.

Generated API availability and live runtime coverage are different. Our live
guarantee is the behavior exercised and asserted by active integration
scenarios; untested combinations are not implied by the presence of a method.
No separate capability registry is maintained. Protocol-generation manifests
and coverage data remain generator inputs/projections, not a live guarantee set.

`GeneratedBaseline` and `Client.Provenance()` preserve source and runtime facts.
Initialization identity does not establish whole-surface compatibility;
`RuntimeCompatibilityUnknown` must not be turned into a positive verdict just
because a version matches or one scenario passes.

## Verification

Run owner-local deterministic checks with:

```sh
go vet ./codexsdk/...
go test -race ./codexsdk/...
```

The accepted real-runtime suite uses two integration stories rather than one
test per RPC. The composed structured call covers ordinary Start. The direct
thread story continues through persistent Start, Resume, Fork, and new work on
the fork. It does not test conversational recall or other Core-owned history
semantics.

See [`Live Codex integration`](../docs/live-codex.md) for the canonical test
environment, scenario assertions, and implementation status. Scenario retirement
must be explicit in the PR and changelog/release notes.

[`example_test.go`](example_test.go) is the compile-checked client setup example.
Use [`llmcaller/codex`](../llmcaller/codex) for provider-neutral typed results.
Protocol evolution is in [`protocol-sync.md`](../docs/protocol-sync.md);
acceptance is in [`verify.md`](../docs/verify.md); platform scope and reporting
are in [`SUPPORT.md`](../SUPPORT.md).
