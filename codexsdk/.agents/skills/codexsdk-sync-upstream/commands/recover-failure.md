# Command: recover-failure

State:
- Candidate apply or local validation failed before protocol implementation completed.

Inputs:
- Failure type, target provenance, apply or validation evidence, and current changed paths.

Boundaries:
- Preserve useful failure evidence before reruns or cleanup.
- For candidate apply failures caused by an unsupported schema shape, may update the smallest focused generator rule and tests justified by drift evidence, restore only failed-attempt mechanical outputs to their captured pre-apply state, then return to `apply-candidate` for one retry.
- For validation failures, inspect and fix the first local implementation error, then rerun the focused failing check before full validation.
- Must not configure authentication, stage, commit, push, create PRs, tag, dispatch workflows, or inspect remote recovery state.

Checks:
- Recovery stays within the reviewed local implementation scope.
- Candidate provenance and target SHA remain unchanged.
- Apply is retried at most once.

Output:
- Failure classification, evidence collected, local recovery action, validation result, remaining blockers, and next local command.

Stop if:
- Evidence is insufficient to classify the failure.
- The same blocker persists after documented recovery steps.
