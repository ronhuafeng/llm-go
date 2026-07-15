# AGENTS.md

## Workflow safety

A `run` step before checkout must not inherit a workflow-level non-root
`defaults.run.working-directory`; set the step's `working-directory` to
`${{ github.workspace }}` explicitly.

Default-branch control-plane workflows must check out the triggering trusted
`${{ github.sha }}` rather than re-resolving a moving default-branch name.
