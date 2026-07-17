# Architecture

The [current design](DESIGN.md) summarizes the accepted repository model.
[Architecture decisions](adr/) record the hard-to-reverse choices, and
the root [context map](../../CONTEXT-MAP.md) links the semantic owners.
Current CI and release evidence are documented in
[repository verification](../verification.md).
The scoped [2026-07-17 retention audit](retention-audit-2026-07-17.md) records
the consumer and ownership evidence for the North Star deletion candidates.

These documents govern repository boundaries. Exported code,
module-local API inventories, public behavior tests, module files, and public
Proxy artifacts remain the authoritative owners of their current facts.
