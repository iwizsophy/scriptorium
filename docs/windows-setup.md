# Windows Setup Guide

This guide is for people who want to install and run the released Windows build of `scriptorium`.

## Package Contents

The Windows archive contains:

- `scriptorium.exe`
- `scriptorium-index.exe`
- `scriptorium-snapshot.exe`
- `LICENSE`
- `docs/windows-setup.md`
- `docs/windows-setup.ja.md`

## Before You Start

Prepare the following:

- a Windows machine that matches the downloaded archive (`amd64` or `arm64`)
- a directory that contains the Markdown files you want to search
- `git` if you want to build a code snapshot from a repository

## 1. Unpack the Archive

Open the downloaded ZIP file and extract it to a working directory such as:

```powershell
New-Item -ItemType Directory -Path $HOME\scriptorium -Force
Expand-Archive -LiteralPath .\scriptorium_<version>_windows_<arch>.zip -DestinationPath $HOME\scriptorium -Force
Set-Location $HOME\scriptorium\scriptorium_<version>_windows_<arch>
```

## 2. Prepare Your Markdown Directory

Choose the directory that contains the Markdown files you want to use with `scriptorium`.

Example:

```powershell
$env:SCRIPTORIUM_MARKDOWN_DIR = (Resolve-Path $HOME\work\docs)
```

## 3. Optional: Build a Docs Index

If you want faster Markdown search, build the docs index once before starting the server.

```powershell
.\scriptorium-index.exe --docs-root $env:SCRIPTORIUM_MARKDOWN_DIR --out "$PWD\scriptorium-index.sqlite"
$env:SCRIPTORIUM_INDEX_FILE = "$PWD\scriptorium-index.sqlite"
```

## 4. Optional: Build a Git Snapshot

If you also want snapshot-based code search, build a snapshot from the target Git repository.

```powershell
.\scriptorium-snapshot.exe `
  --repo "$HOME\work\project" `
  --out "$PWD\scriptorium-snapshot.sqlite" `
  --samples HEAD `
  --code-extensions .go,.ts,.cs

$env:SCRIPTORIUM_SNAPSHOT_FILE = "$PWD\scriptorium-snapshot.sqlite"
```

## 5. Optional: Add Direct Code Roots

If you want direct filesystem-backed code search, set `SCRIPTORIUM_CODE_ROOTS`.

```powershell
$env:SCRIPTORIUM_CODE_ROOTS = (Resolve-Path $HOME\work\project)
```

## 6. Start `scriptorium` From Your MCP Client

Configure your MCP client to launch `scriptorium.exe` over stdio.

- Command: the full path to `scriptorium.exe`
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
