# Runtime Contract

This document is the current product-facing runtime contract for `scriptorium`.

The runtime behavior clients and operators should rely on is documented here and in the companion docs:

- [Retrieval Semantics](./retrieval-semantics.md)
- [Artifacts](./artifacts.md)

## Components

`scriptorium` ships as three command-line programs:

| Component | Purpose |
| --- | --- |
| `scriptorium` | MCP runtime server over stdio |
| `scriptorium-index` | Prebuild a Markdown search artifact |
| `scriptorium-snapshot` | Prebuild a Git-backed code search artifact |

## Runtime Server

- Transport: stdio
- Stdio requests: newline-delimited JSON-RPC and `Content-Length` framed JSON-RPC are both accepted
- Stdio responses: the runtime mirrors the request framing mode for compatibility
- Server version: `1.0.0`
- Default server name: `scriptorium`; when `SCRIPTORIUM_MCP_PROFILE` is set and `SCRIPTORIUM_SERVER_NAME` is unset, the default becomes `scriptorium-<profile>`
- Ready log: the runtime emits an `MCP server ready` log line after startup

On `initialize`, the runtime echoes supported `protocolVersion` requests for `2024-11-05` and `2025-11-25`. When the client omits `protocolVersion`, the runtime keeps the legacy `2024-11-05` response for backward compatibility.

Startup logs identify the runtime base directory, docs root, code-source mode, artifact usage, verify mode, and configured code extensions.

## Environment Variables

`SCRIPTORIUM_MARKDOWN_DIR` is the required variable for the Markdown corpus root. The other variables are optional.

| Variable | Meaning | Default |
| --- | --- | --- |
| `SCRIPTORIUM_SERVER_NAME` | MCP `serverInfo.name` override | `scriptorium` or derived from `SCRIPTORIUM_MCP_PROFILE` |
| `SCRIPTORIUM_MCP_PROFILE` | MCP profile identifier used for server identity and diagnostics | none |
| `SCRIPTORIUM_MCP_TOOL_PREFIX` | Optional advertised tool-name prefix | none |
| `SCRIPTORIUM_MCP_DOMAIN_DESCRIPTION` | Domain description injected into tool metadata | `the configured documentation and code corpus` |
| `SCRIPTORIUM_MCP_CORPUS_SUMMARY` | Short summary of the imported corpus for tool metadata and diagnostics | none |
| `SCRIPTORIUM_MCP_EXAMPLE_QUERIES` | Newline-delimited example queries for diagnostics | none |
| `SCRIPTORIUM_HOME` | Base directory for resolving relative paths | process cwd |
| `SCRIPTORIUM_MARKDOWN_DIR` | Markdown docs root | required |
| `SCRIPTORIUM_CODE_ROOTS` | Filesystem code roots | none |
| `SCRIPTORIUM_SNAPSHOT_FILE` | One or more snapshot artifact paths | none |
| `SCRIPTORIUM_INDEX_FILE` | One or more docs index artifact paths | auto-discover |
| `SCRIPTORIUM_INDEX_VERIFY` | Docs index verify mode: `full`, `mtime`, `off` | `full` |
| `SCRIPTORIUM_ALLOW_SYMLINKS` | Allow display-path-based symlink traversal | `false` |
| `SCRIPTORIUM_MAX_FILE_BYTES` | Maximum readable file size | `1000000` |
| `SCRIPTORIUM_CODE_EXTENSIONS` | Default code extensions | `.cs,.ts` |
| `SCRIPTORIUM_TEXT_ENCODING_FALLBACK` | Fallback encodings for text decode | empty except Windows defaults |

### Path Resolution

- Relative paths resolve against `SCRIPTORIUM_HOME` when it is set.
- Otherwise relative paths resolve against the process working directory.
- Path-bearing list variables accept the host OS path-list separator and newlines.
  - Windows: `;`
  - macOS / Linux: `:`

### Docs-Only Mode

The runtime enters docs-only mode when both `SCRIPTORIUM_CODE_ROOTS` and `SCRIPTORIUM_SNAPSHOT_FILE` are unset.

