# Issue tracker: GitHub

Issues and implementation tickets for this repository live in GitHub Issues.
Use the `gh` CLI for tracker operations and infer the repository from the local
Git remote.

## Conventions

- Create one issue per independently implementable ticket.
- Apply `ready-for-agent` only after scope, acceptance criteria, and blocking
  edges are explicit.
- Use GitHub native issue dependencies for blocking edges when available;
  otherwise keep a `Blocked by` section in the issue body.
- Do not close or modify a parent issue when implementing a child ticket.
- Pull requests are not a request or triage surface.

## Skill operations

When an engineering skill says to publish a ticket, create a GitHub issue in
this repository. When it says to fetch a ticket, read the full issue body,
labels, dependencies, and comments before acting.
