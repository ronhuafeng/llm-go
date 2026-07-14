# Security Policy

## Supported versions

No version of this frozen legacy module is supported after the `v0.5.0`
cutover. Security maintenance continues only in
`github.com/ronhuafeng/llm-go/llmkit`, beginning with `llmkit/v0.6.0`.

## Reporting a vulnerability

Do not open a public issue with exploit details. Until the successor repository
publishes its own confidential intake policy, use this repository's
[private vulnerability reporting](https://github.com/ronhuafeng/llmkit-go/security/advisories/new).
Maintainers may coordinate disclosure and migration guidance through that
channel, but this does not create a commitment to patch legacy releases.

## What to include

Include:

- Affected version or commit.
- Package and API involved.
- Reproduction steps or proof of concept.
- Impact and any known mitigations.

## Handling

The immutable legacy releases are retained as migration evidence but will not
receive security fixes. Consumers should migrate to the successor module and
apply supported releases there.
