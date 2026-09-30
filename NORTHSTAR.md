# North stars

`llm-go` is a Go library. Prefer plain Go, direct observations, and executable
scenarios over project-specific frameworks or parallel sources of truth.

> Models propose. Programs decide. Effects require authority.

## Keep the runtime boundary clear

`codexsdk` drives an already installed official Codex executable through its
local App Server interface. App Server is the integration boundary to Codex
Core, not a reason to reproduce Core or its upstream test suite in Go.
The SDK does not install, build, embed, or certify Codex Core.

This does not make the generated public protocol types private. They remain
useful exact Go representations. It separates their wire contract from the
runtime scenarios for which the project has live evidence.

The repository has three package families in one Go module:

```text
llmkit       ----\
                 +--> llmcaller/codex
codexsdk     ----/
```

`llmkit` owns provider-neutral typed inference. `codexsdk` owns process/stdio,
protocol representation, and the Go-facing thread/turn lifecycle.
`llmcaller/codex` owns translation. The first two do not depend on one another;
the root is not another runtime package. Keep one module, one Go floor, and one
SemVer. Do not recreate sibling modules with workspaces or committed `replace`
or `exclude` directives.

## Preserve facts and meaning

Report only what was observed. Requested configuration, a model name, an
adapter name, or a successful call is not proof of the serving model, provider,
or whole-surface compatibility. Keep generated-baseline provenance, runtime
identity, and runtime compatibility separate.

Preserve absent versus present-empty output, omitted versus null fields, exact
request/response identity, and total versus per-request usage. Errors may retain
partial results without presenting them as accepted output.

The selected upstream schema owns methods, field sets, requiredness,
nullability, references, and other wire facts. Derive lossless Go mappings
mechanically. New field paths and type names do not require a second admission
catalogue merely because they are new. Unconstrained JSON is represented by
protocol-native `JSONValue` when that preserves the entire wire value.

Use handwritten semantic overlays only for a current local invariant the schema
cannot express, such as application authority or lifecycle correlation. Keep
focused proof with the owner. Unrepresentable meaning fails before publication;
do not drop fields, use lossy passthroughs, or weaken validation to advance a
baseline. The adapter must not silently narrow or broaden a caller's schema.

## Applications own decisions

Model output is data. Go code validates and accepts it. Decoding alone is not
validation, and a model's final message is never verification authority.
Applications decide whether to retry and what feedback to disclose.

SDK callbacks expose mechanisms; applications decide approval, permissions,
sandboxing, disclosure, and external effects. `codexsdk` forwards a server
request and encodes the supplied response instead of inventing a decision.
Prompt text is not authority. Test fixtures likewise choose their own policy;
a fixture's settings do not become new SDK defaults.

## Test our integration, not Codex itself

Use deterministic tests for Go behavior, framing, correlation, ordering, race
conditions, and failure mechanics. Use exact schema reconstruction and generated
reproducibility for protocol fidelity.

Use real integration scenarios to verify the public paths through installed
Codex and a real model. The live guarantee follows duck semantics: the active
scenarios and assertions are its source of truth. Do not mechanically create one
test per RPC, mirror upstream Rust tests, or maintain another capability registry.
Where the composed path covers a behavior, avoid a redundant direct-path test.

Check the behavior the SDK owns. Resume and Fork must permit new work to
complete on the resulting thread; tests need not establish conversational
memory fidelity. Most responses need no exact prose assertion. A scenario
specifically about structured output should validate its schema and a simple
closed result, such as an integer arithmetic answer.

[`Live Codex integration`](docs/live-codex.md) owns the accepted suite and its
implementation transition. Its fixed runtime is derived from the synced
baseline. Its live fixture is Linux, direct Mini configuration, and one selected
model, not a platform/model/latest-version matrix.

Adding a live scenario expands the evidenced behavior. Retiring or materially
weakening one reduces it and requires an explicit PR explanation and changelog /
release note. Renaming or consolidating tests without losing assertions is not
retirement. A failure is not permission to silently reduce the guarantee.

## Keep acceptance proportional

Go owns behavioral checks and generation; GitHub Actions orchestrates them.
Codex-affecting changes require the real live path in addition to the existing
native proofs. Unrelated changes should not spend a model call. Neither a live
PASS nor schema regeneration replaces the other proof.

The accepted live design uses native Codex URL/key configuration directly to
Mini. Do not add an llm-go credential proxy, impersonation headers, provider
retry controller, approval service, or proof ledger around that path. Basic
secret hygiene and existing separation of repository-write authority still
apply. The live runner does not retry failed scenarios automatically.

Preserve the existing exact-head acceptance and non-force auto-forward model;
do not manufacture another candidate or acceptance authority. Documentation of
a desired gate must distinguish it from deployed enforcement.

Every abstraction, helper, document, and workflow needs a current consumer or
invariant. Remove retired machinery rather than retaining compatibility scaffolding
without a use. When two designs preserve the same behavior, choose the one that
requires less context to understand, change, and verify.
