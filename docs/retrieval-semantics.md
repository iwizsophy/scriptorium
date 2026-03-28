# Retrieval Semantics

This document describes the externally observable retrieval behavior of the `scriptorium` runtime.

For the general runtime surface, see [Runtime Contract](./runtime-contract.md). For artifact build/load rules, see [Artifacts](./artifacts.md).

## Query Normalization

- Search text is normalized before matching.
- Full-text tokenization keeps CJK-friendly behavior and mixed ASCII/CJK queries.
- Path and label boosts use the normalized phrase plus token matches.

## `search`

`search` returns ranked docs and code references.

### Common Behavior

- Empty or whitespace-only queries return no results.
- `topK` and `snippetLines` are clamped to bounded ranges.
- `extensions` controls whether docs, code, or both are searched.
- results are deduplicated and sorted by score, then path and range tiebreaks

### Docs Search

- If docs index artifacts are available and valid, the runtime searches them first.
- Otherwise it falls back to Markdown filesystem scanning.
- Docs matches are produced from heading blocks rather than arbitrary line slices.
- Heading text and file path receive stronger boosts than body-only matches.

### Code Search

- Snapshot-backed code sources search the prebuilt snapshot first.
- Filesystem-backed code sources scan configured roots when no snapshot result path is available.
- Matching code lines produce `code_range` refs centered around the matched line.
- Nearby hits in the same file are clustered so the best local result survives.
- When no matching line exists but the path or source label strongly matches, the runtime can return a file-level fallback anchored at line 1.

### Implementation Mode

`mode=implementation` is additive. It does not change the tool contract, but it does change ranking:

- startup / entrypoint files are boosted
- DI / service registration hints are boosted
- route / endpoint material is boosted
- attribute / decorator patterns are boosted
- config-oriented material is boosted
- the runtime prefers returning at least one docs result and one code result when both corpora have useful matches

### Snippets

- docs snippets come from the start of the matched heading block
- code snippets center around the matched line when possible
- snippet length is controlled by `snippetLines`

## `get_content`

`get_content` resolves a single `refId`.

### Common Modes

- `snippet`: focused range around the anchor
- `full`: full block or full file, subject to `maxLines`
- `multi_range`: highlighted ranges around important lines

### Markdown Refs

- Markdown refs resolve to heading blocks.
- The slug identifies the heading block inside the file.
- `snippet` and `full` operate on that block.
- `multi_range` marks ordered lists, bullet lists, nested headings, or other flow-relevant lines inside the block.

### Code Refs

- Code refs resolve to a logical code path and line anchor.
- `snippet` centers around the requested line.
- `full` returns the whole file, subject to truncation.
- `multi_range` marks the anchor line, nearby signatures, route handlers, and other interesting implementation lines.

### File Refs

- File refs resolve the whole file path without a line anchor.
- In docs-only mode, code/file refs are unavailable for code sources.

## `expand_related`

`expand_related` starts from one or more seed refs and finds supporting refs.

- seeds are loaded as Markdown or code contexts
- overlap tokens, import-like tokens, path proximity, and framework tags all contribute to score
- results include scored reasons rather than a single opaque score
- docs and code candidates can both appear in the same response

`mode=implementation` enables additional framework-aware relations such as startup wiring, DI, route, attribute/decorator, and config links.

## `summarize_flow`

`summarize_flow` turns refs into a deterministic step list.

### Extraction Priority

For Markdown:

1. numbered steps
2. labeled steps such as `手順1`
3. bullet lists
4. nested headings
5. paragraph fallback

For code:

- route handlers, function/class signatures, and recognizable operations contribute candidate steps
- style selection affects how compactly the steps are rendered

### Output

- `flow`: ordered steps with titles, descriptions, and refs
- `assumptions`: explicit fallback notes
- `confidence`: qualitative confidence derived from source quality and merge stability

If there are no usable refs, the runtime falls back to the requested topic rather than returning a protocol error.

## `guide_implementation`

`guide_implementation` composes retrieval tools into a single grounded guide.

- builds a query from `topic`, `framework`, and `language`
- runs implementation-mode docs and code search
- expands supporting refs from the best initial seeds
- summarizes the combined refs into a short implementation flow
- returns separate `docs` and `sampleCode` support lists, plus `assumptions` and `confidence`

The guide intentionally stays grounded in the current corpus. If supporting refs are missing, the response records that limitation instead of inventing missing sources.
