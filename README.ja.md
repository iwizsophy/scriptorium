<p align="center">
  <img src="./docs/assets/scriptorium-logo-transparent.png" alt="scriptorium" width="360">
</p>

# scriptorium

[English](./README.md) | 日本語

`scriptorium` は、Markdown とコードを根拠付きで検索・参照するための MCP ランタイムです。このリポジトリには、Go 実装、リリース用パッケージ、各種アーティファクトビルダーが含まれます。

## ランタイム文書

現在のランタイム仕様とアーティファクトの挙動は、次の文書を参照してください。

- [ランタイム契約](./docs/runtime-contract.ja.md)
- [取得セマンティクス](./docs/retrieval-semantics.ja.md)
- [アーティファクト](./docs/artifacts.ja.md)

現在の利用者向け挙動の基準は、上記のランタイム文書です。

## コミュニティ

- コントリビューションガイド: [CONTRIBUTING.md](./CONTRIBUTING.md)
- 日本語版コントリビューションガイド: [CONTRIBUTING.ja.md](./CONTRIBUTING.ja.md)
- 行動規範: [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md)
- 日本語行動規範: [CODE_OF_CONDUCT.ja.md](./CODE_OF_CONDUCT.ja.md)
- セキュリティポリシー: [SECURITY.md](./SECURITY.md)
- 日本語セキュリティポリシー: [SECURITY.ja.md](./SECURITY.ja.md)
- サポートポリシー: [.github/SUPPORT.md](./.github/SUPPORT.md)
- 日本語サポートポリシー: [.github/SUPPORT.ja.md](./.github/SUPPORT.ja.md)
- ライセンス: [LICENSE](./LICENSE)
- 日本語ライセンス参考訳: [LICENSE.ja.md](./LICENSE.ja.md)
- Third-party notices: [THIRD-PARTY-NOTICES.md](./THIRD-PARTY-NOTICES.md)

## リリース

`v<major>.<minor>.<patch>` 形式の注釈付きタグを使って GitHub Releases を公開します。

リリースワークフローは、次のプラットフォーム向けにバージョン付きアーカイブを生成します。

- Linux `amd64`, `arm64`
- Windows `amd64`, `arm64`
- macOS `amd64`, `arm64`

Linux 向け archive には、対象プラットフォーム向けの `scriptorium`、
`scriptorium-index`、`scriptorium-snapshot`、
`scriptorium.sbom.spdx.json`、`LICENSE`、`THIRD-PARTY-NOTICES.md`、
および利用者向け Linux セットアップガイドを `docs/`
配下に含めます。

Windows 向け archive には、`scriptorium.exe`、
`scriptorium-index.exe`、`scriptorium-snapshot.exe`、
`scriptorium.sbom.spdx.json`、`LICENSE`、
`THIRD-PARTY-NOTICES.md`、および利用者向け Windows セットアップ
ガイドを `docs/` 配下に含めます。

macOS 向け archive には、対象プラットフォーム向けのバイナリ、
repository README 一式、`scriptorium.sbom.spdx.json`、`LICENSE`、
`THIRD-PARTY-NOTICES.md` を含めます。

通常の CI では、pull request と non-tag push を Linux、Windows、macOS で検証します。

## 現在の機能

- 現在の実装には、次の機能があります。
  - MCP ランタイムサーバーのエントリーポイント
  - docs index builder のエントリーポイント
  - git snapshot builder のエントリーポイント
  - ランタイム設定の読み込み
  - `refId` の解析と仕様準拠の見出し slug 生成
  - ツール結果 envelope の生成
  - diagnostics payload の構築基盤
  - display-path ベースの filesystem guard と決定的なファイル走査
  - UTF-8、UTF-16、Shift_JIS 系 fallback を含むテキストデコード
  - Markdown 見出し block の解析と docs の filesystem fallback 走査
  - 見出し・path の boost、code path fallback、clustered `code_range` を含む docs/code 検索
  - markdown、code、file ref 向けの `get_content`
  - filesystem-backed docs と sample root 向けの `expand_related`
  - filesystem-backed markdown / code ref 向けの `summarize_flow`
  - 取得結果と flow 要約をまとめて、根拠付きの実装ガイドを返す `guide_implementation` MCP tool
  - 自動検出、検証、stale fallback を備えた SQLite/FTS ベースの docs index builder / runtime support
  - snapshot-backed code search と content resolution を支える SQLite/FTS ベースの git snapshot builder / runtime support
  - 文書化されたランタイムシナリオに対する acceptance 重視の検証
  - `initialize`、`ping`、`tools/list`、`tools/call` を受け付ける MCP stdio transport

## 導入手順

### 1. 前提条件

- 配布されている各 OS 向けバイナリを取得しておく
- または GitHub Releases から公開済み archive を取得しておく
- 検索対象の Markdown ディレクトリを用意する
- Git snapshot を使う場合は `git` コマンドが利用できる状態にする

配布物には少なくとも次の 3 つの実行ファイルが含まれている前提です。

- `scriptorium`
- `scriptorium-index`
- `scriptorium-snapshot`

