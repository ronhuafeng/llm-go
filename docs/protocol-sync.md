# Codex protocol synchronization

Keep `codexsdk` compatible with a selected Codex App Server protocol version.
The workflow answers three questions:

1. Did the selected upstream protocol change?
2. If so, what generated/handwritten SDK changes are needed?
3. Do deterministic checks accept the result?

## Workflow

```text
resolve selected upstream source
        |
        v
generate fresh schema candidate
        |
        v
compare with checked-in baseline
        |
   clean? ---------------- yes --> native check + codexsdk tests --> success
        |
        no
        v
apply deterministic generated/baseline changes
        |
        v
one Agent pass on the same worktree
        |
        v
native check + codexsdk tests
        |
   pass? ----------------- yes --> protected protocol-sync PR
        |
        no
        v
workflow fails
```

Infrastructure failures fail the run directly. They are not implementation
prompts.

## Agent contract

For real protocol drift, the workflow may invoke one implementation Agent. The
Agent may edit `codexsdk` source, generated-owner code, focused tests, and directly
related documentation when justified by the observed drift.

The Agent must not resolve a different upstream target, stage, commit, push,
create/merge a PR, tag a release, or decide that its own work succeeded. The
workflow runs deterministic checks afterward.

The repository skill
[`codexsdk-sync-upstream`](../.agents/skills/codexsdk-sync-upstream/SKILL.md)
contains only this implementation contract; workflow orchestration stays in
Actions and protocol mechanics stay in `codexsdk` tooling.

## Deterministic acceptance

After a clean comparison, or after the Agent pass for real drift, require:

- candidate schema matches the checked-in baseline for the selected target;
- generated protocol Go and SDK surface reproduce exactly;
- `gofmt` / whitespace checks pass;
- `GOWORK=off go vet ./...` passes in `codexsdk`;
- `GOWORK=off go test ./...` passes in `codexsdk`;
- focused tests for actual protocol changes pass.

Repository PR verification still proves cross-module/current-source composition.

## Failure and retry

A failed run ends. Retry resolves the selected upstream source and generates a
fresh candidate again. Ordinary protocol sync has no separate repair workflow,
failed-run admission contract, historical artifact restoration, or CI
attestation ledger.

Publication is deliberately small: after deterministic acceptance, commit the
accepted worktree to a protocol-sync branch and create/update a protected PR. If
base movement makes that unsafe, fail and rerun rather than building a recovery
state machine.

## Final architecture acceptance

On the same final PR head `H`:

```text
PR verification(H) == success
AND
Codex Upstream Protocol Sync(
  head=H,
  upstream_ref=<current checked-in stable baseline>,
  force_compare=true,
  validation_only=true
) == success
```

The validation-only run must resolve the selected upstream ref/commit, generate
a fresh candidate, compare it with the baseline, run native protocol/generated
checks and `codexsdk` tests, skip the Agent on the clean baseline, and perform no
commit, PR, merge, or tag effect.

Structural/unit tests prove the drift branch:

```text
drift -> mechanical apply -> one Agent pass -> deterministic checks -> PR only on success
```
