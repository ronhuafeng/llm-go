---
status: accepted
---

# Use deterministic PR, release-preflight, and post-tag verification stages

Repository verification is divided by the facts each stage can observe. A pull
request cannot prove the contents of a tag that has not been published.
Checkout compatibility and published-artifact compatibility are different
facts.

Current procedure, checks, and evidence owners:
[docs/verification.md](../../verification.md).

Protected tag identity:
[docs/releasing.md](../../releasing.md).

The typed implementation of these policies and the thin workflow boundary are
defined by ADR-0011.
