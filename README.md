<p align="center">
  <img src="./docs/assets/scriptorium-logo-transparent.png" alt="scriptorium" width="360">
</p>

# scriptorium

English | [日本語](./README.ja.md)

`scriptorium` is an MCP runtime for grounded Markdown and code retrieval. This repository contains the Go implementation, release packaging, and artifact builders for the runtime.

## Background

Even when library documentation and sample code are well maintained, AI-driven usage has often not worked well enough in practice.

AI tends to scan broad portions of the documentation on each request, making it difficult to retrieve only the necessary information efficiently. This also creates problems for consistency and reproducibility in the referenced sources.

In addition, even when documentation, sample code, and related materials are available, information that is understandable for humans is not always structured in a way that AI can handle effectively.

As a result, teams can end up in a state where "the information exists, but it cannot be used well."

`scriptorium` was developed to address this problem by treating documentation, sample code, and implementation knowledge as a single connected source of truth that AI can reference naturally.

`scriptorium` is an MCP runtime that analyzes Markdown and code together and attaches grounded references to the sources behind search, retrieval, and implementation-guide generation results.

This makes the origin of retrieved information traceable and helps preserve reproducibility and verifiability in development workflows, including AI-assisted ones.

It also allows users to access the knowledge they need in a consistent way without having to think explicitly about where the information lives or how it should be referenced.

## Runtime Docs

Current runtime and artifact behavior is documented in:

- [Runtime Contract](./docs/runtime-contract.md)
- [Retrieval Semantics](./docs/retrieval-semantics.md)
- [Artifacts](./docs/artifacts.md)

Product-facing behavior should follow the runtime docs above.

## Community

- Contribution guide: [CONTRIBUTING.md](./CONTRIBUTING.md)
- Japanese contribution guide: [CONTRIBUTING.ja.md](./CONTRIBUTING.ja.md)
- Code of Conduct: [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md)
- Japanese Code of Conduct: [CODE_OF_CONDUCT.ja.md](./CODE_OF_CONDUCT.ja.md)
- Security policy: [SECURITY.md](./SECURITY.md)
- Japanese security policy: [SECURITY.ja.md](./SECURITY.ja.md)
- Support policy: [.github/SUPPORT.md](./.github/SUPPORT.md)
- Japanese support policy: [.github/SUPPORT.ja.md](./.github/SUPPORT.ja.md)
- Changelog: [CHANGELOG.md](./CHANGELOG.md)
- Japanese changelog: [CHANGELOG.ja.md](./CHANGELOG.ja.md)
- License: [LICENSE](./LICENSE)
- Japanese license reference: [LICENSE.ja.md](./LICENSE.ja.md)
- Third-party notices: [THIRD-PARTY-NOTICES.md](./THIRD-PARTY-NOTICES.md)

## Releases

Annotated tags in the format `v<major>.<minor>.<patch>` are used to publish
GitHub Releases.

Versioned release notes are maintained in [CHANGELOG.md](./CHANGELOG.md).

The release workflow builds versioned archives for:

- Linux `amd64`, `arm64`
- Windows `amd64`, `arm64`
- macOS `amd64`, `arm64`

Linux release archives contain `scriptorium`, `scriptorium-index`,
`scriptorium-snapshot`, `scriptorium.sbom.spdx.json`, `LICENSE`,
`THIRD-PARTY-NOTICES.md`, and user-facing Linux setup guides under
`docs/`.

Windows release archives contain `scriptorium.exe`,
`scriptorium-index.exe`, `scriptorium-snapshot.exe`,
`scriptorium.sbom.spdx.json`, `LICENSE`, `THIRD-PARTY-NOTICES.md`, and
user-facing Windows setup guides under `docs/`.

macOS release archives contain the platform binaries together with the
repository README files, `scriptorium.sbom.spdx.json`, `LICENSE`, and
`THIRD-PARTY-NOTICES.md`.

Normal CI validates pull requests and non-tag pushes on Linux, Windows, and macOS.

## Runtime Capabilities

- Current implementation includes:
  - MCP runtime entrypoint
  - docs index builder entrypoint
  - git snapshot builder entrypoint
  - runtime configuration loading
  - `refId` parsing and spec-compliant heading slug generation
  - tool result envelope generation
  - diagnostics payload scaffolding
  - display-path-based filesystem guarding and deterministic file scanning
  - text decoding for UTF-8, UTF-16, and Shift_JIS-style fallbacks
  - Markdown heading-block parsing and docs filesystem fallback scanning
  - runtime search semantics for docs/code ranking with heading/path boosts, code path fallback, and clustered `code_range` results
  - initial `get_content` semantics for markdown, code, and file refs
  - initial `expand_related` semantics for filesystem-backed docs and sample roots
  - initial `summarize_flow` semantics for filesystem-backed markdown and code refs
  - additive `guide_implementation` MCP tool that composes retrieval and flow summarization into grounded implementation guidance
  - SQLite/FTS-backed docs index builder/runtime support with auto-discovery, verification, and stale fallback
  - SQLite/FTS-backed git snapshot builder/runtime support for snapshot-backed code search and content resolution
  - acceptance-focused verification for documented runtime scenarios
  - MCP stdio transport with `initialize`, `ping`, `tools/list`, and `tools/call`

