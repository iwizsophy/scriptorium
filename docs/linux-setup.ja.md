# Linux セットアップガイド

このガイドは、`scriptorium` の Linux 配布物を利用者として導入し、起動するための手順です。

## 配布物の内容

Linux archive には次の内容が含まれます。

- `scriptorium`
- `scriptorium-index`
- `scriptorium-snapshot`
- `LICENSE`
- `docs/linux-setup.md`
- `docs/linux-setup.ja.md`

## 事前に用意するもの

次のものを用意してください。

- 取得した archive に合った Linux 環境 (`amd64` または `arm64`)
- 検索対象にしたい Markdown ファイルを格納したディレクトリ
- Git リポジトリから code snapshot を作る場合は `git`

## 1. Archive を展開する

```bash
mkdir -p ~/scriptorium
tar -xzf scriptorium_<version>_linux_<arch>.tar.gz -C ~/scriptorium
cd ~/scriptorium/scriptorium_<version>_linux_<arch>
```

## 2. Markdown ディレクトリを決める

`scriptorium` に使わせる Markdown ファイルのディレクトリを決めます。

例:

```bash
export SCRIPTORIUM_MARKDOWN_DIR="$HOME/work/docs"
```

## 3. 任意: docs index を作成する

Markdown 検索を高速化したい場合は、起動前に docs index を一度作成します。

```bash
./scriptorium-index --docs-root "$SCRIPTORIUM_MARKDOWN_DIR" --out "$PWD/scriptorium-index.sqlite"
export SCRIPTORIUM_INDEX_FILE="$PWD/scriptorium-index.sqlite"
```

## 4. 任意: Git snapshot を作成する

snapshot ベースの code 検索も使いたい場合は、対象 Git リポジトリから snapshot を作成します。

```bash
./scriptorium-snapshot \
  --repo "$HOME/work/project" \
  --out "$PWD/scriptorium-snapshot.sqlite" \
  --samples HEAD \
  --code-extensions .go,.ts,.cs

export SCRIPTORIUM_SNAPSHOT_FILE="$PWD/scriptorium-snapshot.sqlite"
```

## 5. 任意: 直接検索する code root を設定する

filesystem 上の code を直接検索したい場合は `SCRIPTORIUM_CODE_ROOTS` を設定します。

```bash
export SCRIPTORIUM_CODE_ROOTS="$HOME/work/project"
```

## 6. MCP client から `scriptorium` を起動する

MCP client 側で、`scriptorium` バイナリを stdio server として起動するよう設定してください。

- Command: `scriptorium` のフルパス
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
