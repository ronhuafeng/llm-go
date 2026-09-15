# Release

Repository required CI owns whether a source commit is accepted.

An external release/version authority chooses the version and maps it to an
already-accepted commit, then creates the immutable version/tag/release.

GitHub Actions in this repository do not select a SemVer, create tags or
GitHub Releases, or observe public module distribution.

Once created, version/tag identity is immutable.
