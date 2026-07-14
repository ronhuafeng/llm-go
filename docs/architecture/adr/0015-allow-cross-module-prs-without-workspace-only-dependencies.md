---
status: accepted
---

# Allow cross-module PRs without workspace-only dependencies

A pull request may modify more than one public module when every affected module
continues to pass independently with `GOWORK=off` and already published
requirements. Repository boundaries are enforced by semantic ownership and
publishability, not by a one-directory-per-PR rule.

Valid cross-module pull requests include changes such as:

- documentation and diagnostics that preserve existing contracts;
- compatible evidence-projection corrections;
- adapter changes that use already published upstream APIs;
- repository architecture checks and three-layer canaries.

When a downstream change requires an unpublished upstream contract, the work is
delivered as linked staged pull requests:

```text
upstream expand PR
    -> merge, tag, and proxy verification
downstream migration PR
    -> update exact go.mod requirement, merge, and tag
optional upstream contract/removal PR
```

The linked pull requests may share one issue, milestone, and generated release
plan. They cannot share an unpublished dependency through committed workspace,
replacement, or pseudo-version state.

## Consequences

- Coherent repository-wide changes can still receive one review when they are
  independently buildable.
- New cross-module contracts expose their real publication edge.
- The monorepo reduces coordination overhead without claiming transactional Go
  module releases.
- Affected-module CI uses the dependency closure rather than assuming each pull
  request belongs to one module.

## Considered options

Requiring exactly one public module per pull request was rejected because it
would split changes that are safe, coherent, and independently verifiable.

Allowing workspace-only cross-module pull requests onto `main` was rejected by
ADR-0009 because it hides unpublished dependency requirements.

Treating a feature branch as a release transaction was rejected because a pull
request merges as one commit range while the public proxy publishes modules
independently.
