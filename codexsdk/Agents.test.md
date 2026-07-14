# Agent Test Rules

Use tests to protect behavior and contracts, not to mirror the current implementation.

## Core Rule

Before adding or changing a test, ask:

```text
Would this test still be meaningful if the implementation were replaced with a different correct implementation?
```

If the answer is no, do not add the test unless it is explicitly a narrow golden or snapshot test for stable generated formatting.

## Valid Tests

A valid new or changed test should satisfy at least one of these:

- It would fail on the known broken behavior before the fix.
- It asserts an externally meaningful contract: input semantics, generated API behavior, workflow behavior, compatibility behavior, or error behavior.
- It protects a regression class that could realistically recur across future inputs or versions.
- It uses small synthetic inputs to isolate a generic rule.
- It derives expected values from a source of truth, not from the current implementation output.

## Tests To Avoid

Avoid tests whose main assertion is:

- fixed object, type, method, or field counts
- broad snapshots of generated files
- exact helper internals
- current ordering, unless ordering is part of the public contract
- output equality against whatever the current generator or implementation emits
- path/name-specific behavior when the real rule should be semantic
- thresholds chosen only to make the current implementation pass

## Generated Code

For generated code, prefer semantic tests over output-shape tests:

- Test the classifier or planner with small synthetic inputs.
- Derive expected outputs from explicit source-of-truth inputs.
- Assert unsupported inputs fail with precise diagnostics.
- Keep golden tests narrow and reserved for stable formatting or public compatibility promises.

Do not use generated output from the current implementation as the primary oracle for correctness.

## Public API And Integration Tests

For public API and integration behavior:

- Test the externally visible contract.
- Derive expected surface area from the declared source of truth.
- Avoid direct coupling to private helpers unless the helper itself owns a stable contract.
- Prefer absence/presence checks only when they follow from a declared manifest, schema, registry, capability list, or compatibility policy.

## Replacing Shallow Tests

Do not weaken, delete, or rewrite tests only to match a new implementation.

If an existing test is implementation-coupled, replace it with a semantic test and explain:

- what contract the replacement protects
- what source of truth defines the expected result
- why the replacement is less coupled to implementation details

If a threshold changes, justify the new threshold from an external contract or computed source of truth.

## Final Report Requirement

For each non-trivial new or changed test, report:

- what bug class or contract it protects
- what source of truth defines the expected result
- why it is not merely encoding the current implementation
