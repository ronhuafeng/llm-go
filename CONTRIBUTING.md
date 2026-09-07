# Contributing

Semantics: [`NORTHSTAR.md`](NORTHSTAR.md).
Live invariants: [`DESIGN.md`](DESIGN.md).

Requires Go 1.23 or newer.

```sh
git clone https://github.com/ronhuafeng/llm-go.git
cd llm-go
go run ./internal/tools/cmd/repoctl verify
```

Public changes must update the owning module's code, public behavior tests,
`CHANGELOG.md`, and structured `.changes/` fragment when the change is
user-visible. Update public package docs or the module README when
consumer-visible behavior or usage changes. Compatibility evidence is derived
from exported source and compared to the latest stable tag during
verification and release.
Inventories and change fragments do not establish public truth when they
disagree with exported artifacts. Update a module `CONTEXT.md` only when its
owned vocabulary changes.

Protocol baseline work uses
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
SDK test design follows [`codexsdk/Agents.test.md`](codexsdk/Agents.test.md).

Do not check in credentials, private prompts, customer data, or local absolute
paths. Report vulnerabilities through [`SECURITY.md`](SECURITY.md).
