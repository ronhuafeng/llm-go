---
status: accepted
---

# Let wire message roles own unknown-member acceptance

The Codex app-server protocol evolves independently of an installed SDK schema
baseline. Codex CLI 0.145.0 demonstrates why schema visibility cannot be treated
as a wire filter: `thread/read` is visible in the non-experimental schema, while
its experimental-only `Thread.canAcceptDirectInput` member is emitted when
`experimentalApi` is omitted, false, or true. The SDK currently rejects unknown
members in every generated object, so one additional unconsumed member can hide
all otherwise valid response evidence from a consumer.

## Decision

Unknown-member acceptance belongs to the root Wire Message Role. It does not
belong to a reusable generated type or to stable-versus-experimental schema
visibility.

- A Server Observation is an RPC result or server notification for a known
  method. Its complete nested object graph accepts and discards Additional Wire
  Members while preserving every known member.
- An Action-Bearing Message is closed at its protocol admission boundary.
  Client-authored requests, notifications, and server-request responses cannot
  acquire unknown members through their typed values; any raw decoding of them
  rejects Additional Wire Members. App-server requests also reject them and
  retain the existing fail-closed response and client-failure semantics.
- An unrecognized server notification method is ignored because it is a new
  standalone observation that cannot be represented honestly as a typed
  notification. It is not published as exact-run evidence or delivered to the
  typed global handler. An unrecognized app-server request remains fail-closed.
- Every known contract remains exact in every role: object shape, duplicate
  keys, required members, nullability, scalar types, enum values, union
  discriminators, and union variants are still validated. An Additional Wire
  Member is the only tolerated unknown protocol shape.

The generator must produce role-aware message-root codecs and recursively carry
the root role through nested objects, collections, nullable values, and union
payloads. Generated value types remain a shared protocol model; they do not gain
fixed request-versus-response strictness merely because one schema happens to
reference them. Standalone value unmarshalling is not a wire admission boundary
and must not be used to preserve the legacy all-types-closed implementation.

Wire Message Role is manifest and generator input, not a public decoder option.
The existing typed facades and lifecycle interface remain the external seam;
transport routing selects the generated root codec internally. This keeps the
role-aware decoder a deep module: callers receive the compatibility behavior
without learning its recursive policy or coordinating it at each call site.

The implementation must contain no field-name exception, including for
`canAcceptDirectInput`, no runtime schema bundle, and no public or private bag of
discarded raw members. Once a later schema baseline models an additional member,
normal generated decoding and its stable/experimental classification apply.

Stable and experimental classification continues to own generated-surface
support expectations, outbound experimental opt-in guards, synchronization,
and release reporting. It never decides whether an unmodeled inbound member is
accepted. Consumers remain responsible for validating the known evidence their
own operation requires; unrelated wire members cannot become consumer gates.

## Consequences

An older SDK continues to expose known evidence when the app-server adds object
members, including members emitted outside their documented experimental gate.
Ignored members are unavailable until the checked-in schema and generated
surface catch up; this is honest absence, not an untyped escape hatch. New or
changed protocol meaning still fails rather than being guessed.

Focused generator tests must prove that one shared nested type is open beneath a
Server Observation and closed beneath an Action-Bearing Message. Transport tests
must cover additional top-level and nested observation members, malformed known
members, unknown notification methods, and fail-closed server requests. A real
field such as `canAcceptDirectInput` is a regression fixture, while synthetic
future-member names establish the general compatibility rule.

## Considered Options

Adding `canAcceptDirectInput` to `Thread` only repairs one schema drift. Making
all generated types permissive weakens action admission. Deriving acceptance
from stable/experimental classification fails when runtime output and schema
visibility differ and when one generated type is reused across roles. Keeping a
legacy strict decoder with a response escape hatch makes the obsolete
implementation the default policy. Retaining raw unknown members adds an
untyped compatibility surface without helping consumers that only require
known evidence.
