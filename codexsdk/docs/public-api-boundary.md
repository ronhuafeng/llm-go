# Pre-v1 Public API Boundary

The accepted target for issue #44 is recorded in
[ADR 0001](adr/0001-pre-v1-public-api-boundary.md). This page translates the
decision into consumer guidance and migration consequences. The concrete-root
root boundary is implemented after v0.2.1. Concrete generated facade ownership
is implemented in v0.4.0.

## Target shape

The root client is a concrete `*codexsdk.Client` created only by
`codexsdk.New`. It owns `Close`, exact thread/turn lifecycle,
typed callbacks, and access to all generated facades. Generated additions then
add methods to a concrete type instead of enlarging an interface every SDK
consumer may have implemented.

Every facade accessor returns an exported concrete value with no exported
fields. The value is opaque but its complete generated method set remains
available. Consumers name only their own narrow interfaces; the SDK does not
publish parallel facade interfaces.

The SDK will not publish a replacement umbrella interface. Declare interfaces
at the consuming seam instead:

```go
type RunStarter interface {
	Start(context.Context, codexsdk.StartThreadRunRequest) (codexsdk.StartedThreadRun, error)
}

func startRun(ctx context.Context, runner RunStarter, req codexsdk.StartThreadRunRequest) error {
	_, err := runner.Start(ctx, req)
	return err
}
```

A facade consumer follows the same rule:

```go
type ModelLister interface {
	List(context.Context, protocolv2.ModelListParams) (protocolv2.ModelListResponse, error)
}

func listModels(ctx context.Context, models ModelLister) error {
	_, err := models.List(ctx, protocolv2.ModelListParams{})
	return err
}
```

The repository compile-checks these patterns without SDK-owned root or facade
interfaces. Consumer fakes implement `ModelLister`, not `codexsdk.Models`.

The deprecated v0.1 projected lifecycle and copied protocol compatibility
types are not part of this boundary. `ThreadRunner` is the only handwritten
thread/turn lifecycle API; generated facades and `protocolv2` are the factual
operation and model source.

## Construction and ownership

`New` requires `ClientOptions` and starts and initializes one app-server
connection. On success, the caller owns the returned root and must call
`Close`. The zero value is deliberately inert, not an alternate construction
path: it does not launch a process, `Close` is safe, and operations return
`ErrClientClosed`.

No `NewClient`, default global, lazy connection, or constructor wrapper is part
of the target. Applications may wrap construction when they own additional
policy, but that policy does not belong to this SDK.

## Generated stable and experimental surface

`protocolv2` and the exact facades remain complete and colocated. The checked-in
classified manifest owns the stable/experimental fact; documentation and
release reports consume it instead of maintaining handwritten lists.

- Before v1, classification guides migration, promotion, documentation, and
  support expectations.
- At v1, every exported generated name in this module receives normal source-
  compatibility protection, including names classified experimental.
- Generated drift that would remove or incompatibly change an exported name
  requires an honest additive compatibility path or the next major version.
- Runtime `ExperimentalAPI` capability opt-in does not alter compile-time
  availability or compatibility classification.
- A separately versioned experimental module is the only clean boundary for
  post-v1 minor-release source breakage, and is rejected until concrete demand
  justifies its additional complexity.

The manifest classifies methods plus every exported generated declaration and
compatibility-relevant member. Sync derives the surface by comparing generation
without and with experimental schema visibility, and release evidence reports
impact by classification. Missing classifications fail validation. Issue #45
remains closed because its design was folded into #44.

## Migration window

The concrete-root change is implemented after v0.2.1. Concrete facade values
replace the remaining generated facade interfaces in v0.4.0 as a documented
pre-v1 breaking change. Code, migration guidance,
compatibility reporting, changelog, package documentation, and clean-consumer
evidence move together.

Typical construction using type inference is unchanged in shape:

```go
root, err := codexsdk.New(options)
```

Explicit storage changes from `codexsdk.Client` to `*codexsdk.Client`. Tests
and adapters should replace broad SDK-root mocks with the narrow interfaces
their code actually consumes. There will be no parallel SDK-owned interface or
alternate constructor transition period.
