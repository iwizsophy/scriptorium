# Changelog

## v1.1.0 - 2026-04-04

### Changed

- Release archives now include `THIRD-PARTY-NOTICES.md`.
- Release archives now include a Syft-generated SPDX SBOM file:
  `scriptorium.sbom.spdx.json`.
- CI is explicitly configured to run on pushes to `main` and `develop`, while
  release publishing remains tag-driven.

### Documentation

- Runtime contract documentation now reflects server version `1.1.0`.
- Repository release documentation now links to these release notes for
  versioned operator-facing changes.
