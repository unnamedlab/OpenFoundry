# Changelog

All notable changes to **OpenFoundry** are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

> **Conventions**
>
> - Each user-visible change must add an entry to the **`[Unreleased]`**
>   section in the same pull request that introduces it.
> - Entries are grouped by type: `Added`, `Changed`, `Deprecated`, `Removed`,
>   `Fixed`, `Security`.
> - Breaking changes are prefixed with **`BREAKING:`** and a short migration
>   note.
> - On release, the maintainers move the contents of `[Unreleased]` under a
>   new dated version heading and create a matching git tag (`vX.Y.Z`).
> - Pre-`1.0.0` releases follow SemVer with the caveat that minor versions
>   may contain breaking changes; these are always called out explicitly.

---

## [Unreleased]

### Added
- Substantive [`CONTRIBUTING.md`](CONTRIBUTING.md) covering workflow,
  Conventional Commits, RFC process, PR checklist, review SLAs and
  service-creation guidelines.
- Substantive [`SECURITY.md`](SECURITY.md) with private reporting channels,
  triage SLAs, severity guidance, scope and safe-harbour terms.
- Domain-based [`.github/CODEOWNERS`](.github/CODEOWNERS) routing reviews
  across the 85+ services, shared libraries, protos, SDKs, infra and docs.
- This `CHANGELOG.md` with Keep a Changelog conventions.

### Changed
- _(add entries here)_

### Deprecated
- _(add entries here)_

### Removed
- Qdrant se retira por restricción de licencia OSS; sustituto futuro: Vespa
  (Apache-2.0). Por ahora pgvector cubre el caso embebido. Se eliminan el
  servicio `qdrant` del compose, los volúmenes y variables
  `OPENFOUNDRY_QDRANT_*` / `QDRANT_URL`, las referencias en helm/terraform y
  el módulo vacío `libs/vector-store/src/qdrant.rs`.

### Fixed
- _(add entries here)_

### Security
- _(add entries here)_

---

<!--
Release template — copy under a new heading on every release:

## [X.Y.Z] - YYYY-MM-DD

### Added
### Changed
### Deprecated
### Removed
### Fixed
### Security

[X.Y.Z]: https://github.com/open-foundry/open-foundry/releases/tag/vX.Y.Z
-->

[Unreleased]: https://github.com/open-foundry/open-foundry/compare/main...HEAD
