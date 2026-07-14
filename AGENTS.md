# AGENTS.md

## Friction-to-Knowledge Loop

Treat project docs as working memory: verified lessons should be captured close
to where future work will need them, not kept only in the current conversation.

When a user correction, repeated friction, or failed validation suggests a reusable lesson:

1. Treat it as evidence, not an automatic rule.
2. Classify it as one-off, project-local, or broadly reusable.
3. Verify with the closest source, code, tool, or failing validation.
4. Draft the smallest wording and scope that would prevent the same miss.
5. Update the closest working-memory doc when the lesson is verified and local.
6. Ask the user before promoting it into a broader rule, global instruction, skill, or helper.
7. Run lightweight validation when a document, skill, helper, or rule changes.
8. Promote only when repeated, broadly applicable, or mechanically enforceable.

## Agent skills

### Issue tracker

Issues live in this repository's GitHub Issues. See `docs/agents/issue-tracker.md`.

### Domain docs

This is a multi-context repository. Start with `CONTEXT-MAP.md` and follow the
module context and ADR links described in `docs/agents/domain.md`.
