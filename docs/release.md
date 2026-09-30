# Release

The repository is one Go module with one SemVer. Required verification owns
source acceptance; it does not publish a module or select its release version.
GitHub Actions here do not create tags or GitHub Releases or certify public
module distribution. Once created, a version/tag identity is immutable.

## Live behavior and release notes

[`live-codex.md`](live-codex.md) defines the scenario-based runtime guarantee.
Use the active scenario assertions and actual results for the released source,
not a separate compatibility matrix or an unimplemented design paragraph.
The selected Codex runtime comes from that source's checked-in baseline.

Adding a scenario adds evidence. Removing or materially weakening a scenario
requires the implementation PR to identify the live-contract retirement and the
behavior no longer covered. Record it in the relevant `Unreleased` section of
[`CHANGELOG.md`](../CHANGELOG.md) and carry that entry into release notes.
A behavior-preserving refactor is not a retirement.

Documentation of a planned gate or scenario must be labelled as design work.
Do not release-note it as an implemented guarantee until its implementation and
applicable acceptance criteria are satisfied. Historical release entries should
not be rewritten to claim tests that had not run.
