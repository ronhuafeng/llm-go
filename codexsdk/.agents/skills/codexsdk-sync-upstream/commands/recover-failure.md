# Command: recover-failure

State:
- Sync PR check, open sync PR state, finalize step, drift verification, or sync tag operation failed.

Inputs:
- Failure type, PR/branch, CI/run ID, target provenance, and landed commit when relevant.

Evidence:
- `../references/recovery.md`
- GitHub PR/check/run/tag state when caller owns remote inspection.

Boundaries:
- Preserve useful failure evidence before reruns or cleanup.
- May inspect remote state and run documented recovery recipes.
- May rerun an expected `action_required` `GITHUB_TOKEN` PR check when caller owns that recovery.
- May create documented follow-up commits or suffix sync tags only through protected recipes.
- Must not weaken protection, bypass checks, introduce tokens, synthesize statuses, force-push `main`, delete/move tags, or add new automation before trying recovery recipes.

Checks:
- Recovery action stays on the protected PR path.
- Required real checks remain authoritative.
- Tags and branch protection are unchanged except documented allowed actions.

Output:
- Failure classification, evidence collected, recovery action taken, remaining blockers, next command, and completion layer.

Stop if:
- Recovery would require bypassing protection or moving/deleting tags.
- Evidence is insufficient to classify the failure.
- The same blocker persists after documented recovery steps.
