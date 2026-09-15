# Codex protocol synchronization

Keep `codexsdk` aligned with one selected Codex App Server protocol version.

## Flow

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

The selected Codex source owns the upstream schema facts. One Go-native
`protocolupgrade` owner resolves the target, applies stable-target policy,
generates and compares the candidate, applies deterministic generated/baseline
changes, and checks the result. GitHub Actions owns triggers, permissions, the
single Agent pass, and publication. For real drift, the workflow invokes
[`codexsdk-sync-upstream`](../.agents/skills/codexsdk-sync-upstream/SKILL.md)
once for targeted handwritten/test changes.

Infrastructure failures fail the run directly. A failed run is retried from the
selected upstream source with a fresh candidate.

## Deterministic acceptance

For a clean comparison, or after the one Agent pass for real drift, require:

- candidate schema matches the checked-in baseline for the selected target;
- generated protocol Go and SDK surface reproduce exactly;
- `gofmt` / whitespace checks pass;
- `go vet ./codexsdk/...` passes;
- `go test ./codexsdk/...` passes;
- focused tests for actual protocol changes pass.

Publication happens only after those checks succeed. It creates or updates a
protected protocol-sync PR; it does not self-merge or tag a release. If base
movement makes publication unsafe, fail and rerun.

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

Structural/unit tests cover the drift branch:

```text
drift -> mechanical apply -> one Agent pass -> deterministic checks -> PR only on success
```
