# AGENTS.md

## Workflow working directories

A `run` step before checkout must not inherit a workflow-level non-root
`defaults.run.working-directory`; set the step's `working-directory` to
`${{ github.workspace }}` explicitly.
