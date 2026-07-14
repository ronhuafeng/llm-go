# llmkit release contract

`llmkit` is the independently versioned module at
`github.com/ronhuafeng/llm-go/llmkit`. Its tags are prefixed by the module
directory:

```text
llmkit/vX.Y.Z
```

The first monorepo release is `llmkit/v0.6.0`, continuing the legacy
`llmkit-go@v0.5.0` lineage. The path migration is intentionally breaking under
the pre-v1 policy and does not change API shape or runtime semantics.

## Public API

The public API is the exported surface of:

- `github.com/ronhuafeng/llm-go/llmkit/settle`
- `github.com/ronhuafeng/llm-go/llmkit/llmschema`
- `github.com/ronhuafeng/llm-go/llmkit/llmadapter`
- `github.com/ronhuafeng/llm-go/llmkit/llmstep`

The canonical inventory is
`internal/architecture/testdata/handwritten-api.txt`. Any inventory diff is an
API-design review, not incidental test churn. Runtime behavior remains owned by
module tests.

## SemVer policy

Before v1.0.0, patch releases contain compatible fixes and documentation;
additive or necessary breaking changes use a minor release and declare the
breaking impact explicitly. At and after v1.0.0, standard SemVer major, minor,
and patch rules apply.

Each user-visible change supplies a module-local structured fragment in
`.changes/`. A breaking fragment links to module-local migration guidance.

## Preflight

Pull-request verification runs the module independently with `GOWORK=off` on
its declared minimum Go version, current Go, and the race detector. It also
checks formatting, tidy metadata, provider-neutral boundaries, the canonical
API inventory, and an isolated source consumer.

Before a tag, protected release CI additionally verifies:

- the requested version and `llmkit/` tag prefix;
- API impact and structured change fragments;
- a clean module archive containing no sibling module, root workspace, or
  repository tooling;
- the exact source commit and tree;
- changelog and migration documentation.

The resulting digest-bound release plan authorizes one immutable tag. Tags are
never created manually or moved after publication.

## Published verification

After the tag is pushed, CI resolves the module from the public Go proxy with
fresh caches, `GOWORK=off`, `GOVCS=*:off`, and the public checksum database. An
isolated external consumer must compile and run before the GitHub Release is
marked verified.

If propagation or verification fails, the tag remains immutable and the
release stays unverified. An incorrect release is corrected with a new version,
never by moving or reusing the tag.
