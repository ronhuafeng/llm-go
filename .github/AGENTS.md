# AGENTS.md

## Workflow safety

A `run` step before checkout must not inherit a workflow-level non-root
`defaults.run.working-directory`; set the step's `working-directory` to
`${{ github.workspace }}` explicitly.

Default-branch control-plane workflows must check out the triggering trusted
`${{ github.sha }}` rather than re-resolving a moving default-branch name.

Inputs that a Codex skill must read from its shell commands must be serialized
to an ignored workspace file. Do not rely on launcher environment variables
remaining visible inside sandboxed Codex tool commands.

The local Codex runner action may accept an API key through its interface, but
only its proxy startup step may receive the key. Its Codex execution step must
not inherit it.

The runner action owns only Codex runtime preparation and execution. Keep its
prompt caller-supplied and protocol-agnostic; the selected skill owns protocol
decisions and implementation.

Scheduled protocol synchronization is best effort. Keep non-Codex orchestration
linear and let its failures surface through the job. The Codex invocation may
perform bounded capacity retries by resuming its current non-ephemeral session;
other failures must surface without fallback.

Treat a Codex final-message state as a claim, not sufficient publication
evidence. Before validation or publication, cross-check it against the selected
workflow mode and the observed worktree state, and fail closed on mismatch.
