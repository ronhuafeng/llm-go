# Decouple exact-run completion and history observation from delivery capacity

Exact streams expose a non-destructive `Wait` operation whose callers wait
independently for the same terminal run and receive isolated result snapshots.
A waiter's context only bounds that caller's wait; `Stream.Close`, client
shutdown or failure, and protocol terminal completion remain the shared run
boundaries. This reuses the complete ordered evidence already retained in the
result instead of introducing subscriber registries or a second history model.
`Next` advances a per-Stream cursor over that same immutable ordered history.
Consequently a caller that only uses `Wait` cannot cause per-run notification
backpressure, while `Next` retains its existing cancellation semantics. The
global notification-handler queue remains a separate bounded delivery concern.

The durable stress contract is a Wait-only exact run containing at least 1,280
notifications before terminal completion. The run must complete successfully,
publish every notification in order, remain fully observable through `Next`,
and leave the Client usable for another run. This fixed lower bound prevents a
future delivery buffer from silently becoming a result-history limit.
