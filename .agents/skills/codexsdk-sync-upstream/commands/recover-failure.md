# Command: recover-failure

State:
- Candidate apply or local validation failed before protocol implementation completed.

Inputs:
- Failure type, target provenance, apply or validation evidence, and current changed paths.

Boundaries:
- Preserve useful failure evidence before reruns or cleanup.
- For candidate apply failures caused by an unsupported schema shape, may update the smallest focused generator rule and tests justified by drift evidence, restore only failed-attempt mechanical outputs to their captured pre-apply state, then return to `apply-candidate`.
- For validation failures, inspect and fix the first local implementation error, then rerun the focused failing check before full validation.
- Repeat recovery only when the next failure is newly evidenced and the preceding focused repair passed. Do not retry an unchanged failure without new evidence.
- Must not configure authentication, stage, commit, push, create PRs, tag, dispatch workflows, or inspect remote recovery state.

Checks:
- Recovery stays within the reviewed local implementation scope.
- Candidate provenance and target SHA remain unchanged.
- Every retry preserves the same candidate provenance and demonstrates focused forward progress.

Output:
- Failure classification, evidence collected, local recovery action, validation result, remaining blockers, and next local command.

Stop if:
- Evidence is insufficient to classify the failure.
- The same blocker persists after documented recovery steps.
- The next repair would leave the reviewed local implementation scope or require remote side effects.
