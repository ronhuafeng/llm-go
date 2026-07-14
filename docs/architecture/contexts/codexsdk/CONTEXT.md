# Codex SDK Public Boundary

The compatibility boundaries between the SDK, the generated Codex protocol,
and applications that consume them.

## Language

**Root Client**:
The application-owned connection to one locally launched Codex app-server,
including its lifecycle and access to exact protocol operations.
_Avoid_: SDK interface, service container

**Lifecycle API**:
A handwritten operation that owns app-server process or thread/turn lifecycle
while preserving exact generated protocol facts.
_Avoid_: Workflow API, convenience DSL

**Generated Facade**:
A protocol-derived exported concrete opaque value exposed by the Root Client.
It carries a group of exact Codex app-server operations.
_Avoid_: Service abstraction, provider API

**Consumer-Owned Interface**:
A narrow interface declared by an application at the point where it consumes a
Lifecycle API or Generated Facade.
_Avoid_: SDK umbrella interface

**Exact Run**:
One composed thread/turn execution together with its ordered attributable
protocol evidence, partial result, and stable terminal cause.
_Avoid_: Workflow, request

**Exact Run Waiter**:
An independent, non-destructive observer of an Exact Run's completion and
isolated result snapshot.
_Avoid_: Subscriber, stream consumer

**Exact Run History Cursor**:
The per-stream position used to observe immutable ordered notification history
already owned by an Exact Run.
_Avoid_: Delivery queue, event store
