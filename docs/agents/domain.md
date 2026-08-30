# Domain docs

This repository is multi-context. Resolve paths from the repository root that
contains root `AGENTS.md`.

Before changing code:

1. Read [Northstar.md](../architecture/Northstar.md) if the change could move
   an owner's destination or collapse a join.
2. Read root `CONTEXT-MAP.md`.
3. Read the module `CONTEXT.md` for the code you will touch.
4. Read [DESIGN.md](../architecture/DESIGN.md) and only the
   [still-binding ADRs](../architecture/adr/) that constrain the area.
5. Read a module-local ADR only when that module's `CONTEXT.md` or README
   names it.

Use glossary terms exactly. If a proposed change contradicts an accepted ADR
or a north star, surface the conflict instead of silently overriding it.
