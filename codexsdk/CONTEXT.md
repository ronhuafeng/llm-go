# Codex SDK

Exact transport, generated protocol access, and thread/turn lifecycle for one
local Codex app-server.

## Language

**Root Client**:
The application-owned connection to one locally launched Codex app-server.
_Avoid_: SDK interface, service container

**Lifecycle API**:
A handwritten operation that owns process or thread/turn lifecycle while
keeping generated protocol facts exact.
_Avoid_: workflow API, convenience DSL

**Generated Facade**:
A protocol-derived concrete opaque value on the Root Client. Generated method
growth adds capability without enlarging an application-implemented interface.
_Avoid_: service abstraction, provider API

**Classified Generated Surface**:
Exported generated declarations classified by stable versus experimental schema
visibility. A type may mix both classes; each member keeps its own class.
_Avoid_: API allowlist, handwritten inventory, frozen protocol, private API

**Wire Message Role**:
The protocol position of a JSON value. It owns unknown-member acceptance,
independent of generated type reuse.
_Avoid_: type strictness, experimental decode mode

**Server Observation**:
A known RPC result or server notification authored by the app-server. Known
members stay exact; additional unknown members do not invalidate it.
_Avoid_: permissive payload, server request

**Action-Bearing Message**:
A client-authored instruction or response, or an app-server request that can
cause or authorize action. Admission fails closed.
_Avoid_: Server Observation, strict generated type

**Additional Wire Member**:
An object member absent from the checked-in protocol baseline. It is not an
unknown method, enum value, union variant, or malformed known member.
_Avoid_: unknown protocol meaning, unvalidated field

**Consumer-Owned Interface**:
A narrow interface declared by an application where it consumes a Lifecycle
API or Generated Facade.
_Avoid_: SDK umbrella interface

**Exact Run**:
One composed thread/turn execution with its ordered attributable protocol
evidence, partial result, and stable terminal cause.
_Avoid_: workflow, request

**Exact Run Waiter**:
An independent observer of an Exact Run's completion and immutable result.
Observation does not destroy the run or its history.
_Avoid_: subscriber, stream consumer

**Exact Run History Cursor**:
The per-stream position over the immutable notification history already owned
by an Exact Run.
_Avoid_: delivery queue, event store

**Shared Run Cancellation**:
The lifecycle boundary that terminates an Exact Run for every observer.
_Avoid_: waiter cancellation, timeout
