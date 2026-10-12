# codexsdk

A Go SDK for driving an installed local Codex runtime through App Server.
This project is unofficial and experimental; it is not an OpenAI product.

```sh
go get github.com/ronhuafeng/llm-go@latest
```

## Runtime boundary

Install and configure the official `codex` executable before creating a client.
The SDK launches the configured command, normally
`codex app-server --listen stdio://`, and owns that child process and its stdio
connection. It does not install Codex, compile its Rust source, embed Core, or
reimplement the model/provider runtime.

`ClientOptions` supplies the working directory, command, initialization, and
application callbacks. Applications own credentials and execution policy.
Ordinary native Codex provider configuration is sufficient; the SDK does not
invent HTTP identity headers or require an llm-go proxy.

## Public API

`github.com/ronhuafeng/llm-go/codexsdk` provides client/process lifecycle,
generated typed facades, notifications, server requests, and `ThreadRunner`.
`github.com/ronhuafeng/llm-go/codexsdk/protocolv2` provides the generated App Server
v2 types and method registry.

`ThreadRunner.Start` and `Resume`, and their stream variants, compose thread
and turn operations. Direct facades expose Codex-specific operations such as
Fork. Applications provide admission decisions and server-request answers;
the SDK does not choose approval, permission, sandbox, user-input, or environment
policy for them.

Stream and result APIs preserve protocol observations and partial failure
information. In particular, final-answer absence and a present empty answer are
different facts. Do not infer a global non-empty-output requirement from an
individual live test that asks for a response.

Inbound newline-delimited JSON-RPC frames are limited to 16 MiB including the
newline delimiter.

## Startup and shutdown deadlines

Use `NewContext(ctx, options)` to bound validation, child launch and initialize
with the caller's context. Caller-owned option memory is copied synchronously
before that work; this preserves environment/input ownership even when a deadline
returns before validation finishes. A context canceled before construction prevents
launch. Initialization/transport failures keep their cause; an incomplete
construction at its deadline returns the context error, with owned cleanup
continuing independently. Canceling the startup context after successful
construction does not close the client. `New(options)` uses a 15-second deadline
for launch/initialize, rather than starting its timeout only after launch.

`CloseContext(ctx)` closes the whole client's admission and returns
`(ShutdownResult, error)` within the caller's wait. `ProcessReaped` separately
reports owned-process cleanup; `CallbackWorkPending` counts admitted work still
executing or queued. `Cause` is the first client failure, or `ErrClientClosed`
when normal closure settled pending work. A nil error proves process cleanup
and all admitted callback work completed. `ShutdownError` uses a fixed safe error
string and retains deadline, first-cause and `ErrCallbackWorkPending` identities
through `errors.Is`. Applications own disclosure of unwrapped errors.

Normal shutdown publishes partial facts and settles pending RPC/Exact Run
observers before joining callbacks. It cancels server-request callbacks promptly
and lets accepted notification callbacks drain for up to two seconds. A caller
shutdown deadline or expiry of that grace cancels the client notification context
and discards queued work explicitly under the close/failure cause. Process cleanup
runs independently of callback joins and `writeMu`: once launch has completed,
it waits at most two seconds for callback grace, then closes transport, terminates
and reaps the child, escalating to kill after a further two seconds. Context
cancellation uses this same cleanup path, allowing an executable launcher to
forward Interrupt and join its native child before escalation. OS launch itself
is not preemptible; a canceled launch is killed/reaped when `os/exec`
returns. Caller return remains bounded while that owned cleanup proceeds.

A Go callback ignoring cancellation cannot be killed. It stays reported as
pending, and its retained notification remains charged until it returns. Queued
reservations are released on discard. Canceled cooperative callbacks get the
remaining grace to finish publishing delivery facts and releasing reservations;
pending work is decremented only after that cleanup. Readable Exact Run histories keep their
ordinary retention ownership. Late callbacks, EOF or terminal delivery cannot
replace the first close/failure cause or create a successful run. EOF during
teardown waits for the publisher's partial-evidence snapshot rather than racing
another terminal snapshot. Callback delivery failure during normal closure is
also reported explicitly by the shutdown API.

`Close()` uses a five-second caller deadline and joins normal cooperative work.
This replaces its prior potentially infinite callback join; callers needing
cleanup observations or another wait should use `CloseContext`. Concurrent close
calls share one cleanup and cannot reopen admission. A second call can observe
completed process cleanup while an uncooperative callback still remains pending.

`Stream.Wait(ctx)` and `Stream.Next(ctx)` remain caller-local observations:
canceling their contexts does not cancel the shared run or client. `Stream.Close`
interrupts the shared run; client close stops the entire client. Terminal
success still waits for all preceding admitted raw/typed callback delivery.
No request is retried or resubmitted by these APIs, and server-request decisions
remain application-owned.

