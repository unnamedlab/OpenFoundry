# Contributing to OpenFoundry

Thanks for your interest in contributing to **OpenFoundry**. This document is the
single source of truth for how to propose changes, what we expect from a pull
request, and how reviews and releases work. Read it before opening your first
PR — it will save you (and the maintainers) time.

> **TL;DR**
>
> 1. Open an issue first for anything non-trivial.
> 2. Fork, branch from `main`, follow [Conventional Commits](https://www.conventionalcommits.org/).
> 3. Run `just lint test` locally before pushing.
> 4. Keep PRs small (< ~400 lines diff), focused, and with tests.
> 5. Changes touching `libs/core-models`, `libs/auth-middleware`, `proto/**`
>    or any public SDK require an **RFC** (see below).

---

## Table of contents

- [Code of Conduct](#code-of-conduct)
- [Ways to contribute](#ways-to-contribute)
- [Project layout](#project-layout)
- [Development environment](#development-environment)
- [Workflow](#workflow)
- [Branch and commit conventions](#branch-and-commit-conventions)
- [Pull request checklist](#pull-request-checklist)
- [Review process](#review-process)
- [Tests and quality gates](#tests-and-quality-gates)
- [RFCs and breaking changes](#rfcs-and-breaking-changes)
- [Adding a new service](#adding-a-new-service)
- [Documentation contributions](#documentation-contributions)
- [Security issues](#security-issues)
- [Licensing and DCO](#licensing-and-dco)
- [Getting help](#getting-help)

---

## Code of Conduct

Participation in this project is governed by our
[Code of Conduct](CODE_OF_CONDUCT.md) (Contributor Covenant 2.1). By
contributing, you agree to uphold it. Report unacceptable behaviour to
`conduct@openfoundry.dev`.

## Ways to contribute

You don't need to write Rust to help. We welcome:

- **Bug reports** — use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.yml).
- **Feature proposals** — use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.yml).
- **New service proposals** — use the [new service template](.github/ISSUE_TEMPLATE/new_service.yml).
- **Documentation** improvements under [`docs/`](docs/).
- **SDK examples** in [`sdks/`](sdks/).
- **Plugins** built on top of [`libs/plugin-sdk`](libs/plugin-sdk).
- **Triage**: reproducing bugs, labelling issues, reviewing PRs.

If you are looking for a starting point, filter issues by
[`good first issue`](../../labels/good%20first%20issue) or
[`help wanted`](../../labels/help%20wanted).

## Project layout

OpenFoundry is a Cargo + pnpm + buf monorepo. The pieces you are most likely
to touch:

| Path | Purpose |
|------|---------|
| [`services/`](services/) | ~85 Rust microservices (one crate per service). |
| [`libs/`](libs/) | Shared Rust libraries (auth, event bus, plugin SDK, storage, etc.). |
| [`proto/`](proto/) | gRPC / Protobuf contracts. Source of truth for inter-service APIs and SDKs. |
| [`apps/web/`](apps/web/) | Frontend (TypeScript, pnpm workspace). |
| [`sdks/`](sdks/) | Generated SDKs for Python, TypeScript and Java. |
| [`docs/`](docs/) | Public documentation site (VitePress). |
| [`infra/`](infra/) | Local stack, Helm charts, Terraform, runbooks. |
| [`benchmarks/`](benchmarks/), [`smoke/`](smoke/) | Performance and end-to-end scenarios. |

A more detailed map lives in [`docs/guide/repository-map.md`](docs/guide/repository-map.md)
and [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Development environment

**Required tooling**

- Rust **1.85+** (workspace MSRV; `rustup default stable`).
- Node **20+** and **pnpm 9+** (`corepack enable`).
- [`just`](https://github.com/casey/just) task runner.
- Docker / Docker Compose (for the local stack).
- [`buf`](https://buf.build) for protobuf changes.

**First-time setup**

```bash
git clone https://github.com/open-foundry/open-foundry.git
cd open-foundry
just dev-stack       # Postgres, Redis, NATS JetStream, MinIO via docker compose
just build           # cargo build --workspace
just test            # cargo test --workspace
```

The full local-development guide lives at
[`docs/getting-started/local-development.md`](docs/getting-started/local-development.md).

**Useful recipes** (see [`justfile`](justfile) for the full list)

```bash
just build-svc <crate>     # build a single service
just test-svc  <crate>     # test a single crate
just lint                  # fmt-check + clippy -D warnings
just deny                  # cargo-deny (licenses + advisories)
just proto-gen             # regenerate code from proto/
just db-migrate            # run sqlx migrations for every service
```

## Workflow

1. **Search existing issues / discussions** to avoid duplication.
2. **Open an issue** for any change that is not a typo / docs fix /
   one-line bugfix. Get rough agreement on the approach before writing code.
3. **Fork** the repository and create a branch from `main`:
   `git checkout -b feat/ontology-bulk-import`.
4. **Implement** the change, adding tests and updating docs.
5. **Run quality gates locally**: `just lint test deny`.
6. **Push** and open a pull request against `main`.
7. **Iterate** with reviewers; keep the branch rebased on `main`.
8. A maintainer will **squash-merge** the PR once approved and CI is green.

We do **not** accept force-pushes to shared branches and we do **not** rebase
merge commits in `main`.

## Branch and commit conventions

- **Branch names**: `<type>/<short-description>` (e.g. `feat/nexus-bulk-export`,
  `fix/auth-jwt-leeway`, `docs/contributing-guide`).
- **Commits**: [Conventional Commits](https://www.conventionalcommits.org/).
  This is enforced in CI and drives the changelog.

  ```
  feat(ontology): add bulk import endpoint
  fix(auth-middleware): tolerate clock skew up to 60s
  docs(getting-started): document just dev-stack
  refactor(core-models): split dataset types into own module
  chore(deps): bump tokio to 1.40
  ```

  Allowed types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`,
  `ci`, `chore`, `revert`. A `!` after the scope (`feat(api)!: ...`) or a
  `BREAKING CHANGE:` footer flags a breaking change.

## Pull request checklist

Every PR must satisfy this checklist before review:

- [ ] Linked to an issue (`Closes #123`) when applicable.
- [ ] Diff stays focused; unrelated changes split into separate PRs.
- [ ] `just lint test` passes locally.
- [ ] New / changed behaviour covered by tests (unit, integration, or smoke).
- [ ] Public APIs (proto, SDK, REST) updated together with their generated
      artefacts (`just proto-gen`, `just sdk-typescript-gen`, etc.).
- [ ] Docs in [`docs/`](docs/) updated when behaviour or interfaces change.
- [ ] An entry added to the **Unreleased** section of [`CHANGELOG.md`](CHANGELOG.md)
      for any user-visible change.
- [ ] Migrations are **forward-only** and reversible scripts are provided
      (`cargo sqlx migrate add -r ...`).
- [ ] No secrets, credentials, or customer data committed.

PRs that fail any of the above will be sent back without a review round.

## Review process

- **Routing**: GitHub auto-assigns reviewers based on
  [`.github/CODEOWNERS`](.github/CODEOWNERS). At least **one CODEOWNER
  approval** is required for the touched paths.
- **SLA**: maintainers aim to triage within **3 business days** and to give a
  first review within **7 business days**. Ping the PR if you have not heard
  back after that window.
- **Merge strategy**: squash-merge with the PR title used as the commit
  message (must follow Conventional Commits).
- **Stale PRs** with no contributor activity for 30 days are auto-labelled
  `stale` and closed after 14 more days. Reopen freely when you have time.

## Tests and quality gates

CI runs on every PR (see [`.github/workflows/`](.github/workflows/)):

- `cargo fmt --all -- --check`
- `cargo clippy --workspace --all-targets -- -D warnings`
- `cargo test --workspace` (Postgres + Redis services spun up by CI)
- `cargo deny check`
- `buf lint` and `buf breaking` against `main`
- OpenAPI drift check and SDK typecheck (TypeScript / Python)
- Frontend lint, typecheck, unit and E2E tests
- Helm and Terraform validation

A PR cannot be merged with red CI. If a check is genuinely flaky, document it
in the PR and ping a maintainer; do **not** disable it.

**Coverage** — we are progressively introducing per-crate coverage gates with
`cargo llvm-cov`. New code in a touched crate should not lower its coverage.

## RFCs and breaking changes

Some changes need a written design before implementation:

- Any change to the **public API surface** (`proto/**`, generated SDKs,
  REST routes documented in OpenAPI).
- Any change to **`libs/core-models`** or **`libs/auth-middleware`** types
  re-exported by services.
- Introducing a new **cross-cutting library** under [`libs/`](libs/).
- Adding or removing a **service** under [`services/`](services/).
- Changes to the **storage schema** that require coordinated migrations
  across more than one service.

Process:

1. Open an issue using the `RFC: <title>` prefix and the
   `kind/rfc` label.
2. Fill the [RFC template](docs/adr/TEMPLATE.md) (context, decision,
   alternatives, consequences, migration plan).
3. Allow **7 days minimum** for community comments.
4. A maintainer marks the RFC as **accepted**, **rejected**, or
   **needs-revision**. Accepted RFCs land as a Markdown file under
   [`docs/adr/`](docs/adr/) and can then be implemented.

Breaking changes to protos must additionally:

- Bump the package version (`open_foundry.<domain>.v1` → `v2`).
- Keep the previous version compiling for at least **one minor release**.
- Be flagged with `!` and a `BREAKING CHANGE:` footer in the commit message.

## Adding a new service

We try to keep the 85+ services consistent. Before adding a new one:

1. Open a `new service` issue and get approval from a Platform CODEOWNER.
2. Use `just new-service <name>` (when available) or copy an existing,
   minimal service such as
   [`services/health-check-service`](services/health-check-service) as a
   template.
3. Register the crate in the root [`Cargo.toml`](Cargo.toml) workspace
   members **and** in [`.github/CODEOWNERS`](.github/CODEOWNERS).
4. Add proto definitions under [`proto/<domain>/v1/`](proto/) and regenerate.
5. Add at minimum: a `/health` endpoint, structured tracing via
   `core_models::observability::init_tracing`, sqlx migrations under
   `services/<name>/migrations/`, and a smoke scenario under
   [`smoke/scenarios/`](smoke/scenarios/).

## Documentation contributions

- The public site lives in [`docs/`](docs/) and is built with VitePress.
- Preview locally with `pnpm --filter docs dev`.
- Reference docs for SDKs are generated; **edit the source proto / Rust
  doc-comments**, not the generated output.

## Security issues

**Do not file security vulnerabilities as public issues.** Follow the
disclosure process documented in [`SECURITY.md`](SECURITY.md).

## Licensing and DCO

- OpenFoundry is licensed under **AGPL-3.0-only** (see [`LICENSE`](LICENSE)).
- By contributing you agree that your contribution is licensed under the
  same terms.
- All commits must be **signed off** with the
  [Developer Certificate of Origin](https://developercertificate.org/):

  ```bash
  git commit -s -m "feat(scope): your message"
  ```

  CI rejects PRs whose commits are not signed off.

## Getting help

- **Documentation**: <https://diocrafts.github.io/OpenFoundry/>
- **Discussions / Q&A**: GitHub Discussions on this repository.
- **Chat**: see the [Community](README.md#-community) section of the README.
- **Maintainer ping**: mention `@open-foundry/maintainers` on your issue or PR.

Thanks for helping make OpenFoundry better. 🛠️
