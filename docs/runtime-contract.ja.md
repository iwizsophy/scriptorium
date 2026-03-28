# Runtime Contract

この文書は、`scriptorium` の現行ランタイム契約をまとめた利用者向けの参照文書です。

現在のランタイム挙動は、この文書と次の関連文書を基準にします。

- [取得セマンティクス](./retrieval-semantics.ja.md)
- [アーティファクト](./artifacts.ja.md)

## コンポーネント

`scriptorium` は 3 つのコマンドラインプログラムを提供します。

| コンポーネント | 役割 |
| --- | --- |
| `scriptorium` | stdio 上で動作する MCP ランタイムサーバー |
| `scriptorium-index` | Markdown 検索用アーティファクトの事前生成 |
| `scriptorium-snapshot` | Git ベースのコード検索用アーティファクトの事前生成 |

## Runtime Server

- Transport: stdio
- Stdio request: newline-delimited JSON-RPC と `Content-Length` framed JSON-RPC の両方を受け付けます
- Stdio response: 互換性のため request 側の framing mode に合わせて返します
- Server version: `1.0.0`
- Default server name: `scriptorium`。`SCRIPTORIUM_MCP_PROFILE` が設定されていて `SCRIPTORIUM_SERVER_NAME` が未設定なら `scriptorium-<profile>`
- Ready log: 起動完了後に `MCP server ready`

`initialize` では、`2024-11-05` と `2025-11-25` の supported `protocolVersion` request をそのまま返します。client が `protocolVersion` を省略した場合は、後方互換のため legacy の `2024-11-05` を返します。

起動ログには runtime base directory、docs root、code-source mode、アーティファクト利用状況、verify mode、code extensions が含まれます。

## 環境変数

Markdown corpus root には `SCRIPTORIUM_MARKDOWN_DIR` を使います。その他の変数は任意です。

| 変数 | 意味 | 既定値 |
| --- | --- | --- |
| `SCRIPTORIUM_SERVER_NAME` | MCP `serverInfo.name` の override | `scriptorium` または `SCRIPTORIUM_MCP_PROFILE` 由来 |
| `SCRIPTORIUM_MCP_PROFILE` | server identity と diagnostics に出す MCP profile 識別子 | なし |
| `SCRIPTORIUM_MCP_TOOL_PREFIX` | advertise する tool 名の optional prefix | なし |
| `SCRIPTORIUM_MCP_DOMAIN_DESCRIPTION` | tool metadata に差し込む domain 説明 | `the configured documentation and code corpus` |
| `SCRIPTORIUM_MCP_CORPUS_SUMMARY` | tool metadata と diagnostics に出す corpus の要約 | なし |
| `SCRIPTORIUM_MCP_EXAMPLE_QUERIES` | diagnostics に出す改行区切りの example query 一覧 | なし |
| `SCRIPTORIUM_HOME` | 相対 path 解決の基準ディレクトリ | process cwd |
| `SCRIPTORIUM_MARKDOWN_DIR` | Markdown docs root | required |
| `SCRIPTORIUM_CODE_ROOTS` | filesystem code root の列 | none |
| `SCRIPTORIUM_SNAPSHOT_FILE` | snapshot artifact path の列 | none |
| `SCRIPTORIUM_INDEX_FILE` | docs index artifact path の列 | auto-discover |
| `SCRIPTORIUM_INDEX_VERIFY` | docs index の検証モード: `full`, `mtime`, `off` | `full` |
| `SCRIPTORIUM_ALLOW_SYMLINKS` | display-path-based な symlink traversal を許可するか | `false` |
| `SCRIPTORIUM_MAX_FILE_BYTES` | 読み取り可能な最大ファイルサイズ | `1000000` |
| `SCRIPTORIUM_CODE_EXTENSIONS` | 既定の code extension 一覧 | `.cs,.ts` |
| `SCRIPTORIUM_TEXT_ENCODING_FALLBACK` | テキストデコード時の fallback encoding 一覧 | Windows 以外は空 |

### Path 解決

