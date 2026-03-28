# Contributing

Thanks for your interest in contributing to `scriptorium`.

## Before you start

- For behavior and compatibility work, consult repository docs in this order:
  `docs/runtime-contract.md`, `docs/retrieval-semantics.md`,
  `docs/artifacts.md`, `docs/repository-governance.md`, `AGENTS.md`,
  `README.md`.
- Use GitHub Issues for bug reports, feature requests, and public design
  discussion when appropriate.
- Open an issue before large changes, public behavior changes, architecture
  changes, release-process changes, or new dependency proposals.
- Keep changes focused and small when possible.
- The default branch is `main`. Unless maintainers say otherwise, open pull
  requests against `main`.
- Release tags must be annotated tags in the format
  `v<major>.<minor>.<patch>` and should point to commits already reachable from
  `main`.
- The expected CI checks are the test matrix, `coverage`, and `build`.
- New third-party dependencies and major dependency updates require an issue and
  an explicit rationale.
- When behavior changes, update the active runtime docs and relevant tests in the
  same change whenever practical.
- User-facing docs should keep English and Japanese counterparts when practical.

## Development workflow

1. Fork the repository and create a topic branch.
2. Open or update an issue when the work changes behavior, architecture, or
   repository policy.
3. Implement the change with matching tests or documentation updates.
4. Run local validation from the repository root:
   - `go test ./...`
   - `go build ./cmd/...`
   - PowerShell coverage: `./scripts/test-coverage.ps1`
   - Bash coverage: `./scripts/test-coverage.sh`
5. Submit a pull request with:
   - What changed
   - Why it changed
   - Validation results

Direct pushes to `main` should be avoided outside maintainer-controlled release
or emergency situations.

## Repository expectations

- Preserve externally observable behavior defined by the runtime contract docs
  unless the change intentionally updates that contract.
- Keep runtime, search, index, and snapshot responsibilities separated.
- Avoid opportunistic refactors that are not required for correctness,
  maintainability, or testability.
- Document assumptions when the active runtime docs are silent, and cite any
  historical reference used.

## Additional docs

- User guide: `README.md`
- Japanese user guide: `README.ja.md`
- Runtime contract docs: `docs/runtime-contract.md`,
  `docs/retrieval-semantics.md`, `docs/artifacts.md`
- Repository governance: `docs/repository-governance.md`
- Repository policy for coding agents: `AGENTS.md`
- Markdown authoring guide: `MARKDOWN_AUTHORING_GUIDE.md`
- Japanese Markdown authoring guide: `MARKDOWN_AUTHORING_GUIDE.ja.md`
- Code of Conduct: `CODE_OF_CONDUCT.md`
- Security policy: `SECURITY.md`
- Support policy: `.github/SUPPORT.md`
- License: `LICENSE`