## Setup

### 1. Prerequisites

- Obtain the distributed binaries for your OS.
- Or download a published release archive from GitHub Releases.
- Prepare the Markdown directory to index and search.
- If you use Git snapshots, make sure the `git` command is available.

The distribution is expected to contain at least these three executables:

- `scriptorium`
- `scriptorium-index`
- `scriptorium-snapshot`

On Windows they are typically distributed with `.exe`.

### 2. Unpack the distribution

```powershell
mkdir scriptorium
cd scriptorium
# Place the distributed binaries here
```

### 3. Build a docs index

If you want faster docs search, generate the SQLite index first.

```powershell
.\scriptorium-index.exe --docs-root .\docs --out .\scriptorium-index.sqlite
```

macOS / Linux:

```bash
./scriptorium-index --docs-root ./docs --out ./scriptorium-index.sqlite
```

### 4. Build a Git snapshot

If you want snapshot-backed code search, generate a snapshot from the target repository.

```powershell
.\scriptorium-snapshot.exe --repo . --out .\scriptorium-snapshot.sqlite --samples HEAD --code-extensions .go,.ts,.cs
```

macOS / Linux:

```bash
./scriptorium-snapshot --repo . --out ./scriptorium-snapshot.sqlite --samples HEAD --code-extensions .go,.ts,.cs
```

Typical `--samples` values:

- `HEAD`: snapshot the current HEAD
- `WORKTREE`: snapshot the working tree
- `feature/foo`: snapshot a specific branch or ref

`--samples ALL` expands local branches only. It does not automatically include remote-tracking refs such as `origin/feature/foo`, even when `--fetch` is enabled.

### 5. Configure the environment

The required configuration is `SCRIPTORIUM_MARKDOWN_DIR`. `SCRIPTORIUM_CODE_ROOTS` enables direct filesystem-backed code search, and `SCRIPTORIUM_INDEX_FILE` / `SCRIPTORIUM_SNAPSHOT_FILE` enable prebuilt artifacts. Path-bearing list variables use the host OS path-list separator (`;` on Windows, `:` on macOS / Linux) or newlines, so paths containing spaces remain intact.

```powershell
$env:SCRIPTORIUM_MARKDOWN_DIR = (Resolve-Path .\docs)
$env:SCRIPTORIUM_INDEX_FILE = (Resolve-Path .\scriptorium-index.sqlite)
$env:SCRIPTORIUM_SNAPSHOT_FILE = (Resolve-Path .\scriptorium-snapshot.sqlite)
$env:SCRIPTORIUM_CODE_EXTENSIONS = ".go,.ts,.cs"
$env:SCRIPTORIUM_INDEX_VERIFY = "full"
```

Optional MCP identity metadata can make the server easier for AI clients to select. `SCRIPTORIUM_SERVER_NAME` overrides `serverInfo.name`, `SCRIPTORIUM_MCP_PROFILE` provides a profile identifier, `SCRIPTORIUM_MCP_TOOL_PREFIX` prefixes advertised tool names, and `SCRIPTORIUM_MCP_DOMAIN_DESCRIPTION` / `SCRIPTORIUM_MCP_CORPUS_SUMMARY` make tool descriptions more specific. `SCRIPTORIUM_MCP_EXAMPLE_QUERIES` accepts newline-delimited examples that show up in diagnostics.

Example with multiple artifacts:

```powershell
$env:SCRIPTORIUM_INDEX_FILE = @(
  (Resolve-Path .\artifacts\docs-a.sqlite)
  (Resolve-Path .\artifacts\docs-b.sqlite)
) -join [IO.Path]::PathSeparator
$env:SCRIPTORIUM_SNAPSHOT_FILE = @(
  (Resolve-Path .\artifacts\repo-a.sqlite)
  (Resolve-Path .\artifacts\repo-b.sqlite)
) -join [IO.Path]::PathSeparator
```

Example with direct filesystem sample roots:

```powershell
$env:SCRIPTORIUM_CODE_ROOTS = (Resolve-Path .\src)
```

### 6. Start the MCP server

```powershell
.\scriptorium.exe
```

After startup it accepts `initialize`, `tools/list`, and `tools/call` over stdio transport.

macOS / Linux:

```bash
./scriptorium
```

### 7. Validate the runtime

Call `diagnostics` first to inspect docs index, git snapshot, and cache state.

