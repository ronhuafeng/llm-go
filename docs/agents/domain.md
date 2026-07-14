# Domain docs

This repository uses a multi-context documentation layout.

Before exploring or changing code:

1. Read the root `CONTEXT-MAP.md`.
2. Read the context documents relevant to the requested module.
3. Read system-wide ADRs under `docs/architecture/adr/` that touch the area.
4. After module histories are imported, also read any module-local ADRs and
   context documents named by the context map.

Use glossary terms exactly. If a proposed change contradicts an accepted ADR,
surface the conflict instead of silently overriding it.
