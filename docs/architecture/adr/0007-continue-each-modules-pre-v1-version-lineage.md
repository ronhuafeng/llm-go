---
status: accepted
---

# Continue each module's pre-v1 version lineage

The first releases from `github.com/ronhuafeng/llm-go` continue the version
lineage of the corresponding old repository and advance one minor version:

```text
github.com/ronhuafeng/llm-go/llmkit
    tag: llmkit/v0.5.0

github.com/ronhuafeng/llm-go/codexsdk
    tag: codexsdk/v0.6.0

github.com/ronhuafeng/llm-go/llmcaller/codex
    tag: llmcaller/codex/v0.5.0
```

The baselines verified before this decision were `llmkit-go@v0.4.1`,
`codexsdk-go@v0.5.0`, and `llmcaller-codex-go@v0.4.1`.

These versions identify the repository and import-path migration. They do not
imply lockstep versioning. After the first release, each module advances only
when its own contract changes.

## Consequences

- Consumers can relate the new modules to their established pre-v1 history.
- The migration does not reset mature projects to `v0.1.0`.
- The migration does not claim v1 stability without a dedicated stability
  review.
- The initial module versions intentionally differ; repository membership is
  not encoded as version equality.

## Considered options

Restarting at `v0.1.0` was rejected because it discards useful maturity and
lineage signals.

Starting all modules at `v1.0.0` was rejected because the repository migration
does not itself establish a reviewed v1 compatibility promise.

Giving all modules the same initial version was rejected because it would
contradict independent SemVer and recreate pressure toward lockstep releases.
