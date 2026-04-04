# Changelog

## v1.1.0 - 2026-04-04

### 変更

- 配布 archive に `THIRD-PARTY-NOTICES.md` を含めるようにしました。
- 配布 archive に Syft で生成した SPDX SBOM
  `scriptorium.sbom.spdx.json` を含めるようにしました。
- CI は `main` と `develop` への push を明示的に対象にしつつ、release
  publish 自体は引き続き tag 起点のままにしています。

### ドキュメント

- ランタイム契約文書の server version 表記を `1.1.0` に更新しました。
- version ごとの operator 向け変更点を追いやすいよう、repository
  documentation からこの release notes へ導線を追加しました。
