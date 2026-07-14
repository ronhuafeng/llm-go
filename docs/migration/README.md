# Migration status

The complete frozen `codexsdk-go` and `llmcaller-codex-go` histories are now
reachable from this repository through their recorded relocation and merge
edges. The `llmkit-go` history remains pending its final legacy release and
provenance baseline. No replacement module tag is available from this
repository yet.

The accepted sequencing and completion gates are defined by the
[target design](../architecture/DESIGN.md) and its
[migration Definition of Done](../architecture/adr/0023-require-complete-provenance-equivalence-and-published-evidence.md).

Consumer-facing old-to-new import guidance will be published only when the
corresponding replacement module is ready and its planned tag is explicit.

## Imported-history issue audit

GitHub evaluates historical closing keywords when previously unrelated commits
become reachable from the default branch. A legacy commit that referred to an
issue number in its source repository can therefore close the same number in
this repository even though the tickets are unrelated.

This occurred when imported adapter commit
[`c4cdcb5`](https://github.com/ronhuafeng/llm-go/commit/c4cdcb5de99406013a98b072186b758981f0a834)
closed [`#5`](https://github.com/ronhuafeng/llm-go/issues/5). The issue-event
audit exposed the imported commit as the closure source, and the issue was
reopened after verifying that the toolkit destination and provenance entry did
not exist.

After every history-import merge, inspect issue events for closures attributed
to imported commits and reopen any ticket whose own completion evidence is
absent. Do not treat tracker state alone as proof that an import ticket or its
dependents are complete; verify the destination tree, provenance entry, and
merge ancestry first.
