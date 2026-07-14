# Contributing

Thanks for helping make `codexsdk-go` boringly dependable.

## Development Setup

Use Go 1.23 or newer. If you work inside a larger local `go.work`, run release
checks with `GOWORK=off` so the module is tested the way outside users consume
it.

```sh
GOWORK=off go test ./...
```

## Required Checks

Run these before opening a pull request:

```sh
git ls-files -z -- '*.go' ':!:vendor/**' | xargs -0 gofmt -w
GOWORK=off go vet ./...
GOWORK=off go test ./...
```

Check generated protocol code reproducibility:

```sh
tmp="$(mktemp -d)"
GOWORK=off go run ./codexsdk/internal/cmd/protocolv2gen -out "$tmp"
diff -u codexsdk/protocolv2/method_registry.gen.go "$tmp/method_registry.gen.go"
diff -u codexsdk/protocolv2/protocol_types.gen.go "$tmp/protocol_types.gen.go"
```

## Public Surface

- Keep `codexsdk` focused on transport, typed facades, thread helpers,
  streaming, and server-request handling.
- Do not add provider-neutral LLM toolkit, business-domain, or private product
  dependencies.
- Avoid raw JSON-RPC escape hatches in the public API. Prefer typed
  `protocolv2` params and responses.
- Document any public API behavior that affects compatibility.

## Schema Baseline Updates

When updating the Codex app-server schema baseline:

1. Generate drift artifacts with `scripts/codexsdk_track_upstream.sh`.
2. Review upstream changes against the checked-in manifest and coverage matrix.
3. Copy only reviewed schema and metadata updates into
   `codexsdk/internal/protocolschema/appserver/v2`.
4. Keep provenance public and auditable: use upstream URLs, commit hashes,
   license identifiers, and repo-relative paths. Do not check in local absolute
   paths.
5. Regenerate `codexsdk/protocolv2/*.gen.go`.
6. Run gofmt, vet, tests, and generated-code reproducibility checks.
7. Update `CHANGELOG.md`, `NOTICE`, and `docs/release.md` if the legal,
   compatibility, or maintenance story changed.

The schema baseline may include experimental upstream surface. Marking a method
as present in a stable schema only means it appeared in the non-experimental
generated schema for that baseline; it is not an upstream lifetime guarantee.

## Real App-Server Testing

The real smoke test is opt-in:

```sh
CODEXSDK_REAL_APP_SERVER_SMOKE=1 \
CODEXSDK_REAL_APP_SERVER_MODEL=<model> \
GOWORK=off go test ./codexsdk -run TestRealAppServerSmokeStartResumeFork -count=1
```

Use a disposable workspace and an approval policy appropriate for the test. Do
not check in smoke-test transcripts, account data, tokens, or generated runtime
state.

## Public Cleanliness

Before publishing or tagging:

```sh
rg -n '(/Users/|/opt/homebrew|SECRET|TOKEN|PASSWORD|BEGIN (RSA|OPENSSH|EC|PRIVATE) KEY|sk-[A-Za-z0-9]{20,})' .
```

Review matches carefully. Protocol enum values such as `apikey` may be benign;
real credentials, local paths, private repos, and business data are not.
