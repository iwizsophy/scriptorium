# Artifacts

This document describes the build-time and runtime contract for `scriptorium` artifacts.

For the runtime server surface, see [Runtime Contract](./runtime-contract.md). For search/content semantics, see [Retrieval Semantics](./retrieval-semantics.md).

## Docs Index

### Builder CLI

`scriptorium-index` builds a SQLite/FTS artifact for Markdown docs search.

Supported flags:

- `--docs-root` (required): Markdown docs root
- `--out` (required): output artifact path
- `--allow-symlinks`: allow display-path-based symlink traversal during build

### Stored Information

The docs index stores:

- artifact metadata such as schema version, generation time, docs root, file/block counts, and source fingerprint
- Markdown file records
- heading-block records
- an FTS table used for ranked block lookup

### Runtime Selection

At runtime the docs index can be supplied explicitly through `SCRIPTORIUM_INDEX_FILE`.

- multiple artifacts are allowed
- explicit paths are loaded in the listed order
- all explicit artifacts must load and verify successfully or the runtime falls back to filesystem scan

When no explicit path is set, the runtime probes:

1. `<docs-root>/scriptorium-index.sqlite`
2. `<docs-root>/../scriptorium-index.sqlite`
3. `<docs-root>/../../scriptorium-index.sqlite`

### Verification

`SCRIPTORIUM_INDEX_VERIFY` controls verification:

- `full`: docs root and source fingerprint must match
- `mtime`: docs root, file count, and max mtime must match
- `off`: skip verification

Verification mismatch disables docs index usage and falls back to filesystem scan.

## Git Snapshot

### Builder CLI

`scriptorium-snapshot` builds a SQLite/FTS artifact for code search.

Supported flags:

- `--repo` (required): Git repository path
- `--out` (required): output artifact path
- `--samples` (required): sample selectors
- `--code-extensions`: extension filter list
- `--fetch`: enable `git fetch` before build
- `--fetch-on-start`: fetch before materialization when `--fetch` is enabled
- `--fetch-remote`: remote name for build-time fetch
- `--allow-symlinks`: allow display-path-based symlink traversal during build
- `--max-file-bytes`: maximum file size included in the snapshot

### Sample Selectors

`--samples` accepts one or more selectors separated by semicolons or newlines.

Common forms:

- `HEAD`
- `WORKTREE`
- `feature/foo`
- `alias=feature/foo`
- `*` or `ALL` to expand all branches

`ALL` expands local branches from `refs/heads`.
Remote-tracking refs such as `origin/feature/foo` are not included automatically.
`--fetch` can refresh remote state before the build, but it does not change `ALL` into a remote-branch selector.

Each selector becomes a snapshot root with a stable root id and logical path prefix.

### Stored Information

The snapshot artifact stores:

- artifact metadata such as schema version, repo path, generation time, root count, file count, and fingerprint
- snapshot roots with ids, descriptions, and labels
- code entries with logical path, relative path, extension, line count, and content
- an FTS table used for ranked code search

### Runtime Selection

At runtime snapshots are supplied through `SCRIPTORIUM_SNAPSHOT_FILE`.

- multiple snapshot artifacts are allowed
- all listed artifacts must load successfully
- snapshot root ids must remain unique across all loaded artifacts

If any artifact is missing, invalid, or introduces a duplicate root id, snapshot-backed code search is disabled for the runtime and a warning is reported through diagnostics/logging.

## Write Strategy

Both artifact builders publish SQLite files atomically:

- write to a sibling temp file
- close the database
- replace the target path in a final publish step

This avoids partially written artifacts at the final target path.
