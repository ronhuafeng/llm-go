---
status: accepted
---

# Use deterministic PR, release-preflight, and post-tag verification stages

Repository verification is divided by the facts that each stage can actually
observe. CI owns merge and release evidence, but a pull request cannot prove the
contents of a tag that has not been published.

## Pull-request required checks

Pull-request checks are deterministic with respect to the checkout:

- compute the affected module closure from changed paths;
- run each affected module with `GOWORK=off` on the minimum supported Go
  version and the current Go version;
- run race tests on the supported current environment;
- run the workspace three-layer canary when its dependency closure is affected;
- verify the permitted import graph, module paths, committed requirements, API
  inventories, provenance, and release metadata;
- compile and run an ephemeral consumer against checkout sources, while clearly
  labeling that result as source compatibility rather than published-artifact
  evidence.

## Release preflight

Before creating a tag, CI:

- runs the complete applicable pull-request suite;
- verifies a clean tree, tidy module files, module zip contents, and the planned
  path-prefixed tag;
- verifies that every committed upstream version needed by the adapter is
  already available from the public proxy;
- emits an immutable release plan containing module, commit, version,
  dependency tuple, and publication order.

Preflight authorizes a specific tag operation. It does not claim that the tag's
artifact already exists.

## Post-tag verification

After an immutable tag is pushed, CI:

- uses empty module and build caches;
- sets `GOWORK=off` and `GOVCS=*:off`;
- resolves through `proxy.golang.org` with the official checksum database;
- downloads the tagged module rather than importing source from the checkout;
- compiles and runs a clean external consumer;
- for the adapter, compares the dependency versions declared by the
  proxy-resolved adapter artifact with the complete resolved three-module
  tuple.

Only a successful post-tag check marks the GitHub Release as verified. A failed
published tag is never moved or reused; recovery publishes a new version.

## Consequences

- Network-sensitive proxy evidence runs only where a published artifact exists
  to inspect.
- Pull requests retain stable required checks without weakening release proof.
- Checkout compatibility and artifact compatibility are reported as different
  facts.
- GitHub Actions, rather than developer network conditions, owns authoritative
  merge and release evidence.
- Check names and outputs must make the verification stage explicit.

The typed implementation of these policies and the intentionally thin workflow
boundary are defined by ADR-0011.

## Considered options

Running a live public-proxy integration on every pull request was rejected
because the candidate tag does not yet exist and the result would only recheck
older artifacts while adding network flakiness.

Using only post-tag tests was rejected because source and packaging failures
should be caught before an immutable tag is created.

Treating a local checkout consumer as published-module evidence was rejected
because workspace and filesystem resolution do not prove what the proxy serves.
