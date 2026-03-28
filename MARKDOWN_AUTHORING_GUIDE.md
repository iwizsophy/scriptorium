# Markdown Authoring Guide For Scriptorium

English | [日本語](./MARKDOWN_AUTHORING_GUIDE.ja.md)

This guide documents the preferred Markdown shape for docs that will be parsed, indexed, searched, and summarized by the `scriptorium` runtime.

The runtime can read arbitrary Markdown, but some formats produce much better results for:

- `search`
- `get_content`
- `expand_related`
- `summarize_flow`

The recommendations below follow the current runtime behavior documented in [Runtime Contract](./docs/runtime-contract.md) and [Retrieval Semantics](./docs/retrieval-semantics.md), especially heading-block parsing, slug generation, `get_content(multi_range)`, and Markdown flow extraction.

## Why Format Matters

`scriptorium` treats Markdown as heading blocks.

- A block starts at a heading line.
- A block ends immediately before the next heading of the same or shallower depth.
- Search snippets come from the start of the matched block.
- `summarize_flow` prefers explicit numbered steps, then bullet lists, then nested headings, then paragraph fallback.

This means authoring quality directly affects retrieval quality.

## Recommended Structure

### 1. Use one topic per heading block

Prefer:

- one implementation concern per heading
- a short heading followed by a focused explanation
- splitting large pages into multiple subheadings when topics shift

Avoid:

- one heading that contains setup, configuration, usage, troubleshooting, and examples all mixed together
- very long blocks that bury the key implementation details far below the heading

Good:

```md
## Register the service
Add `builder.Services.AddXyz()` in `Program.cs`.

## Configure authentication
Set `Auth:Issuer` and `Auth:Audience` in `appsettings.json`.
```

Weak:

```md
## Setup
This section contains everything about the feature.
```

### 2. Make headings explicit and stable

Prefer headings that contain the actual action or concept name.

Good:

- `## Register the service`
- `## Configure the router`
- `## Add the middleware`
- `## 認証フロー`

Avoid vague headings such as:

- `## Overview`
- `## Notes`
- `## Misc`
- `## More`

Important:

- Heading slugs are derived from the heading text.
- Duplicate headings are allowed, but unique headings are better for stable `refId` targeting.
- Japanese headings are preserved as Japanese slugs, so natural Japanese headings are fine.

### 3. For procedures, write explicit numbered steps

If a section describes an implementation procedure, use numbered steps. This is the best format for `summarize_flow`.

Preferred forms:

```md
## Add authentication
1. Register the authentication handler.
2. Bind the configuration values.
3. Add the middleware to the request pipeline.
```

Also recognized:

```md
## Add authentication
手順1: Register the authentication handler.
手順2: Bind the configuration values.
手順3: Add the middleware to the request pipeline.
```

If the content is not sequential, use bullets instead of pretending it is a flow.

### 4. Keep implementation identifiers literal

Search and related-expansion quality improves when literal identifiers appear in the docs exactly as they appear in code.

Prefer including:

- API names
- type names
- package or namespace names
- configuration keys
- route patterns
- CLI command names
- file names such as `Program.cs`, `appsettings.json`, `vite.config.ts`

Good:

```md
Set `Auth:Issuer` in `appsettings.json` and register `AddJwtBearer()`.
```

Weak:

```md
Update the auth configuration and register the auth handler.
```

### 5. Keep examples near the explanation

If a heading explains one implementation step, keep the example in the same heading block instead of referring to a distant appendix.

Good:

~~~md
## Register the endpoint
Call `app.MapPost("/orders", HandleCreateOrder)`.

```csharp
app.MapPost("/orders", HandleCreateOrder);
```
~~~

This helps block-level search and `get_content` return useful context without requiring extra expansion.

### 6. Use nested headings for decomposing a larger flow

When a topic is too large for one step list, use a parent heading with child headings.

Good:

```md
# Order Processing

## Validate input
...

## Persist the order
...

## Publish the event
...
```

When no numbered steps exist, `summarize_flow` can fall back to nested headings.

### 7. Separate reference material from procedures

Reference pages and task pages should not be mixed unless the section boundary is clear.

Prefer:

- task-oriented pages: setup, migration, adding features, wiring components
- reference-oriented pages: option lists, API references, error catalogs

If both must exist in one file, separate them with headings so retrieval can land on the right block.

## Ideal Content Patterns

### Ideal setup section

~~~md
## Add the package
1. Add the `Example.Framework` package.
2. Restore dependencies.

## Register services
1. Call `builder.Services.AddExampleFramework()`.
2. Bind the `ExampleFramework` section from configuration.

```csharp
builder.Services.AddExampleFramework();
builder.Services.Configure<ExampleOptions>(
    builder.Configuration.GetSection("ExampleFramework"));
```
~~~

### Ideal configuration section

```md
## Configure ExampleFramework
Set the following keys in `appsettings.json`.

- `ExampleFramework:Endpoint`
- `ExampleFramework:ApiKey`
- `ExampleFramework:TimeoutSeconds`
```

### Ideal sample linkage

```md
## See also
- Sample app startup: `samples/web/Program.cs`
- Endpoint implementation: `samples/web/Features/Orders/CreateOrder.cs`
```

## Formats To Avoid

- Huge pages with only one or two headings
- Repeating the same generic heading text many times
- Procedure text written as one large paragraph with no steps
- Headings that do not mention the framework feature or action name
- Docs that omit literal identifiers and only describe concepts abstractly
- "See sample" without naming the sample file, route, type, or function

## Authoring Checklist

- Does each heading block cover one implementation concern?
- Are procedure sections written as numbered steps when order matters?
- Do headings contain the concrete action or feature name?
- Are API names, config keys, routes, and file names written literally?
- Is the most important example in the same heading block?
- If this page explains a flow, will nested headings still make sense if numbered steps are absent?

## Notes

- These are authoring recommendations, not hard validation rules.
- The runtime still accepts ordinary Markdown, but following this guide improves retrieval and summarization quality substantially.
