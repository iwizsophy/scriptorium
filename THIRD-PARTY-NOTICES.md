# Third-Party Notices

This document lists third-party Go modules currently included in the
repository dependency graph through `go.mod`.

## Scope

- Listed items cover modules explicitly present in `go.mod`, including indirect
  entries.
- The Go standard library is not listed here.
- Additional transitive dependencies that are not represented in `go.mod` are
  reviewed during dependency updates and release validation, but are not listed
  separately by default.

## Trademarks

- Product names, project names, module names, and logos referenced in this
  document remain the property of their respective owners.
- This document provides attribution and notice information only and does not
  grant any trademark license or imply endorsement.

## Current modules

### golang.org/x/text v0.35.0

- License: BSD 3-Clause
- Source: `golang.org/x/text`

### modernc.org/sqlite v1.48.0

- License: BSD 3-Clause
- Source: `modernc.org/sqlite`

### github.com/dustin/go-humanize v1.0.1

- License: MIT
- Source: `github.com/dustin/go-humanize`

### github.com/google/uuid v1.6.0

- License: BSD 3-Clause
- Source: `github.com/google/uuid`

### github.com/mattn/go-isatty v0.0.20

- License: MIT
- Source: `github.com/mattn/go-isatty`

### github.com/ncruces/go-strftime v1.0.0

- License: MIT
- Source: `github.com/ncruces/go-strftime`

### github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec

- License: BSD 3-Clause
- Source: `github.com/remyoudompheng/bigfft`

### golang.org/x/sys v0.42.0

- License: BSD 3-Clause
- Source: `golang.org/x/sys`

### modernc.org/libc v1.70.0

- License: BSD 3-Clause
- Source: `modernc.org/libc`

### modernc.org/mathutil v1.7.1

- License: BSD 3-Clause
- Source: `modernc.org/mathutil`

### modernc.org/memory v1.11.0

- License: BSD 3-Clause
- Source: `modernc.org/memory`

## Update policy

- Update this file when a dependency is added, removed, or its version changes
  in `go.mod`.
- Re-check license terms when dependency versions change.
- If a module ships multiple notices or mixed-license files, summarize that
  fact here and retain the upstream notice requirements in distributed
  materials when applicable.
