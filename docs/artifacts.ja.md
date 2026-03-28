# Artifacts

この文書は、`scriptorium` の artifact に関する build-time / runtime 契約をまとめたものです。

runtime server の公開面は [Runtime Contract](./runtime-contract.ja.md)、search/content の意味論は [Retrieval Semantics](./retrieval-semantics.ja.md) を参照してください。

## Docs Index

### Builder CLI

`scriptorium-index` は Markdown docs search 用の SQLite/FTS artifact を生成します。

利用できる flags:

- `--docs-root` (required): Markdown docs root
- `--out` (required): output artifact path
- `--allow-symlinks`: build 時に display-path-based な symlink traversal を許可する

### 保持情報

docs index には次が入ります。

- schema version、generation time、docs root、file/block counts、source fingerprint などの metadata
- Markdown file record
- heading-block record
- ranked block lookup に使う FTS table

### Runtime での選択

runtime では `SCRIPTORIUM_INDEX_FILE` で docs index artifact を明示指定できます。

- 複数 artifact を指定できます
- explicit path は列挙順で読み込みます
- explicit artifact はすべて load / verify に成功する必要があり、失敗した場合は filesystem scan に fallback します

explicit path がない場合は、次の順で probe します。

1. `<docs-root>/scriptorium-index.sqlite`
2. `<docs-root>/../scriptorium-index.sqlite`
3. `<docs-root>/../../scriptorium-index.sqlite`

### Verification

`SCRIPTORIUM_INDEX_VERIFY` の意味:

- `full`: docs root と source fingerprint が一致すること
- `mtime`: docs root、file count、max mtime が一致すること
- `off`: verify しない

verify mismatch の場合、docs index は無効化され、filesystem scan に fallback します。

## Git Snapshot

### Builder CLI

`scriptorium-snapshot` は code search 用の SQLite/FTS artifact を生成します。

利用できる flags:

- `--repo` (required): Git repository path
- `--out` (required): output artifact path
- `--samples` (required): sample selector
- `--code-extensions`: extension filter list
- `--fetch`: build 前に `git fetch` を有効化
- `--fetch-on-start`: `--fetch` 有効時、materialization 前に fetch を実行
- `--fetch-remote`: build-time fetch に使う remote 名
- `--allow-symlinks`: build 時に display-path-based な symlink traversal を許可
- `--max-file-bytes`: snapshot に含める最大 file size

### Sample Selectors

`--samples` は semicolon または newline 区切りで複数 selector を受け付けます。

代表例:

- `HEAD`
- `WORKTREE`
- `feature/foo`
- `alias=feature/foo`
- `*` または `ALL` で全 branch を展開

`ALL` は `refs/heads` にあるローカル branch を展開します。
`origin/feature/foo` のような remote-tracking ref は自動では含まれません。
`--fetch` は build 前に remote 状態を更新できますが、`ALL` の対象を remote branch まで広げるものではありません。

各 selector は stable な root id と logical path prefix を持つ snapshot root になります。

### 保持情報

snapshot artifact には次が入ります。

- schema version、repo path、generation time、root count、file count、fingerprint などの metadata
- id / description / label を持つ snapshot root
- logical path、relative path、extension、line count、content を持つ code entry
- ranked code search に使う FTS table

### Runtime での選択

runtime では `SCRIPTORIUM_SNAPSHOT_FILE` で snapshot artifact を指定します。

- 複数 snapshot artifact を指定できます
- 列挙した artifact はすべて正常に load できる必要があります
- artifact をまたいで snapshot root id が一意である必要があります

artifact が missing / invalid だったり duplicate root id が見つかった場合、snapshot-backed code search は無効化され、diagnostics / log に warning が出ます。

## Write Strategy

両 builder とも SQLite file を atomic に publish します。

- sibling temp file に書き込む
- database を close する
- 最後に target path へ publish する

これにより final target path に partial artifact が残るのを避けます。
