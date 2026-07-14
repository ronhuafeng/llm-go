# Command: validate-local

State:
- Local checked-in sync surface needs validation before commit, publish, or final report.

Inputs:
- Candidate schema directory when available.
- Target SHA and current checked-in baseline.

Tools:
- `scripts/codexsdk_validate_sync.sh`
- Focused tests, `gofmt`, generator reproduction, sync-state, and path-sanitization checks when they match changed files.

Boundaries:
- May run local validation and formatting/check commands.
- Must not commit, push, tag, create PRs, request merges, change branches, or publish remote state.

Checks:
- Report exact commands and pass/fail results.
- Confirm no local absolute paths or cache output paths leaked into checked-in baseline metadata or checked-in reports.

Output:
- Validation result, commands run, blockers if any, and highest completed local layer.

Stop if:
- Validation fails; report the first actionable failure.
- Validation inputs do not match current baseline provenance.
