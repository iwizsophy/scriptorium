# Linux Setup Guide

This guide is for people who want to install and run the released Linux build of `scriptorium`.

## Package Contents

The Linux archive contains:

- `scriptorium`
- `scriptorium-index`
- `scriptorium-snapshot`
- `LICENSE`
- `docs/linux-setup.md`
- `docs/linux-setup.ja.md`

## Before You Start

Prepare the following:

- a Linux machine that matches the downloaded archive (`amd64` or `arm64`)
- a directory that contains the Markdown files you want to search
- `git` if you want to build a code snapshot from a repository

## 1. Unpack the Archive

```bash
mkdir -p ~/scriptorium
tar -xzf scriptorium_<version>_linux_<arch>.tar.gz -C ~/scriptorium
cd ~/scriptorium/scriptorium_<version>_linux_<arch>
```

## 2. Prepare Your Markdown Directory

Choose the directory that contains the Markdown files you want to use with `scriptorium`.

Example:

```bash
export SCRIPTORIUM_MARKDOWN_DIR="$HOME/work/docs"
```

## 3. Optional: Build a Docs Index

If you want faster Markdown search, build the docs index once before starting the server.

```bash
./scriptorium-index --docs-root "$SCRIPTORIUM_MARKDOWN_DIR" --out "$PWD/scriptorium-index.sqlite"
export SCRIPTORIUM_INDEX_FILE="$PWD/scriptorium-index.sqlite"
```

## 4. Optional: Build a Git Snapshot

If you also want snapshot-based code search, build a snapshot from the target Git repository.

```bash
./scriptorium-snapshot \
  --repo "$HOME/work/project" \
  --out "$PWD/scriptorium-snapshot.sqlite" \
  --samples HEAD \
  --code-extensions .go,.ts,.cs

export SCRIPTORIUM_SNAPSHOT_FILE="$PWD/scriptorium-snapshot.sqlite"
```

## 5. Optional: Add Direct Code Roots

If you want direct filesystem-backed code search, set `SCRIPTORIUM_CODE_ROOTS`.

```bash
export SCRIPTORIUM_CODE_ROOTS="$HOME/work/project"
```

## 6. Start `scriptorium` From Your MCP Client

Configure your MCP client to launch the `scriptorium` binary over stdio.

- Command: the full path to `scriptorium`
- Environment:
  - `SCRIPTORIUM_MARKDOWN_DIR`
  - `SCRIPTORIUM_INDEX_FILE` if you built a docs index
  - `SCRIPTORIUM_SNAPSHOT_FILE` if you built a Git snapshot
  - `SCRIPTORIUM_CODE_ROOTS` if you use direct code roots

## 7. Check That It Is Ready

After the client starts the server, confirm that:

- the server starts without an error
- the client can call `diagnostics`
- the reported docs and snapshot settings match the files you prepared

If you built the optional artifacts, `diagnostics` should show them as enabled.
