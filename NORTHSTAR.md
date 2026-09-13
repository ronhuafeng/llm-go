# North stars

`llm-go` is a Go library. Keep its rules close to the behavior they protect.
Prefer plain Go, direct protocol facts, and executable tests over repository-specific
frameworks or terminology.

The main rule is:

> **Models propose. Programs decide. Effects require authority.**

## Rules

### Report only what is known

Do not fill API fields from guesses, defaults, adapter names, or model-name
heuristics when the value was not actually obtained.

Preserve distinctions that callers can observe and that change behavior, such
as absent versus present-empty output, requested versus server-reported model,
or total versus per-request token counts.

A failed operation may still return partial data when the API can do so safely.

### Model output is data

Parsing or decoding model output does not make it correct. Deterministic caller
code decides acceptance. Rejected output may be retained for diagnostics but
must not appear where the API promises accepted output.

If retries are used, the application decides what validation feedback is sent
back to the model.

### Applications own policy and side effects

SDKs may expose callbacks and mechanisms. Applications decide approval,
permissions, sandboxing, disclosure, and external mutations. Prompt text is not
authority.

When Codex asks the application for a decision or value, `codexsdk` delivers the
request and encodes the supplied response. It does not invent one.

### Preserve protocol and caller meaning

`codexsdk` follows the selected Codex App Server protocol. If the generator
cannot represent a supported schema shape, generation fails rather than silently
dropping it.

`llmcaller/codex` may translate Codex data into `llmkit` types or reject a schema
Codex cannot represent. It must not silently change the caller's schema meaning.

### Keep ownership small

The runtime has three package families inside one Go module:

```text
llmkit         ---\
                   +--> llmcaller/codex
codexsdk       ---/
```

`llmkit` owns provider-neutral typed inference. `codexsdk` owns the local Codex
App Server protocol and lifecycle. `llmcaller/codex` owns the translation.
`llmkit` and `codexsdk` do not depend on each other directly. The repository
root is not another runtime package.

The repository is one source unit, one Go module (`github.com/ronhuafeng/llm-go`),
one Go floor, and one SemVer. Do not reconstruct sibling modules with workspace
files, `replace` directives, or published-closure checks.

Do not add `common`, `core`, `shared`, registries, facades, or compatibility
layers without a current behavioral need.

### Test at the owner

Use Go tests for Go behavior, Go generators/checkers for generated Go source,
the selected Codex source for upstream schema facts, and GitHub Actions for
orchestration and repository effects.

Verify the root module directly. Generated protocol reproducibility is a
separate deterministic check, not a substitute for `go test`.

### Keep verification proportional

For ordinary source changes, prefer:

```text
canonical input -> change -> deterministic checks -> allowed effect
```

If a run fails, retry from canonical input. Do not create cross-run repair
state, attestation ledgers, or durable proof objects for state that can simply be
regenerated and checked again.

Immutable identities still matter when identity itself is part of the contract,
for example an upstream commit or release tag.

### Delete retired machinery

Every persistent abstraction, workflow state, helper, compatibility path,
document, or test needs a current consumer or invariant. If removing it would
not weaken a current requirement, remove it. Git history records retired
designs.

## Package family goals

- **`llmkit`:** typed provider-neutral model calls with deterministic caller
  validation and bounded retries.
- **`codexsdk`:** a faithful Go client for one local Codex App Server.
- **`llmcaller/codex`:** the smallest meaning-preserving bridge between them.
- **Repository:** one module and one version, without creating another runtime
  or CI framework.

When two designs preserve the same behavior, choose the one that requires less
context to understand, change, and verify.