## Child process environment

`ClientOptions.Env` follows Go's process environment contract. A nil slice
preserves ordinary parent inheritance, including `os/exec` working-directory
handling. A non-nil slice completely replaces the child environment; an empty
slice requests no application environment. Each entry is `KEY=value`, where
`KEY=` means present-empty. Omit a key from a replacement to remove it from the
child. Keys and values are not trimmed; values may contain `=`.

Duplicate keys use the last supplied value, matching `os/exec`. Matching is
case-sensitive on Unix and case-insensitive on Windows, including Windows
hidden drive-directory entries. Platform-required entries such as Windows
`SYSTEMROOT` may still be supplied by `os/exec`. Executable-name lookup uses the
parent's environment, as in Go; use an absolute `Command[0]` when selecting an
executable independently of the parent `PATH`.

The SDK validates and copies entries before launch. Missing separators, empty
ordinary keys and NUL bytes return `ErrInvalidEnvironment` without including
entry names or values. Caller slices must not be mutated concurrently with
construction; mutation after construction does not change client options.
To override inherited values, build a replacement from `os.Environ()` and append
the overrides. To remove inherited values, filter those keys from that copy.
The SDK never calls `os.Setenv` or `os.Unsetenv` for a child.

For isolated concurrent clients, provide distinct `CODEX_HOME` and credential
entries in each client's replacement. Credentials and all other process
configuration remain application-owned. Environment values are passed only
through the process environment, without argv additions or environment dumps in
SDK diagnostics. This changes neither initialization nor runtime identity
inference. The [compiled example](example_test.go) shows explicit replacement;
controlled process fixtures prove isolation without provider requests.

## Required raw notifications

Set `ClientOptions.RawNotifications` to a `RawNotificationSubscription` with
explicit `Methods` and a `Handler`. Generated server notification methods are
supported. Unknown notifications remain ignored when unsubscribed; unsupported
required methods, duplicate method entries, and missing handlers fail `New`
before launching the child.

`rawResponseItem/completed` is upstream internal-only at the selected baseline:
it has a standalone payload schema but is absent from the public notification
union. Subscribe using `RawResponseItemCompletedExtension` and explicitly set
`EnableRawResponseItemExtension: true`. This SDK extension validates the entire
payload against the checked-in standalone schema, without changing generated
protocol declarations or their provenance. Schema regeneration updates that
validation input; it must continue to compile and satisfy the extension tests.
This does not establish an official runtime emission guarantee. Applications
request raw emission through the existing `ThreadStartParams.ExperimentalRawEvents`
and experimental initialization settings when supported by their runtime.

```go
options.RawNotifications = &codexsdk.RawNotificationSubscription{
    Methods: []string{codexsdk.RawResponseItemCompletedExtension},
    EnableRawResponseItemExtension: true,
    Handler: func(ctx context.Context, event codexsdk.RawNotification) error {
        // Inspect event.Envelope privately; return an error to reject delivery.
        return nil
    },
}
```

`RawNotification.Envelope` retains JSON values, unknown fields, exact number
tokens, and omitted/null distinctions. `Method`, `ThreadID`, and `TurnID` expose
wire correlation. Raw and typed callbacks share the existing finite notification
queue (64 entries by default, configurable with `NotificationQueueCapacity`),
consume events in transport order, and run raw before typed for the same frame.
A duplicate emitted frame is a separate observation and is delivered once;
the SDK does not infer item deduplication from payload equality. Attachment and
replay do not dispatch a frame again. Observation is client-scoped: late frames
retain their wire identity without rewriting an already completed run snapshot.
Raw items go to the callback, without a
second raw history in `ThreadRunResult`.

Exact Run successful terminal completion waits for preceding admitted callback
work, including raw observation even if only raw-item methods were subscribed.
Direct typed facade responses can arrive before asynchronous callbacks complete;
`Close` joins admitted callback work within the finite shutdown grace described
below. Callbacks should return promptly and avoid synchronous close on their own
client.
Invalid required payloads fail the client with `ErrRawNotificationDecode`;
callback error/panic uses `ErrRawNotificationDelivery` and `ErrHandlerFailed`;
queue saturation uses `ErrNotificationBackpressure`. Pending work receives the
first failure and Exact Run preserves available partial typed observations.
Raw callback errors retain cause identity for `errors.Is`/`errors.As`, while
their error strings and panic diagnostics omit private callback/payload values.
Applications control any disclosure of the delivered envelope or unwrapped cause.

