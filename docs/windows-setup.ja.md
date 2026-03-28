# Windows セットアップガイド

このガイドは、`scriptorium` の Windows 配布物を利用者として導入し、起動するための手順です。

## 配布物の内容

Windows archive には次の内容が含まれます。

- `scriptorium.exe`
- `scriptorium-index.exe`
- `scriptorium-snapshot.exe`
- `LICENSE`
- `docs/windows-setup.md`
- `docs/windows-setup.ja.md`

## 事前に用意するもの

次のものを用意してください。

- 取得した archive に合った Windows 環境 (`amd64` または `arm64`)
- 検索対象にしたい Markdown ファイルを格納したディレクトリ
- Git リポジトリから code snapshot を作る場合は `git`

## 1. Archive を展開する

ダウンロードした ZIP を作業用ディレクトリに展開します。例:

```powershell
New-Item -ItemType Directory -Path $HOME\scriptorium -Force
Expand-Archive -LiteralPath .\scriptorium_<version>_windows_<arch>.zip -DestinationPath $HOME\scriptorium -Force
Set-Location $HOME\scriptorium\scriptorium_<version>_windows_<arch>
```

## 2. Markdown ディレクトリを決める

`scriptorium` に使わせる Markdown ファイルのディレクトリを決めます。

例:

```powershell
$env:SCRIPTORIUM_MARKDOWN_DIR = (Resolve-Path $HOME\work\docs)
```

## 3. 任意: docs index を作成する

Markdown 検索を高速化したい場合は、起動前に docs index を一度作成します。

```powershell
.\scriptorium-index.exe --docs-root $env:SCRIPTORIUM_MARKDOWN_DIR --out "$PWD\scriptorium-index.sqlite"
$env:SCRIPTORIUM_INDEX_FILE = "$PWD\scriptorium-index.sqlite"
```

## 4. 任意: Git snapshot を作成する

snapshot ベースの code 検索も使いたい場合は、対象 Git リポジトリから snapshot を作成します。

```powershell
.\scriptorium-snapshot.exe `
  --repo "$HOME\work\project" `
  --out "$PWD\scriptorium-snapshot.sqlite" `
  --samples HEAD `
  --code-extensions .go,.ts,.cs

$env:SCRIPTORIUM_SNAPSHOT_FILE = "$PWD\scriptorium-snapshot.sqlite"
```

## 5. 任意: 直接検索する code root を設定する

filesystem 上の code を直接検索したい場合は `SCRIPTORIUM_CODE_ROOTS` を設定します。

```powershell
$env:SCRIPTORIUM_CODE_ROOTS = (Resolve-Path $HOME\work\project)
```

## 6. MCP client から `scriptorium.exe` を起動する

MCP client 側で、`scriptorium.exe` を stdio server として起動するよう設定してください。

- Command: `scriptorium.exe` のフルパス
- Environment:
  - `SCRIPTORIUM_MARKDOWN_DIR`
  - docs index を作成した場合は `SCRIPTORIUM_INDEX_FILE`
  - Git snapshot を作成した場合は `SCRIPTORIUM_SNAPSHOT_FILE`
  - direct code root を使う場合は `SCRIPTORIUM_CODE_ROOTS`

## 7. 起動後に確認する

client から server を起動したあと、次を確認してください。

- server がエラーなく起動する
- client から `diagnostics` を呼べる
- `diagnostics` に表示される docs / snapshot の設定が、用意した内容と一致している

任意の artifact を作成した場合は、`diagnostics` 上でも有効状態として見えるはずです。
