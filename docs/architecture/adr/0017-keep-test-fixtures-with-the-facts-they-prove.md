---
status: accepted
---

# Keep test fixtures with the facts they prove

Tests, helpers, fakes, and fixtures live with the module-level facts they prove:

```text
llmkit tests
  -> neutral schema, decode, validation, retry, and toolkit evidence

codexsdk tests
  -> transport, protocol, lifecycle, streaming, history, and exact evidence

llmcaller/codex tests
  -> Codex schema policy, effective profile, and evidence projection

internal/tools integration
  -> black-box three-layer canary, clean consumer, release, and proxy artifact
```

The repository does not add a `testutil`, `sharedtest`, or root fixture package
that public modules import. Similar helpers may remain duplicated when they
serve different semantic assertions.

The adapter may define fakes against consumer-owned narrow SDK interfaces. The
SDK does not publish adapter-specific testing abstractions merely to remove test
code from the adapter.

Cross-module fixtures belong in `internal/tools` only when the test genuinely
observes the composed public system. Such tests import public APIs and cannot
inspect module-internal or private layout.

Module public-behavior tests remain the owners of module semantics. Integration
canaries prove composition and evidence preservation; they do not redefine the
individual module contracts.

## Consequences

- Test convenience cannot introduce a hidden fourth dependency join.
- Internal refactors remain private to their owning module's tests.
- Cross-module canaries resemble real external consumers.
- Some small fake and fixture implementations may intentionally repeat.
- A failing test identifies whether the broken fact is module-local or a
  composition contract.

## Considered options

A shared repository-wide test utility package was rejected because public
modules would gain a new cross-boundary dependency even if it were used only in
tests.

Publishing SDK test helpers for adapter behavior was rejected because it would
make the SDK own a consumer's policy and fixture model.

Moving all tests to the root was rejected because integration tests cannot
replace the module-local public behavior facts required for independent
releases.