A required subscription ensures SDK delivery or explicit failure for admitted
frames. A server that never emits an expected raw item is an application
acceptance decision; a clean SDK terminal alone does not prove that observation.
These fixture-tested delivery semantics do not expand the existing live scenarios.

## Notification retention

Every client has a finite `ClientOptions.NotificationRetention` budget with
`MaxCount` and `MaxBytes`. Zero selects `DefaultNotificationRetentionCount`
(65,536 representation slots) and `DefaultNotificationRetentionBytes` (64 MiB)
independently; negative limits fail before launch. Neither limit can be disabled.
Applications expecting larger histories must explicitly choose larger finite
limits. This is a compatibility change from unbounded Exact Run histories and
attachment buffers; ordinary history/cursor/result semantics stay intact.

The count covers retained representations, not just emitted frames. Byte charges
include Go value headers, pointed-to values, strings, slice capacity, and
conservative map/ownership overhead; shared backing storage is charged again.
They are deterministic admission charges, not measured heap size or JSON wire
length. The SDK reserves before storing evidence, using these owners:

| Owner | Representation slots and release |
| --- | --- |
| Callback queue, including executing callback | One typed slot and one raw slot when both exist; released after dispatch/discard. |
| Exact Run history | Three slots per typed observation, reserving the cloned history plus usage/terminal/error projections; retained with the run. |
| Pending turn / replay | Four slots for params and future history/projections; transferred to the run during replay without further allocation of budget. |
| Pending thread | Three slots including future history; transferred at attachment. |
| Decode diagnostics | One slot per retained reference, transferred to the matching run or released at failure. |
| Armed attachment identities | One slot per distinct thread/turn identity; released at attachment or client failure/closure. |

Conservative reservations remain charged after transfer even if fewer copies
are needed. All clients' concurrent runs share their own client budget; separate
clients have independent budgets. Buffer cleanup clears payload references and
releases its ownership. Completed/canceled `Stream` histories remain readable,
so they stay charged until the run and its cursors become unreachable; Go cleanup
then releases their reservations. Cleanup timing is controlled by Go, and delayed
collection can conservatively exhaust a budget. `Next` does not evict history;
its current value references that immutable SDK history. Returned results and
notification snapshots are copied and application-owned. Retaining arbitrarily
many application snapshots is outside this SDK budget.

Exhaustion fails the client with the fixed, payload-free
`ErrNotificationRetention`, settles pending RPCs and Exact Run observers, and
preserves admitted partial evidence. A later terminal cannot replace that cause.
No required event is silently evicted to permit success. This differs from
`ErrNotificationBackpressure`, which reports a full callback channel even when
retention budget remains. A missing/slow callback, direct typed facades, raw
subscriptions, late/duplicate events, and attachment races cannot bypass these
limits. The existing 16 MiB ingress-frame bound limits transient decoding;
retention limits constrain cumulative stored observations. This adds no durable
recorder and changes no application acceptance or server-request authority.

## Compatibility and evidence

The generated surface follows the selected source in
[`baseline_metadata.json`](internal/protocolschema/appserver/v2/baseline_metadata.json).
Live verification resolves the official latest Codex CLI once per execution and
records its exact observed version separately from that schema source.

Generated API availability and live runtime coverage are different. Our live
guarantee is the behavior exercised and asserted by active integration
scenarios; untested combinations are not implied by the presence of a method.
No separate capability registry is maintained. Protocol-generation manifests
and coverage data remain generator inputs/projections, not a live guarantee set.

`GeneratedBaseline` and `Client.Provenance()` preserve source and runtime facts.
Initialization identity does not establish whole-surface compatibility;
`RuntimeCompatibilityUnknown` must not be turned into a positive verdict just
because a version matches or one scenario passes.

## Verification

Run owner-local deterministic checks with:

```sh
go vet ./codexsdk/...
go test -race ./codexsdk/...
```

The accepted real-runtime suite uses two integration stories rather than one
test per RPC. The composed structured call covers ordinary Start. The direct
thread story continues through persistent Start, Resume, Fork, and new work on
the fork. It does not test conversational recall or other Core-owned history
semantics.

See [`Live Codex integration`](../docs/live-codex.md) for the canonical test
environment, scenario assertions, and implementation status. Scenario retirement
must be explicit in the PR and changelog/release notes.

[`example_test.go`](example_test.go) is the compile-checked client setup example.
Use [`llmcaller/codex`](../llmcaller/codex) for provider-neutral typed results.
Protocol evolution is in [`protocol-sync.md`](../docs/protocol-sync.md);
acceptance is in [`verify.md`](../docs/verify.md); platform scope and reporting
are in [`SUPPORT.md`](../SUPPORT.md).
