# Repository Governance

This document is the active repository-level governance reference for `scriptorium`.

For runtime behavior, use the runtime contract docs:

- [Runtime Contract](./runtime-contract.md)
- [Retrieval Semantics](./retrieval-semantics.md)
- [Artifacts](./artifacts.md)

## Active Source Of Truth

Use repository documents in this order:

1. runtime contract docs for externally observable runtime behavior
2. this document for current engineering workflow and repository policy
3. [AGENTS.md](../AGENTS.md) for coding-agent execution rules
4. contributor-facing docs such as [CONTRIBUTING.md](../CONTRIBUTING.md) and [README.md](../README.md)

## Repository Policy

- Keep runtime, search, docs index, and git snapshot responsibilities separated.
- Update behavior docs, operator docs, and issue files in the same work item when behavior or policy changes.
- Large or non-trivial work must have a matching markdown issue under `issues/`.
- Validation is required before work is treated as complete.
- Coverage work should aim for 100%; only technically unstable branches may remain uncovered, and they must carry a nearby `COVERAGE_EXCEPTION` comment.
- When issue scope is complete, mark the issue `done` before starting the next issue.
- Keep commits scoped to a single issue or policy work item.

## Historical Issue Policy

- Existing issues may contain migration-era references.
- Do not rewrite old issues just to remove historical context.
- When active work touches an old issue, add a clarification note only if the historical wording would otherwise be ambiguous.
