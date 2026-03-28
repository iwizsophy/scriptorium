# Retrieval Semantics

この文書は、`scriptorium` runtime の外部から観測できる retrieval 挙動をまとめたものです。

runtime 全体の公開面は [Runtime Contract](./runtime-contract.ja.md)、artifact の build/load ルールは [Artifacts](./artifacts.ja.md) を参照してください。

## Query Normalization

- search text は matching 前に normalize されます
- full-text tokenization は CJK を含む mixed query を扱います
- path boost と label boost は normalized phrase と token match を使います

## `search`

`search` はランク付けされた docs/code ref を返します。

### 共通動作

- empty または whitespace-only query は結果なしになります
- `topK` と `snippetLines` は許容範囲内に clamp されます
- `extensions` により docs / code / 両方を切り替える
- result は dedupe 後、score、path、range で sort されます

### Docs Search

- 有効な docs index artifact があればまずそれを使います
- 無効または未設定なら Markdown filesystem scan に fallback します
- docs match は arbitrary line ではなく heading block 単位で返します
- heading text と file path は body-only match より強く boost されます

### Code Search

- snapshot-backed source は prebuilt snapshot を先に検索します
- snapshot path が使えない場合は configured filesystem root を scan します
- line match は matched line を中心にした `code_range` ref になります
- 同一 file 内で近接する hit は cluster され、最良の local result を残します
- line match がなくても path や source label が強く合う場合は line 1 anchored の file-level fallback を返せます

### Implementation Mode

`mode=implementation` は追加的な ranking hint です。

- startup / entrypoint file を boost
- DI / service registration hint を boost
- route / endpoint material を boost
- attribute / decorator pattern を boost
- config-oriented material を boost
- docs と code の両方に有用な match があれば、両 corpus から少なくとも 1 件ずつ返す方向に寄せます

### Snippets

- docs snippet は一致した heading block の先頭から切り出します
- code snippet は可能なら matched line を中心に取ります
- 長さは `snippetLines` で制御する

## `get_content`

`get_content` は 1 つの `refId` を解決します。

### 共通 mode

- `snippet`: anchor 周辺の focused range
- `full`: block または file 全体。ただし `maxLines` に従う
- `multi_range`: 重要行の周辺 range を強調表示

### Markdown Refs

- Markdown ref は heading block を解決します
- slug が file 内の heading block を指します
- `snippet` と `full` はその block に対して動作します
- `multi_range` は ordered list、bullet list、nested heading など flow に有用な行を中心に range を作ります

### Code Refs

- code ref は logical code path と line anchor を解決します
- `snippet` は requested line を中心に切り出します
- `full` は truncate 条件付きで file 全体を返します
- `multi_range` は anchor line、近傍の signature、route handler、そのほか注目すべき実装行を強調します

### File Refs

- file ref は line anchor なしで file path 全体を解決します
- docs-only mode では code source に対する code/file ref は使えません

## `expand_related`

`expand_related` は 1 つ以上の seed ref から supporting ref を探します。

- seed は Markdown または code context として読み込まれます
- overlap token、import-like token、path proximity、framework tag が score に寄与します
- result には opaque な score だけでなく、score の理由も含まれます
- docs と code の candidate は同じ response に混在できます

`mode=implementation` では startup wiring、DI、route、attribute/decorator、config link など framework-aware な relation を追加で考慮します。

## `summarize_flow`

`summarize_flow` は refs を決定的な step list に変換します。

### 抽出優先順位

Markdown:

1. numbered steps
2. `手順1` のような labeled step
3. bullet lists
4. nested headings
5. paragraph fallback

Code:

- route handler、function/class signature、認識可能な operation から candidate step を作ります
- style により step の compactness が変わります

### 出力

- `flow`: title、description、ref を持つ順序付き step
- `assumptions`: fallback が入った理由
- `confidence`: source quality と merge 安定性に基づく定性的な confidence

使える ref がない場合も protocol error にはせず、requested topic から fallback を作ります。

## `guide_implementation`

`guide_implementation` は retrieval tool を束ねて、根拠付きの implementation guide を 1 回で返します。

- `topic`, `framework`, `language` から query を構築します
- implementation-mode の docs/code search を実行します
- 最良の initial seed から supporting ref を追加収集します
- その ref を flow summary にして短い implementation guide を組み立てます
- `docs`, `sampleCode`, `assumptions`, `confidence` を分けて返します

guide は現在の corpus に grounding されます。supporting ref が足りない場合は、その制約を response に明記し、存在しない source を補完しません。
