# Use a concrete root client with classified generated compatibility

Issue #44 selects a pre-v1 boundary in which `New` constructs an exported
concrete `*codexsdk.Client`. The concrete root owns app-server lifecycle,
exposes the handwritten exact `ThreadRunner`, and preserves access to every
generated facade. Applications define narrow interfaces where they consume
those capabilities. This removes the false compatibility coupling created by
the current SDK-owned `Client` interface embedding the growing generated
`SDKSurface`.

## Status

Accepted; the concrete-root portion is implemented by issue #62 after v0.2.1.
Generated stability manifest extension is implemented. Issue #89 completes the
same ownership rule for generated facades in v0.4.0: facade
accessors return exported concrete opaque values, not SDK-owned interfaces.

## Decision

- `New(ClientOptions) (*Client, error)` is the only constructor. Do not add a
  parallel SDK-owned umbrella interface, `NewClient`, or alternate constructor
  syntax sugar.
- A successfully constructed root owns its subprocess, transport, callback
  admission, generated facades, and exact thread lifecycle. Its owner must call
  `Close`. `Close` remains idempotent and concurrency-safe and continues to
  report the root's first terminal failure where the lifecycle contract
  requires it.
- The zero value of `Client` is not a connected client. It is safe but inert:
  `Close` succeeds and operations fail with `ErrClientClosed`. Valid connected
  clients come only from `New`; methods must not lazily start a process.
- Handwritten lifecycle APIs may compose exact generated operations and retain
  evidence, but must not copy generated facts into handwritten models.
  Generated facades remain the exact, complete protocol escape hatch.
- Each generated facade is an exported concrete value with private
  representation. Adding a generated method expands capability without
  enlarging an interface implemented by applications. Applications define
  narrow interfaces at their consuming seams.
- Consumers mock only the narrow operation or lifecycle interface they use.
  They are never expected to implement `SDKSurface` or another SDK-owned root
  interface.

## Generated compatibility policy

The checked-in classified protocol manifest is the single source of truth for
stable versus experimental generated surface. Its current method
classification is derived by comparing schema generation with and without
`--experimental`. Follow-up tooling must extend that same manifest model to
classify every exported generated declaration and compatibility-relevant
member, including fields, enum values, and union variants. A declaration or
member reachable from the non-experimental schema is stable; one present only
in the experimental schema is experimental. Mixed types retain stable
protection for their stable members while experimental-only members remain
experimental. "Stable" records schema visibility, not an upstream lifetime
guarantee.

Before v1, classification guides migration, support expectations, and promotion
from experimental to stable. At v1, every exported generated name in this Go
module receives normal source-compatibility protection, regardless of its
stability metadata. Generated drift that would remove or incompatibly change
an exported name requires an additive compatibility path that remains honest
about current upstream support, or the next SDK major version. Release notes
must identify affected classifications and give migration instructions.

Runtime opt-in and compile-time compatibility are separate. Setting
`Initialize.Capabilities.ExperimentalAPI` permits experimental calls and
fields at runtime; it neither changes what Go declarations are compiled nor
removes exported names from the effective Go surface. The default generated
package remains complete, so exact protocol users do not need a second package,
module, build tag, or provider-neutral abstraction. A separately versioned
experimental module would be the clean way to permit post-v1 minor-release
source breakage, but its extra generation, dependency, and shared-type boundary
is rejected until concrete consumer demand justifies it.

## Migration timing

This decision changes no production Go behavior or export. The concrete-root
migration must be a later bounded issue, no earlier than v0.3 and before the v1
freeze. Because the existing interface already owns the name `Client`, the
change happens in one documented pre-v1 breaking release rather than through a
parallel umbrella interface. Inferred local variables generally remain source
compatible; stored `codexsdk.Client` values move to `*codexsdk.Client`, and
test doubles move to consumer-owned interfaces.

The implementation release must update the public API inventory, migration
guide, changelog, package documentation, and clean-consumer compile evidence
together. Issue #45 is closed because its design scope was folded into #44; it
must not be reopened or repurposed. Concrete-root implementation and manifest
extension/enforcement must be proposed as new bounded issues after #44. Neither
follow-up may implement provider-neutral abstractions, a workflow DSL, or
handwritten copies of generated facts.

## Considered options

Keeping the current `Client`/`SDKSurface` interface would make every generated
facade addition enlarge an SDK-owned mock contract. Splitting SDK-owned stable
and experimental root interfaces would retain that coupling and make mixed
generated types ambiguous. A separately versioned experimental module would
create a valid SemVer boundary but would complicate generation, dependencies,
and shared types without demonstrated demand. Merely documenting an unstable
subset inside the v1 module was rejected because exported Go names remain part
of consumers' effective source surface.