Windows では通常 `.exe` 付きになります。

### 2. 配布アーカイブを展開する

```powershell
mkdir scriptorium
cd scriptorium
# ここに配布されたバイナリを配置
```

### 3. docs index を作成する

docs 検索を高速化したい場合は、先に SQLite index を生成します。

```powershell
.\scriptorium-index.exe --docs-root .\docs --out .\scriptorium-index.sqlite
```

macOS / Linux:

```bash
./scriptorium-index --docs-root ./docs --out ./scriptorium-index.sqlite
```

### 4. Git snapshot を作成する

コード検索を snapshot ベースで使いたい場合は、対象リポジトリから snapshot を生成します。

```powershell
.\scriptorium-snapshot.exe --repo . --out .\scriptorium-snapshot.sqlite --samples HEAD --code-extensions .go,.ts,.cs
```

macOS / Linux:

```bash
./scriptorium-snapshot --repo . --out ./scriptorium-snapshot.sqlite --samples HEAD --code-extensions .go,.ts,.cs
```

代表的な `--samples`:

- `HEAD`: 現在の HEAD を snapshot 化
- `WORKTREE`: ワークツリーを snapshot 化
- `feature/foo`: 特定 branch / ref を snapshot 化

`--samples ALL` はローカル branch のみを展開します。`--fetch` を有効にしても、`origin/feature/foo` のような remote-tracking ref は自動では含まれません。

### 5. 環境変数を設定する

Markdown corpus root には `SCRIPTORIUM_MARKDOWN_DIR` を使います。`SCRIPTORIUM_CODE_ROOTS` を使うと filesystem 上の code root を直接検索でき、`SCRIPTORIUM_INDEX_FILE` / `SCRIPTORIUM_SNAPSHOT_FILE` を使うと事前生成したアーティファクトを利用できます。複数 path を受け付ける環境変数は、OS ごとの path-list separator と改行を区切りとして扱うため、空白を含む path も途中で分割されません。Windows では `;`、macOS / Linux では `:` を使います。

```powershell
$env:SCRIPTORIUM_MARKDOWN_DIR = (Resolve-Path .\docs)
$env:SCRIPTORIUM_INDEX_FILE = (Resolve-Path .\scriptorium-index.sqlite)
$env:SCRIPTORIUM_SNAPSHOT_FILE = (Resolve-Path .\scriptorium-snapshot.sqlite)
$env:SCRIPTORIUM_CODE_EXTENSIONS = ".go,.ts,.cs"
$env:SCRIPTORIUM_INDEX_VERIFY = "full"
```

optional な MCP identity metadata を設定すると、AI client からこの server を選びやすくなります。`SCRIPTORIUM_SERVER_NAME` は `serverInfo.name` を override し、`SCRIPTORIUM_MCP_PROFILE` は profile 識別子を与え、`SCRIPTORIUM_MCP_TOOL_PREFIX` は advertise される tool 名に prefix を付けます。`SCRIPTORIUM_MCP_DOMAIN_DESCRIPTION` / `SCRIPTORIUM_MCP_CORPUS_SUMMARY` は tool description を具体化し、`SCRIPTORIUM_MCP_EXAMPLE_QUERIES` は diagnostics に出す改行区切りの例を設定します。

複数 artifact を横断検索したい場合の例:

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

filesystem sample root を直接使う場合の例:

```powershell
$env:SCRIPTORIUM_CODE_ROOTS = (Resolve-Path .\src)
```

### 6. MCP server を起動する

```powershell
.\scriptorium.exe
```

起動後は stdio transport で `initialize`、`tools/list`、`tools/call` を受け付けます。

macOS / Linux:

```bash
./scriptorium
```

### 7. 動作確認をする

まず `diagnostics` を呼ぶと、docs index、git snapshot、cache の状態まで含めて確認できます。

### 参考: 最小の MCP 利用例

多くの MCP client は transport の framing を内部で処理するため、通常は server command を登録するだけで使えます。ここでは、runtime が受け付ける request body の最小例だけを示します。

`tools/list`:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/list",
  "params": {}
}
```

`tools/call` で `diagnostics` を呼ぶ:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "diagnostics",
    "arguments": {}
  }
}
```

