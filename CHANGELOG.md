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
- _(add entries here)_

### Changed
- _(add entries here)_

### Deprecated
- _(add entries here)_

### Removed
- _(add entries here)_

### Fixed
- _(add entries here)_

### Security
- _(add entries here)_

---

## [0.2.0-foundry-pattern] - 2026-05-04

Cutover release for the **Foundry-pattern orchestration substrate**
(ADR-0037, supersedes ADR-0021). Apache Temporal and the Go workers
in `workers-go/` are gone; orchestration state now lives in Postgres
state machines + `libs/saga` + `libs/state-machine` + transactional
outbox + Debezium + Kafka, with pipeline runs materialised as
`SparkApplication` CRs and cron-driven dispatch owned by k8s
`CronJob`s. See
[`docs/architecture/foundry-pattern-orchestration.md`](docs/architecture/foundry-pattern-orchestration.md)
for the design and
[`docs/architecture/foundry-pattern-migration-closing-audit.md`](docs/architecture/foundry-pattern-migration-closing-audit.md)
for the close-out evidence (six green grep gates).

### Added
- ADR-0037 (Foundry-pattern orchestration) and ADR-0038 (event
  contract + idempotency).
- `libs/saga` (LIFO compensation runner with typed `saga.*.v1`
  events).
- `libs/state-machine` (`PgStore` with optimistic concurrency).
- `libs/idempotency` (`PgIdempotencyStore`, record-before-process
  invariant).
- `libs/outbox` (INSERT+DELETE same-TX pattern + Debezium
  `EventRouter` SMT).
- `services/workflow-automation-service` Postgres state machine +
  Kafka consumer + HTTP retries (Automate pattern, FASE 5).
- `services/automation-operations-service` saga substrate with
  `cleanup_workspace` (3-step) and `retention_sweep` (1-step) sagas
  plus a chaos test suite (FASE 6).
- `services/approvals-service` 5-state machine + dedicated
  `approvals-timeout-sweep` `CronJob` binary (FASE 7).
- `services/reindex-coordinator-service` Rust Kafka-driven
  coordinator replacing the Go `workers-go/reindex` worker (FASE 4).
- `schedules-tick` `CronJob` (Rust binary in `libs/event-scheduler`)
  driving cron dispatch from `schedules.definitions` rows.
- End-to-end smoke scenario
  [`smoke/scenarios/foundry-pattern-full-flow.json`](smoke/scenarios/foundry-pattern-full-flow.json),
  k6 latency bench at
  [`benchmarks/foundry-pattern/`](benchmarks/foundry-pattern/) and
  ChaosMesh SPOF-kill suite at
  [`infra/test-tools/chaos/foundry-pattern/`](infra/test-tools/chaos/foundry-pattern/)
  covering the four orchestration-plane SPOFs.
- Substantive [`CONTRIBUTING.md`](CONTRIBUTING.md) covering workflow,
  Conventional Commits, RFC process, PR checklist, review SLAs and
  service-creation guidelines.
- Substantive [`SECURITY.md`](SECURITY.md) with private reporting channels,
  triage SLAs, severity guidance, scope and safe-harbour terms.
- Domain-based [`.github/CODEOWNERS`](.github/CODEOWNERS) routing reviews
  across the 85+ services, shared libraries, protos, SDKs, infra and docs.
- This `CHANGELOG.md` with Keep a Changelog conventions.

### Changed
- **BREAKING:** Pipeline runs are now submitted as Spark Operator
  `SparkApplication` CRs by `pipeline-build-service`, replacing the
  Temporal `PipelineRun` workflow + `ExecutePipeline` activity pair.
  `restartPolicy.onFailureRetries` and `spec.timeToLiveSeconds` mirror
  the previous Temporal retry budget. Operators must install the
  Spark Operator chart (`infra/helm/operators/spark-operator/`).
- **BREAKING:** Cron / Time triggers are now dispatched by the
  `schedules-tick` Kubernetes `CronJob`, not by Temporal Schedules.
  `services/pipeline-schedule-service` continues to own the
  declarative `schedules.definitions` rows but no longer runs an
  in-process tick loop or a Temporal Schedules adapter.
- **BREAKING:** Replaced the Redis container image with **Valkey 8** (OSS,
  BSD-3-Clause fork hosted by the Linux Foundation) across the Compose stack.
  The Compose service is renamed `redis` → `valkey`, the volume `redis_data` →
  `valkey_data`, the image variable `OPENFOUNDRY_REDIS_IMAGE` →
  `OPENFOUNDRY_VALKEY_IMAGE` (default `valkey/valkey:8-alpine`), and the
  intra-cluster `REDIS_URL` now points to `redis://valkey:6379`. The Rust
  `redis-rs` client is unchanged; Valkey speaks the same wire protocol.
  Migration: `docker compose down` then `docker compose up -d` (the old
  `redis_data` volume is no longer referenced; recreate state if needed).

### Deprecated
- ADR-0021 (Temporal on Cassandra). Marked **Superseded by ADR-0037**;
  retained for historical context only.

### Removed
- **BREAKING:** `libs/temporal-client` Rust crate.
- **BREAKING:** `workers-go/` Go worker workspace (pipeline,
  workflow-automation, approvals, reindex). Run-time replacements
  ship as Rust services consuming Kafka events.
- **BREAKING:** `infra/helm/infra/temporal/` Helm chart and the
  Temporal frontend / history / matching / worker Deployments.
  `temporal-workers.yaml`, the Temporal devserver + UI services in
  `infra/compose/docker-compose.yml`, and the
  `temporal-history-kill.yaml` ChaosMesh schedule are also gone.
- **BREAKING:** Cassandra keyspaces `temporal_persistence` and
  `temporal_visibility`. New clusters never create them; brownfield
  clusters drop them via [`infra/runbooks/temporal.md`](infra/runbooks/temporal.md).
- `TEMPORAL_HOST_PORT`, `TEMPORAL_NAMESPACE`,
  `TEMPORAL_REQUIRE_REAL_CLIENT` and `TEMPORAL_TASK_QUEUE_*`
  environment variables are no longer injected by the
  `services.yaml` templates of `of-platform`, `of-data-engine` or
  `of-apps-ops`.
- `libs/testing/src/temporal.rs` and `libs/testing/src/go_workers.rs`
  testcontainer harnesses, plus the `.github/workflows/go-workers.yml`
  CI matrix.
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

[Unreleased]: https://github.com/open-foundry/open-foundry/compare/v0.2.0-foundry-pattern...HEAD
[0.2.0-foundry-pattern]: https://github.com/open-foundry/open-foundry/releases/tag/v0.2.0-foundry-pattern
