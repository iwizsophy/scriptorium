# Repository Governance

この文書は、`scriptorium` の現行リポジトリガバナンスをまとめた参照文書です。

ランタイム挙動については、次のランタイム契約文書を参照してください。

- [ランタイム契約](./runtime-contract.ja.md)
- [取得セマンティクス](./retrieval-semantics.ja.md)
- [アーティファクト](./artifacts.ja.md)

## 現行の参照順

リポジトリ文書は、次の優先順で使ってください。

1. 外部から観測できるランタイム挙動については runtime contract docs
2. 現在の engineering workflow と repository policy についてはこの文書
3. coding agent の実行ルールについては [AGENTS.md](../AGENTS.md)
4. contributor 向け文書として [CONTRIBUTING.ja.md](../CONTRIBUTING.ja.md) や [README.ja.md](../README.ja.md)

## Repository Policy

- runtime、search、docs index、git snapshot の責務分離を保つこと。
- 挙動や policy が変わる作業では、同じ work item で behavior docs、operator docs、issue file を更新すること。
- 大きい作業や非自明な作業には、対応する markdown issue を `issues/` 配下に作成すること。
- 完了扱いにする前に validation を行うこと。
- coverage work は 100% を目標とし、技術的に安定して検証できない分岐だけを未カバーのまま残してよい。その場合は近くに `COVERAGE_EXCEPTION` コメントを置くこと。
- issue の scope が終わったら、次の issue に着手する前に status を `done` にすること。
- commit は 1 つの issue または policy work item に閉じること。

## Historical Issue Policy

- 既存 issue には移植期の参照が残っていて構いません。
- 古い issue を、履歴参照を消す目的だけで書き換えないでください。
- active work の対象になっており、履歴上の文面が曖昧さを生む場合に限って、古い issue を注記または更新してください。
