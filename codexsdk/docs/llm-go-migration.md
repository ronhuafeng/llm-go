# Migrating from codexsdk-go to llm-go

`github.com/ronhuafeng/codexsdk-go` ends at `v0.5.1`. Development continues in
the independently released SDK module within
`github.com/ronhuafeng/llm-go`, beginning with `codexsdk/v0.6.0`.

The replacement is available only after
`github.com/ronhuafeng/llm-go/codexsdk@v0.6.0` resolves through the public Go
proxy. Until then, the immutable legacy release remains the consumable version.

## Import mappings

| Legacy import | Replacement import |
| --- | --- |
| `github.com/ronhuafeng/codexsdk-go/codexsdk` | `github.com/ronhuafeng/llm-go/codexsdk` |
| `github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2` | `github.com/ronhuafeng/llm-go/codexsdk/protocolv2` |

The repository migration preserves exported identifiers, Go package names,
generated protocol facts, exact lifecycle behavior, and typed evidence. Update
module requirements and imports together; do not add a local `replace`,
workspace dependency, forwarding package, or pseudo-version as migration
evidence.

The new repository becomes the sole active source after its replacement
modules pass public-proxy and clean-consumer verification. This legacy path
receives no feature or security updates after `v0.5.1`, although its immutable
tags remain proxy-resolvable.
