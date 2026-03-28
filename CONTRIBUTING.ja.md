# Contributing

`scriptorium` へのコントリビュートに関心を持っていただきありがとうございます。

## 開始前に

- 挙動や互換性に関わる変更では、次の順で文書を参照してください:
  `docs/runtime-contract.ja.md`、`docs/retrieval-semantics.ja.md`、
  `docs/artifacts.ja.md`、`docs/repository-governance.ja.md`、`AGENTS.md`、
  `README.ja.md`
- バグ報告、機能要望、公開の設計議論が必要な場合は GitHub Issues を使ってください。
- この repository では実装トラッキングとして `issues/` 配下の markdown issue も使っているため、大きな作業では maintainer が対応する tracker を作成または更新することがあります。
- 大きな変更、公開挙動変更、アーキテクチャ変更、release process 変更、新しい依存関係の提案は、実装前に issue を作成してください。
- 変更はできるだけ小さく、焦点を絞ってください。
- default branch は `main` です。maintainer から別指示がない限り、pull request は `main` に向けてください。
- release tag は annotated tag とし、`v<major>.<minor>.<patch>` 形式を使ってください。tag は `main` から到達可能な commit を指すべきです。
- 想定している CI checks は test matrix、`coverage`、`build` です。
- 新しい third-party dependency と major dependency update には issue と明示的な理由が必要です。
- 挙動が変わる場合は、可能な限り同じ変更で active runtime docs と関連テストも更新してください。
- user-facing docs は、可能な限り英語版と日本語版をそろえてください。

## 開発フロー

1. repository を fork し、作業用ブランチを作成します。
2. 挙動、アーキテクチャ、repository policy に影響する作業では issue を作成または更新します。
3. 作業が大きい場合は `issues/*.md` に対応する tracker を作成または既存 tracker を更新します。
4. 変更内容に対応するテストまたはドキュメント更新を行います。
5. repository root で次を実行して確認します。
   - `go test ./...`
   - `go build ./cmd/...`
   - PowerShell coverage: `./scripts/test-coverage.ps1`
   - Bash coverage: `./scripts/test-coverage.sh`
6. pull request には次を含めてください。
   - 何を変えたか
   - なぜ変えたか
   - どのように検証したか

maintainer が管理する release や緊急対応を除き、`main` への直接 push は避けてください。

## この repository で重視すること

- active runtime docs が定義する externally observable behavior を、意図的に更新する場合を除いて維持する
- runtime、search、index、snapshot の責務分離を崩さない
- 正しさ、maintainability、testability に必要でない opportunistic refactor を避ける
- active runtime docs に明示されていない前提は文書化し、使った historical reference があれば併記する

## 関連ドキュメント

- ユーザーガイド: `README.md`
- 日本語ユーザーガイド: `README.ja.md`
- runtime contract docs: `docs/runtime-contract.ja.md`,
  `docs/retrieval-semantics.ja.md`, `docs/artifacts.ja.md`
- repository governance: `docs/repository-governance.ja.md`
- coding agent 向け repository policy: `AGENTS.md`
- Markdown authoring guide: `MARKDOWN_AUTHORING_GUIDE.md`
- 日本語 Markdown authoring guide: `MARKDOWN_AUTHORING_GUIDE.ja.md`
- 行動規範: `CODE_OF_CONDUCT.ja.md`
- セキュリティポリシー: `SECURITY.ja.md`
- サポートポリシー: `.github/SUPPORT.ja.md`
- ライセンス: `LICENSE` / `LICENSE.ja.md`
