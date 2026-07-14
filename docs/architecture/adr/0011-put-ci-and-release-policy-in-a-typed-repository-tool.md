---
status: accepted
---

# Put CI and release policy in a typed repository tool

The non-published `internal/tools` workspace module contains a typed Go command
at `internal/tools/cmd/repoctl`. It owns repository verification and release
policy that must be deterministic and testable:

```text
repoctl affected
repoctl verify
repoctl release-plan
repoctl verify-tag
```

The command owns:

- affected-module closure;
- permitted dependency ordering;
- module and tag identity validation;
- release-plan construction;
- module graph and compatibility-tuple checks;
- machine-readable evidence generation;
- post-tag artifact verification logic.

GitHub Actions workflows remain thin. They own only GitHub platform concerns:

- triggers and event inputs;
- job permissions;
- environments, secrets, and concurrency;
- checkout and Go runtime setup;
- invoking `repoctl`;
- uploading evidence artifacts and updating release status.

Workflow files receive normal syntax and action validation, plus a small
end-to-end smoke test. The repository does not maintain a custom parser that
tries to prove business policy by inspecting workflow YAML structure.

`repoctl` emits versioned machine-readable JSON evidence. Human-readable logs
are a rendering of that evidence, not an independent fact source.

Its governed release units come from the minimal registry defined by ADR-0012;
module and dependency facts continue to come from `go.mod`.

The public modules must not import `internal/tools`. Architecture checks enforce
that the tool remains a repository consumer rather than a runtime dependency.

Module-owned generator semantics remain outside `repoctl`; see ADR-0014.

## Consequences

- Release behavior can be unit-tested with ordinary Go fixtures.
- Local reproduction and CI invoke the same policy implementation.
- Workflow changes are smaller and primarily concern platform wiring.
- GitHub Actions remains the authority for real permissions, hosted execution,
  and public network observations.
- The internal tool has no public compatibility promise or independent release.

## Considered options

Encoding release policy directly in workflow YAML and shell was rejected
because it fragments logic across weakly typed, difficult-to-test surfaces.

Maintaining a workflow-structure parser was rejected because it tests an
incidental YAML arrangement rather than the behavior and evidence contract.

Publishing the orchestration tool as a fourth module was rejected because it is
repository infrastructure and must not become part of the runtime module graph.
