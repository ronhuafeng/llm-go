# codexsdk release contract

`codexsdk` is the independently versioned module at
`github.com/ronhuafeng/llm-go/codexsdk`. Its tags are prefixed by the module
directory:

```text
codexsdk/vX.Y.Z
```

The first monorepo release is `codexsdk/v0.6.0`, continuing the legacy
`codexsdk-go@v0.5.1` lineage. The pre-v1 path migration is intentionally
breaking; it changes no exported identifier, generated protocol fact, exact
lifecycle behavior, or evidence contract.

## Public API

The handwritten public API is the exported surface of the module-root
`codexsdk` package. Its canonical inventory is
`testdata/handwritten-api.txt`. Generated `protocolv2` declarations and
generated facade methods are derived from the classified schema manifest and
remain generator-owned facts.

An inventory diff is an API-design review, not incidental test churn. The first
monorepo release must be mechanically equivalent to the legacy inventory after
mapping:

```text
github.com/ronhuafeng/codexsdk-go/codexsdk
  -> github.com/ronhuafeng/llm-go/codexsdk
```

## SemVer policy

Before v1.0.0, patch releases contain compatible fixes and documentation;
additive or necessary breaking changes use a minor release and declare the
breaking impact explicitly. At and after v1.0.0, standard SemVer rules apply.

Each user-visible change supplies a module-local structured fragment in
`.changes/`. Breaking fragments link to module-local migration guidance.

## Module-owned generation

Protocol schema inputs, manifests, generation code, generated output, and
drift policy remain inside this module. From the `codexsdk` directory, verify
the checked-in generated facts with:

```sh
./scripts/codexsdk_validate_sync.sh
```

The equivalent focused generation checks are:

```sh
GOWORK=off go run ./internal/cmd/protocolv2gen -stdout method-registry |
  diff -u protocolv2/method_registry.gen.go -
GOWORK=off go run ./internal/cmd/protocolv2gen -stdout protocol-types |
  diff -u protocolv2/protocol_types.gen.go -
tmp="$(mktemp -d)/sdk_surface.gen.go"
python3 scripts/codexsdk_generate_sdk_surface.py --out "$tmp"
gofmt -w "$tmp"
diff -u sdk_surface.gen.go "$tmp"
```

Root GitHub workflows own scheduling and platform permissions for upstream
protocol sync. They invoke these module-owned commands; they do not reproduce
schema or generation policy.

## Preflight and publication

Pull-request verification runs the module independently with `GOWORK=off` on
minimum Go, current Go, and the race detector. It checks formatting, tidy
metadata, vet, ordinary and lifecycle tests, the canonical API inventory,
generator drift, a clean module archive, and an isolated source consumer.

Before a tag, protected release CI additionally binds the requested version,
path-prefixed tag, exact source commit and tree, declared impact, migration
documentation, module archive, mapped legacy API inventory, and evidence
digests into one immutable authorization. Tags are created only by protected
CI and are never moved or reused.

After tag creation, CI resolves the module from the public Go proxy with fresh
caches, `GOWORK=off`, `GOVCS=*:off`, and the public checksum database. An
isolated external consumer must compile and run before the GitHub Release is
marked verified. If verification fails, the tag remains immutable and the
release stays unverified; correction uses a new version.