- `SCRIPTORIUM_HOME` が設定されていれば、それを基準に相対 path を解決します。
- 未設定時は process working directory を基準にします。
- 複数 path を受け付ける環境変数は、host OS の path-list separator と改行を受け付けます。
  - Windows: `;`
  - macOS / Linux: `:`

### Docs-Only Mode

`SCRIPTORIUM_CODE_ROOTS` と `SCRIPTORIUM_SNAPSHOT_FILE` の両方が未設定なら docs-only mode です。

docs-only mode では:

- docs search は有効
- code search は無効
- code/file `refId` 解決は失敗
- diagnostics は `docs.docsOnlyMode=true` を返す

## MCP Tools

runtime は 6 つの tool を公開します。

canonical な tool 契約自体は固定ですが、`tools/list` が返す名前と description は profile 設定に応じて変わることがあります。`SCRIPTORIUM_MCP_TOOL_PREFIX` を設定すると advertise 名には prefix が付きますが、互換性のため `tools/call` は canonical な base 名も受け付けます。

| ツール | 必須入力 | 主な任意入力 | 主な返却内容 |
| --- | --- | --- | --- |
| `diagnostics` | なし | なし | runtime の状態、corpus 構成、cache、fallback 警告 |
| `search` | `query` | `extensions`, `topK`, `snippetLines`, `mode` | ランク付け済みの docs/code ref |
| `get_content` | `refId` | `mode`, `snippetLines`, `maxLines` | snippet / full / multi-range の内容 |
| `expand_related` | `seeds` | `extensions`, `budget`, `signals`, `snippetLines`, `mode` | score 理由付きの related ref |
| `summarize_flow` | なし | `topic`, `seeds`, `sources`, `style`, `maxSteps` | 決定的な step list |
| `guide_implementation` | `topic` | `framework`, `language`, `preference`, `maxRefs` | supporting ref 付き implementation guide |

### Tool Result Envelope

成功時はテキストブロックと `structuredContent` を返します。

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

失敗時も MCP protocol error ではなく、tool result として error shape を返します。

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

## Ref IDs と Logical Paths

`scriptorium` は 3 種類の `refId` 形式を使います。

- Markdown heading block: `md:<normalized-path>#<heading-slug>`
- Code line anchor: `code:<logical-path>@L<line>`
- File path: `file:<logical-path>`

path normalization では、forward slash 化、先頭 `./` の除去、`.` / `..` の clean を行います。複数 code source が有効な場合、logical code path は `@<rootId>/<relative/path>` となり、どの source に属するかを明示します。

Markdown heading slug には次の規則があります。

- NFKC で normalize
- lowercase 化
- 文字と数字を保持
- それ以外の連続区間を `-` に collapse
- duplicate heading には numeric suffix を付ける

## Safety And File Access

- filesystem access は configured docs/code root を基準にします。
- `SCRIPTORIUM_MAX_FILE_BYTES` が text file の上限です。
- binary-like content は拒否します。
- directory scan では `.git`, `node_modules`, `dist`, `bin`, `obj` など既知の build/dependency directory を無視します。

### Symlink Policy

- `SCRIPTORIUM_ALLOW_SYMLINKS=false` のときは symlink traversal を拒否します。
- `SCRIPTORIUM_ALLOW_SYMLINKS=true` のときは display-path-based policy を使います。
  - 要求された display path 自体は configured root 配下である必要がある
  - OS が解決した実体 path は root 外でもよい
  - 通常の file API で解決・読取可能ならアクセスを許可する

## Diagnostics Shape

`diagnostics` のトップレベルセクション:

- `server`: server name, version, runtime, platform, pid
- `profile`: active MCP profile metadata、tool prefix、domain description、corpus summary、example queries
- `runtime`: working directory, base dir, docs index verify mode
- `docs`: docs root, docs-only mode, docs index status/meta
- `code`: code enabled flag, configured extensions, filesystem / snapshot sources
- `caches`: filesystem text cache と code-source cache の統計
- `retrieval`: corpus の利用可否、artifact 件数、fallback 状態、warnings

## Related Runtime Docs

- [取得セマンティクス](./retrieval-semantics.ja.md)
- [アーティファクト](./artifacts.ja.md)
