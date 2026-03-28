# Agent Contract

All AI coding agents working in this repository MUST follow these rules:

1. Treat the active runtime contract docs as the primary source of truth for behavior and compatibility:
   - [docs/runtime-contract.md](./docs/runtime-contract.md)
   - [docs/retrieval-semantics.md](./docs/retrieval-semantics.md)
   - [docs/artifacts.md](./docs/artifacts.md)
2. Treat [docs/repository-governance.md](./docs/repository-governance.md) as the primary source of truth for current engineering workflow and repository policy.
3. Do not treat migration-era porting documents as part of the active engineering workflow; rely on the current runtime and governance docs instead.
4. Do not implement behavior based on unstated assumptions. When the active runtime/governance docs are silent, document the assumption in code comments, tests, or follow-up notes and note any historical reference used.
5. Favor parity with the documented runtime contract and preserved historical compatibility over intentionally minimal substitute implementations. Small slices are acceptable only when they clearly converge toward current product behavior, fidelity, and operational characteristics.
6. Track non-trivial work items in GitHub issues. Large changes must have an open issue before or with the implementation change.
7. Code must not be treated as complete without validation. Add or update tests when practical, and always record what was or was not verified.
8. Public tool contracts, `refId` rules, search semantics, diagnostics shape, and safety constraints from the active runtime docs must remain explicit in code and documentation.
9. Do not introduce local fixes that break the intended architecture around runtime/search/index/snapshot responsibility boundaries.
10. Before modifying more than 3 files, explain the plan and the components affected.
11. Avoid opportunistic refactoring outside the requested scope unless correctness, testability, or architectural consistency requires it.
12. Update related documentation when repository policy, behavior, compatibility assumptions, or issue tracking rules change.
13. If a branch is left intentionally uncovered because it is not stably testable in local/CI conditions, document the reason in a nearby code comment containing the exact keyword `COVERAGE_EXCEPTION` so exclusions remain searchable and auditable.
14. Repository work should aim for 100% coverage. If a branch is technically testable, add the test even when the setup is awkward or implementation effort is non-trivial.
15. The only acceptable reason to leave a branch uncovered is that stable verification is not technically feasible in local/CI conditions; in that case use `COVERAGE_EXCEPTION` with a concrete technical reason in nearby code.
16. Preserve the intended architecture when implementing changes; do not favor expedient local implementations that cut across responsibility boundaries or bypass the runtime design.
17. Keep repository documentation current with the codebase and issue policy state; update affected docs in the same work item when behavior, policy, or operator guidance changes.
18. When an issue's scope is complete, mark the issue done/closed before moving on to the next issue.
19. Keep commits scoped to a single issue or policy work item; do not mix unrelated issues into the same commit.
20. When new follow-up work, open questions, or carry-over tasks are discovered, create a new GitHub issue and record the remaining work before closing the current issue.

If governance documents conflict, follow this order:

`docs/runtime-contract.md`, `docs/retrieval-semantics.md`, `docs/artifacts.md` -> `docs/repository-governance.md` -> `AGENTS.md`

Within that framework, use this implementation decision order:

Specification correctness -> Compatibility -> Safety -> Maintainability -> Testability -> Simplicity

## Workflow

Development should generally follow this sequence:

1. Read the relevant active runtime/governance docs.
2. If active docs are silent and compatibility matters, document the assumption in the issue, tests, code comments, or follow-up notes before implementing.
3. Identify the impact scope.
4. Confirm or create the relevant GitHub issue.
5. Design the smallest change that still moves the repository toward documented runtime behavior and preserved compatibility.
6. Write or update tests where useful.
7. Implement the change in Go.
8. Run validation.
9. Update documentation or notes if behavior changed.

## Large Change Rule

A change is considered large if any of the following applies:

- It changes a public MCP tool contract or externally visible behavior
- It changes compatibility requirements from the active runtime contract docs
- It changes architecture or responsibility boundaries
- It spans multiple layers such as runtime, indexing, and snapshot generation
- It is expected to modify 7 or more files

Changes affecting 4 to 6 files require a written change plan before implementation even when they are not classified as large.

## Dependency Policy

- Prefer the Go standard library unless a third-party dependency materially improves correctness or implementation cost.
- New third-party dependencies require an explicit reason in the change summary or related documentation.
- Favor permissive licenses such as MIT, Apache-2.0, and BSD.
- If a dependency inventory file is introduced later, keep it updated together with dependency changes.

## Repository Notes

- Work items are tracked in GitHub issues.
- Public issue and pull request intake lives under `.github/`, and active implementation tracking also uses GitHub issues.
- CI and release automation now live under `.github/workflows`.
- User-facing repository docs now include `README*`, `CONTRIBUTING*`, `CODE_OF_CONDUCT*`, `SECURITY*`, license files, and GitHub support policy files.
- Keep this file aligned when repository policy, community health files, or workflow assumptions change.
