# Scriptorium 向け Markdown Authoring Guide

[English](./MARKDOWN_AUTHORING_GUIDE.md) | 日本語

この文書は、`scriptorium` runtime が解析、index 化、検索、要約の対象にする docs 向けに、望ましい Markdown の書き方をまとめたものです。

runtime 自体は一般的な Markdown を読めますが、次の用途では書き方によって結果の質がかなり変わります。

- `search`
- `get_content`
- `expand_related`
- `summarize_flow`

以下の推奨は、[Runtime Contract](./docs/runtime-contract.ja.md) と [Retrieval Semantics](./docs/retrieval-semantics.ja.md) にまとめた現行 runtime behavior、特に heading-block parsing、slug generation、`get_content(multi_range)`、Markdown flow extraction を前提にしています。

## なぜフォーマットが重要か

`scriptorium` は Markdown を heading block 単位で扱います。

- block は heading 行から始まる
- block は、次の「同じか浅い深さ」の heading の直前で終わる
- search snippet は一致した block の先頭から切り出される
- `summarize_flow` は numbered steps を最優先し、次に bullet list、次に nested headings、最後に paragraph fallback を使う

つまり、authoring quality が retrieval quality に直結します。

## 推奨構造

### 1. 1 heading block に 1 トピックを置く

推奨:

- 1 つの heading では 1 つの実装上の関心事だけを扱う
- 短い heading の直下に焦点の合った説明を置く
- 話題が切り替わるなら subheading に分割する

避けたい例:

- setup, configuration, usage, troubleshooting, examples を 1 つの heading に詰め込む
- 重要な実装情報が block のかなり下まで埋もれる長大な block

良い例:

```md
## サービスを登録する
`Program.cs` で `builder.Services.AddXyz()` を呼びます。

## 認証を設定する
`appsettings.json` に `Auth:Issuer` と `Auth:Audience` を設定します。
```

弱い例:

```md
## セットアップ
この節では機能に関するすべてをまとめて説明します。
```

### 2. heading は具体的で安定した名前にする

heading には、実際の action や concept 名を入れるのが望ましいです。

良い例:

- `## サービスを登録する`
- `## ルーターを設定する`
- `## ミドルウェアを追加する`
- `## 認証フロー`

避けたい曖昧な例:

- `## Overview`
- `## Notes`
- `## Misc`
- `## More`

重要:

- heading slug は heading text から作られる
- 同名 heading も使えるが、安定した `refId` を作りたいなら一意な heading のほうが良い
- 日本語 heading は日本語 slug のまま保持されるので、自然な日本語 heading をそのまま使ってよい

### 3. 手順を書くなら numbered steps にする

実装手順を説明する section では、numbered steps を使うのが最良です。これは `summarize_flow` に最も相性が良い形式です。

推奨形:

```md
## 認証を追加する
1. 認証ハンドラーを登録します。
2. 設定値をバインドします。
3. リクエストパイプラインにミドルウェアを追加します。
```

次の形式も認識されます:

```md
## 認証を追加する
手順1: 認証ハンドラーを登録します。
手順2: 設定値をバインドします。
手順3: リクエストパイプラインにミドルウェアを追加します。
```

順番が本質でない場合は、無理に手順化せず bullet list を使ってください。

### 4. 実装識別子は literal に書く

docs 内の識別子が code と同じ文字列で出てくるほど、search と related-expansion の質が上がります。

含めたいもの:

- API 名
- type 名
- package / namespace 名
- configuration key
- route pattern
- CLI command 名
- `Program.cs`, `appsettings.json`, `vite.config.ts` のような file 名

良い例:

```md
`appsettings.json` に `Auth:Issuer` を設定し、`AddJwtBearer()` を登録します。
```

弱い例:

```md
認証設定を更新し、認証ハンドラーを登録します。
```

### 5. 例は説明の近くに置く

ある heading が 1 つの実装ステップを説明しているなら、example も同じ heading block に置くのが望ましいです。離れた appendix に飛ばすより retrieval の質が上がります。

良い例:

~~~md
## エンドポイントを登録する
`app.MapPost("/orders", HandleCreateOrder)` を呼びます。

```csharp
app.MapPost("/orders", HandleCreateOrder);
```
~~~

こうしておくと、block-level search と `get_content` が追加の展開なしでも有用な文脈を返しやすくなります。

### 6. 大きな flow は nested headings で分解する

1 つの step list に収まらない topic なら、parent heading の下に child heading を置いてください。

良い例:

```md
# 注文処理

## 入力を検証する
...

## 注文を保存する
...

## イベントを発行する
...
```

numbered steps が無い場合でも、`summarize_flow` は nested headings を fallback として使えます。

### 7. reference material と procedure を分ける

reference page と task page は、section boundary が明確でない限り混在させないほうが良いです。

推奨:

- task-oriented pages: setup, migration, feature の追加, component の wiring
- reference-oriented pages: option list, API reference, error catalog

どうしても 1 ファイルに入れるなら、heading で明確に切って retrieval が適切な block に着地できるようにしてください。

## 理想的な内容パターン

### 理想的な setup section

~~~md
## パッケージを追加する
1. `Example.Framework` パッケージを追加します。
2. 依存関係を復元します。

## サービスを登録する
1. `builder.Services.AddExampleFramework()` を呼びます。
2. 設定から `ExampleFramework` セクションをバインドします。

```csharp
builder.Services.AddExampleFramework();
builder.Services.Configure<ExampleOptions>(
    builder.Configuration.GetSection("ExampleFramework"));
```
~~~

### 理想的な configuration section

```md
## ExampleFramework を設定する
`appsettings.json` に次のキーを設定します。

- `ExampleFramework:Endpoint`
- `ExampleFramework:ApiKey`
- `ExampleFramework:TimeoutSeconds`
```

### 理想的な sample linkage

```md
## 関連資料
- サンプルアプリの起動処理: `samples/web/Program.cs`
- エンドポイント実装: `samples/web/Features/Orders/CreateOrder.cs`
```

## 避けたいフォーマット

- heading が 1 個か 2 個しかない巨大な page
- 同じ generic heading text を何度も繰り返す page
- 手順があるのに大きな paragraph 1 つで書いている page
- framework feature や action 名が heading に出てこない page
- concept だけ説明して識別子を一切書かない page
- sample file, route, type, function を示さずに "See sample" だけで済ませる page

## Authoring Checklist

- 各 heading block は 1 つの実装関心事だけを扱っているか
- 順序が重要な section は numbered steps になっているか
- heading に具体的な action または feature 名が入っているか
- API 名、config key、route、file 名を literal に書いているか
- 一番重要な example を同じ heading block に置いているか
- numbered steps が無い場合でも nested heading だけで flow が読めるか

## Notes

- これは hard validation rule ではなく authoring recommendation です。
- runtime 自体は一般的な Markdown を受け付けますが、このガイドに沿うと retrieval と summarization の質は大きく上がります。