In docs-only mode:

- docs search remains enabled
- code search is disabled
- code/file `refId` resolution fails
- diagnostics report `docs.docsOnlyMode=true`

## MCP Tools

The runtime publishes six tools.

The canonical tool contracts stay fixed, but `tools/list` may advertise profile-aware names and descriptions. When `SCRIPTORIUM_MCP_TOOL_PREFIX` is set, advertised tool names are prefixed while `tools/call` still accepts the canonical base names for compatibility.

| Tool | Required input | Main optional input | Result focus |
| --- | --- | --- | --- |
| `diagnostics` | none | none | runtime health, corpus shape, caches, retrieval fallback warnings |
| `search` | `query` | `extensions`, `topK`, `snippetLines`, `mode` | ranked docs/code refs |
| `get_content` | `refId` | `mode`, `snippetLines`, `maxLines` | snippet, full, or multi-range content |
| `expand_related` | `seeds` | `extensions`, `budget`, `signals`, `snippetLines`, `mode` | related refs with scored reasons |
| `summarize_flow` | none | `topic`, `seeds`, `sources`, `style`, `maxSteps` | deterministic step list with assumptions/confidence |
| `guide_implementation` | `topic` | `framework`, `language`, `preference`, `maxRefs` | grounded implementation guide with supporting refs |

### Tool Result Envelope

Successful tool calls return a text block plus `structuredContent`.

```json
{
  "content": [
    {
      "type": "text",
      "text": "summary text\n\n{ ...pretty JSON payload... }"
    }
  ],
  "structuredContent": { "...payload..." }
}
```

Tool failures return an error-shaped tool result instead of an MCP protocol error.

```json
{
  "content": [
    {
      "type": "text",
      "text": "error message\n\n{ \"error\": { \"message\": \"...\" } }"
    }
  ],
  "structuredContent": {
    "error": {
      "message": "..."
    }
  },
  "isError": true
}
```

## Ref IDs And Logical Paths

`scriptorium` exposes three `refId` forms:

- Markdown heading block: `md:<normalized-path>#<heading-slug>`
- Code line anchor: `code:<logical-path>@L<line>`
- File path: `file:<logical-path>`

Path normalization uses forward slashes, removes leading `./`, and cleans `.` / `..` segments. When multiple code sources are active, logical code paths use `@<rootId>/<relative/path>` so the source remains explicit.

Markdown heading slugs:

- normalize with NFKC
- lowercase the result
- keep letters and numbers
- collapse non-alphanumeric runs to `-`
- append numeric suffixes for duplicates

## Safety And File Access

- Filesystem access is anchored to configured docs/code roots.
- `SCRIPTORIUM_MAX_FILE_BYTES` limits readable text files.
- Binary-like content is rejected.
- Directory scans ignore known build and dependency directories such as `.git`, `node_modules`, `dist`, `bin`, and `obj`.

### Symlink Policy

- When `SCRIPTORIUM_ALLOW_SYMLINKS=false`, symlink traversal is rejected.
- When `SCRIPTORIUM_ALLOW_SYMLINKS=true`, the runtime uses display-path-based access rules:
  - the requested display path must remain under the configured root
  - the OS-resolved target may be outside the root
  - access is allowed if the normal file API can resolve and read it

## Diagnostics Shape

`diagnostics` returns the following top-level sections:

- `server`: server name, version, runtime, platform, pid
- `profile`: active MCP profile metadata, tool prefix, domain description, corpus summary, example queries
- `runtime`: working directory, base dir, docs index verify mode
- `docs`: docs root, docs-only mode, docs index status/meta
- `code`: code enabled flag, configured extensions, filesystem and snapshot sources
- `caches`: filesystem text cache and code-source cache stats
- `retrieval`: corpus availability, artifact counts, fallback state, warnings

## Related Runtime Docs

- [Retrieval Semantics](./retrieval-semantics.md)
- [Artifacts](./artifacts.md)