Recommended checks:

- `docs.docsOnlyMode`
- `docs.index.enabled`
- `code.gitSnapshot.enabled`
- `caches.textFiles`
- `caches.sampleRoots`
- `retrieval.corpus`
- `retrieval.fallback`
- `retrieval.warnings`

Recommended MCP tools:

- `diagnostics` for runtime state and cache visibility
- `search`, `get_content`, `expand_related`, and `summarize_flow` for primitive retrieval workflows
- `guide_implementation` when the client wants a single grounded implementation guide with supporting docs and sample-code refs

Implementation-guidance retrieval:

- `search` accepts additive `mode=implementation` to enable framework-aware ranking and docs/code diversification.
- `expand_related` accepts additive `mode=implementation` and the `framework_links` signal for startup wiring, DI, route, attribute/decorator, and config-style relations.
- `guide_implementation` uses those implementation-aware retrieval hints internally.

## Docs Authoring Guide

If you want to shape docs for better `scriptorium` parsing and retrieval, see [MARKDOWN_AUTHORING_GUIDE.md](./MARKDOWN_AUTHORING_GUIDE.md).

The most important points are:

- Put one implementation topic in each heading block.
- Use numbered lists for procedural steps.
- Keep API names, config keys, routes, type names, and file names literal.
- Keep important examples in the same heading block as the explanation.

## Work Tracking

- Open engineering work is tracked in GitHub Issues.

## Test Coverage

- Coverage command:
  - PowerShell: `./scripts/test-coverage.ps1`
  - Bash: `./scripts/test-coverage.sh`
- The coverage scripts run package-by-package `go test -coverprofile` commands, merge them into `coverage.out`, and print the `go tool cover -func` summary.
- Current merged statement coverage: `99.2%`.
- Packages with the most remaining room are `internal/ref` (`98.2%`), `internal/docsindex` (`98.5%`), `internal/content` (`98.7%`), `internal/related` (`98.7%`), and `internal/gitsnapshot` (`98.9%`).

## Historical Test Audit

- Historical test-audit notes from the migration era are not part of the active workflow.
- Coverage mapping is tracked by scenario rather than by file name because repository tests are organized by package.

## Snapshot Builder Notes

- The git snapshot builder now applies the same size, ignored-directory, and fallback-decoding rules during snapshot materialization that runtime filesystem sample roots use, and follows the same display-path-based symlink policy when symlinks are enabled.
- Build-time fetch is intentionally simple: `--fetch` enables it, `--fetch-on-start` decides whether it runs before snapshot generation, and `--fetch-remote` selects the remote when enabled.

## Diagnostics Notes

- `diagnostics.caches.textFiles` now reflects the live filesystem text cache used by docs and filesystem-backed code reads.
- `diagnostics.code.sampleSources` now lists both filesystem roots and snapshot-backed roots so runtime source coverage is visible without cross-checking `gitSnapshot.roots`.
- `diagnostics.server` now reports `runtime=go` and `runtimeVersion` only.
- `diagnostics.caches.textFiles` includes `evictions`.
- `diagnostics.caches.sampleRoots` now reflects the live runtime code-source cache used to resolve filesystem and snapshot-backed code sources, with `evictions`, `filesystemRoots`, and `snapshotRoots` counters for the current cache contents.

## Dependencies

- The repository now uses `modernc.org/sqlite` as a pure-Go SQLite driver so the docs index and git snapshot artifacts can match the original SQLite/FTS architecture without adding a CGO requirement.

## Package Shape

- `cmd/scriptorium`: runtime server source entrypoint for the distributed `scriptorium` binary
- `cmd/build-docs-index`: docs index builder source entrypoint for the distributed `scriptorium-index` binary
- `cmd/build-git-snapshot`: git snapshot builder source entrypoint for the distributed `scriptorium-snapshot` binary
- `internal/app`: command orchestration
- `internal/config`: environment and runtime configuration
- `internal/content`: `get_content` request handling and range selection
- `internal/diagnostics`: diagnostics payload
- `internal/docs`: Markdown block parsing and docs filesystem scanning
- `internal/docsindex`: docs index artifact build/load/verify helpers
- `internal/filesafe`: guarded filesystem access and deterministic scans
- `internal/flow`: `summarize_flow` extraction, merge, and confidence logic
- `internal/gitsnapshot`: snapshot artifact build/load helpers
- `internal/protocol`: tool result envelope helpers
- `internal/related`: `expand_related` request handling and scoring
- `internal/ref`: path normalization and `refId` handling
- `internal/search`: query normalization and filesystem-backed search helpers
- `internal/source`: shared filesystem-backed code source resolution
- `internal/textdecode`: spec-oriented text decoding helpers
- `internal/textutil`: shared line normalization, splitting, and centered range helpers
