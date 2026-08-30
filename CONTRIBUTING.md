# Contributing

Destinations: [`NORTHSTAR.md`](NORTHSTAR.md).
Invariants: [`DESIGN.md`](DESIGN.md).

Requires Go 1.23 or newer.

```sh
git clone https://github.com/ronhuafeng/llm-go.git
cd llm-go
go run ./internal/tools/cmd/repoctl verify
```

Public changes must update the owning module's inventory, behavior tests,
`CHANGELOG.md`, and a structured `.changes/` fragment when the change is
user-visible. Breaking changes update that module's README or CONTEXT when the
current contract changes.

Protocol baseline work uses
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
SDK test design follows [`codexsdk/Agents.test.md`](codexsdk/Agents.test.md).

Do not check in credentials, private prompts, customer data, or local
absolute paths. Report vulnerabilities through [`SECURITY.md`](SECURITY.md).
