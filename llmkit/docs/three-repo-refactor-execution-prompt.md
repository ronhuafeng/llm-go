# Three-Repository Refactor Orchestration Prompt

Coordinate the three repository-local refactors through their own authoritative
plans and execution prompts:

- `llmkit-go`:
  [plan](https://github.com/ronhuafeng/llmkit-go/blob/main/docs/v0.2-refactor-plan.md),
  [prompt](https://github.com/ronhuafeng/llmkit-go/blob/main/docs/v0.2-refactor-execution-prompt.md)
- `codexsdk-go`:
  [plan](https://github.com/ronhuafeng/codexsdk-go/blob/main/docs/v0.2-refactor-plan.md),
  [prompt](https://github.com/ronhuafeng/codexsdk-go/blob/main/docs/v0.2-refactor-execution-prompt.md)
- `llmcaller-codex-go`:
  [plan](https://github.com/ronhuafeng/llmcaller-codex-go/blob/main/docs/v0.2-refactor-plan.md),
  [prompt](https://github.com/ronhuafeng/llmcaller-codex-go/blob/main/docs/v0.2-refactor-execution-prompt.md)

This prompt contains no API spec. Do not reconstruct one from this index or an
older conversation. For each repository, read and follow its local prompt and
complete local plan.

## Order

1. Recheck all three worktrees and contract mirrors.
2. Execute the independent `llmkit-go` and `codexsdk-go` foundation work.
3. Validate both standalone with `GOWORK=off` and complete release readiness.
4. Stop at tag/publish gates unless explicitly authorized.
5. Execute the caller's dependency-independent schema work at any time.
6. Execute the caller's permanent API join only after real upstream target tags
   resolve without replace directives.
7. Run standalone, minimum, all-heads, and real tag canaries separately.

## Non-Negotiable Checks

- The caller's `contract:llmkit-caller` mirror is byte-identical to llmkit's.
- The caller's `contract:codexsdk-caller` mirror is byte-identical to the SDK's.
- Every SDK spec reference to a generated type exists in current generated code.
- Generated outputs are changed only through generators.
- Each repository enforces its handwritten exports separately.
- No local path, `replace`, or committed development `go.work` masks a missing
  released API.
- No repository claims completion while its local plan has an unmet requirement.

Do not push, tag, publish, create remote PRs, or overwrite unrelated user changes
without explicit authorization.
