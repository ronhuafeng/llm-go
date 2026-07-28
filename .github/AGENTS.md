# AGENTS.md

## Workflow safety

A `run` step before checkout must not inherit a workflow-level non-root
`defaults.run.working-directory`; set the step's `working-directory` to
`${{ github.workspace }}` explicitly.

Default-branch control-plane workflows must check out the triggering trusted
`${{ github.sha }}` rather than re-resolving a moving default-branch name.

Inputs that a Codex skill must read from its shell commands must be serialized
to an ignored workspace file. Do not assume caller `env` on
`openai/codex-action` remains visible inside Codex tool commands.

Treat a Codex final-message state as a claim, not sufficient publication
evidence. Before validation or publication, cross-check it against the selected
workflow mode and the observed worktree state, and fail closed on mismatch.