`tools/call` で `search` を呼ぶ:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "search",
    "arguments": {
      "query": "authentication middleware",
      "extensions": [
        "docs",
        "code"
      ],
      "topK": 5,
      "snippetLines": 6
    }
  }
}
```

今の runtime が公開するツールは次の 6 つです。

- `diagnostics`
- `search`
- `get_content`
- `expand_related`
- `summarize_flow`
- `guide_implementation`

各ツールの必須パラメータと主な任意パラメータは [ランタイム契約](./docs/runtime-contract.ja.md) にまとまっています。builder CLI の flags は [アーティファクト](./docs/artifacts.ja.md) を参照してください。

確認項目:

- `docs.docsOnlyMode`
- `docs.index.enabled`
- `code.gitSnapshot.enabled`
- `caches.textFiles`
- `caches.sampleRoots`
- `retrieval.corpus`
- `retrieval.fallback`
- `retrieval.warnings`

推奨 MCP tool:

- `diagnostics`: runtime の状態と cache 可視性の確認
- `search`, `get_content`, `expand_related`, `summarize_flow`: 基本的な取得ワークフロー
- `guide_implementation`: supporting docs / sample-code refs 付きの根拠ある実装ガイドを 1 回で取得したい場合

実装ガイド向け retrieval:

- `search` は additive な `mode=implementation` を受け付け、framework-aware な ranking と docs/code の多様化を有効にします。
- `expand_related` は additive な `mode=implementation` と `framework_links` signal を受け付け、startup wiring、DI、route、attribute/decorator、config 系の関係も追加で考慮します。
- `guide_implementation` は、内部でこれらの implementation-aware retrieval hint を利用します。

## Docs 作成ガイド

docs を `scriptorium` が解析しやすい形で整備したい場合は、[MARKDOWN_AUTHORING_GUIDE.ja.md](./MARKDOWN_AUTHORING_GUIDE.ja.md) を参照してください。

特に次の点が効きます。

- 1 つの heading block には 1 つの実装トピックを置く
- 手順は numbered list で書く
- API 名、設定キー、route、型名、ファイル名を省略せずに書く
- 重要なサンプルは、その説明と同じ heading block に置く

## 作業管理

- 進行中の engineering work は GitHub Issues で追跡しています。

## テストカバレッジ

- Coverage command:
  - PowerShell: `./scripts/test-coverage.ps1`
  - Bash: `./scripts/test-coverage.sh`
- coverage script は package 単位で `go test -coverprofile` を実行し、`coverage.out` に統合したうえで `go tool cover -func` の要約を出力します。
- 現在の merged statement coverage は `99.2%` です。
  - まだ改善余地が大きい package は `internal/ref` (`98.2%`), `internal/docsindex` (`98.5%`), `internal/content` (`98.7%`), `internal/related` (`98.7%`), `internal/gitsnapshot` (`98.9%`) です。

## 履歴テスト監査

- 移行期の履歴テスト監査メモは、現行の active workflow には含めません。
- coverage mapping は、リポジトリの tests を package 単位で構成しているため、file 単位ではなく scenario 単位で追跡しています。

## Snapshot Builder に関する注意

- git snapshot builder は、runtime の filesystem sample roots と同じ size、ignored-directory、fallback-decoding のルールを snapshot の生成処理に適用し、symlink を許可した場合は同じ display-path-based policy に従います。
- build-time fetch は意図的に単純化しており、`--fetch` で有効化し、`--fetch-on-start` で snapshot 生成前に実行するかを決め、`--fetch-remote` で remote を選びます。

## Diagnostics に関する注意

- `diagnostics.caches.textFiles` は、docs と filesystem-backed code read が使う live filesystem text cache を反映します。
- `diagnostics.code.sampleSources` は filesystem roots と snapshot-backed roots の両方を出すため、runtime source coverage を `gitSnapshot.roots` と突き合わせなくても確認できます。
- `diagnostics.server` は `runtime=go` と `runtimeVersion` のみを報告します。
- `diagnostics.caches.textFiles` には `evictions` が含まれます。
- `diagnostics.caches.sampleRoots` は filesystem と snapshot-backed code source の解決に使う live runtime code-source cache を反映し、現在の cache 内容に対する `evictions`、`filesystemRoots`、`snapshotRoots` を報告します。

## 依存関係

- このリポジトリでは `modernc.org/sqlite` を pure-Go の SQLite driver として使っています。これにより、CGO を追加せずに docs index と git snapshot artifact を元実装に近い SQLite/FTS の形で扱えます。

## パッケージ構成

- `cmd/scriptorium`: 配布される `scriptorium` バイナリのランタイムサーバー用エントリーポイント
- `cmd/build-docs-index`: 配布される `scriptorium-index` バイナリの docs index builder 用エントリーポイント
- `cmd/build-git-snapshot`: 配布される `scriptorium-snapshot` バイナリの git snapshot builder 用エントリーポイント
- `internal/app`: コマンド実行のオーケストレーション
- `internal/config`: 環境変数とランタイム設定
- `internal/content`: `get_content` の request 処理と range 選択
- `internal/diagnostics`: diagnostics payload
- `internal/docs`: Markdown block の解析と docs の filesystem 走査
- `internal/docsindex`: docs index artifact の build/load/verify helper
- `internal/filesafe`: 保護付き filesystem access と決定的な走査
- `internal/flow`: `summarize_flow` の抽出、統合、confidence 判定
- `internal/gitsnapshot`: snapshot artifact の build/load helper
- `internal/protocol`: tool result envelope helper
- `internal/related`: `expand_related` の request 処理と scoring
- `internal/ref`: path 正規化と `refId` 処理
- `internal/search`: query 正規化と filesystem-backed search helper
- `internal/source`: 共有の filesystem-backed code source 解決
- `internal/textdecode`: 仕様志向の text decoding helper
- `internal/textutil`: line normalization、split、centered range を共有する helper package
