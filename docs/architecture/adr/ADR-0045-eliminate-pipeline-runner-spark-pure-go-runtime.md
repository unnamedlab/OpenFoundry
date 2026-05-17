# ADR-0045: Eliminate `pipeline-runner-spark`, port the pipeline runtime to pure Go

- **Status:** Proposed
- **Date:** 2026-05-17
- **Deciders:** Pipelines working group + Architecture committee (pending review)
- **Related ADRs:**
  - [ADR-0014](./ADR-0014-retire-trino-flight-sql-only.md) — Trino removal precedent (analytical SQL retired from the runtime path; Flight SQL gateway is the only edge BI surface).
  - [ADR-0036](./ADR-0036-builds-foundry-parity.md) — builds parity targets that the pipeline runner serves.
  - [ADR-0037](./ADR-0037-foundry-pattern-orchestration.md) — orchestration substrate used by build dispatching and outbox emission.
  - [ADR-0041](./ADR-0041-iceberg-catalog-service.md) — Iceberg REST Catalog used by the new Go runtime for `read_table` / `write_table` operations.
- **Related code (current Scala/Spark perimeter):**
  - [`services/pipeline-runner-spark/`](../../../services/pipeline-runner-spark/) — Scala 2.12 / Spark 3.5 sources: `PipelineRunner.scala`, `ActionLogStreamSink.scala`, `IcebergToObjectStoreIndexer.scala`, `build.sbt`, `Dockerfile`.
  - [`services/pipeline-runner/internal/runner/run.go`](../../../services/pipeline-runner/internal/runner/run.go) — Go orchestrator that today shells out to `spark-submit`.
  - [`services/pipeline-build-service/internal/spark/spark.go`](../../../services/pipeline-build-service/internal/spark/spark.go) — Go dispatcher that renders `SparkApplication` CRs and submits them to the K8s API.
  - [`services/pipeline-build-service/CLAUDE.md`](../../../services/pipeline-build-service/CLAUDE.md) — declares two execution paths (`DISTRIBUTED` / `FASTER`) and explicitly forbids introducing a third.
  - `infra/dev/{spark-smoke,spark-ingest-online-retail,action-log-sink,indexer-online-retail,poc-pipeline-nodes}.yaml` — `SparkApplication` CRs.
  - `infra/helm/infra/spark-jobs/` — Spark Operator chart + pipeline-run template.

## Context

OpenFoundry is documented as a **single Go module plus a React frontend**
(see [`CLAUDE.md`](../../../CLAUDE.md), § "What this repo is"). The
`services/pipeline-runner-spark/` directory is the only exception: it
ships ~250 LOC of Scala 2.12 backed by Spark 3.5 + Iceberg 1.5, packaged
as a fat JAR consumed by the Go orchestrator via `spark-submit`. The
Scala module fulfils three responsibilities:

1. **`PipelineRunner`** — receives an inline SQL string composed by
   `pipeline-build-service` (`internal/spark/spark.go::escapeYAMLString` →
   `--inline-sql` argument) and executes it against an Iceberg REST
   catalog, persisting the result via
   `df.writeTo(target).createOrReplace()` so a single atomic Iceberg
   snapshot is published. This is the "DISTRIBUTED" runtime path.
2. **`ActionLogStreamSink`** — Spark Structured Streaming consumer of
   the `ontology.actions.applied.v1` Kafka topic; parses the JSON
   envelope and appends to the Iceberg `lakekeeper.default.action_log`
   table with S3 checkpoints (exactly-once semantics).
3. **`IcebergToObjectStoreIndexer`** — reads an Iceberg table via Spark
   and emits one HTTP `PUT` per row against
   `object-database-service`. Production stand-in for the
   `tools/online-retail/seed_object_database.py` script.

This Scala perimeter creates four ongoing costs that the rest of the
monorepo does not pay:

- **Toolchain duplication.** Every engineer touching this surface needs
  JDK 21 + sbt + Scala 2.12 in addition to the Go toolchain. `make ci`
  does **not** cover Scala — there is no `sbt test` job, no Scala
  linter, no drift check, no coverage gate. The fat JAR is rebuilt
  inside a multi-stage Dockerfile (`pipeline-runner-spark/Dockerfile`)
  that is invisible to `make build-services`.
- **False positives in audits.** Repository-wide audits that walk
  `services/*` treat `pipeline-runner-spark` as a Go microservice and
  flag the absence of `cmd/`, `internal/{server,handlers,domain,repo}`,
  `go.mod` entry points, etc. The README mismatch surfaces on every
  consolidation pass.
- **Runtime topology coupling to Spark Operator.** Production runs
  require the `sparkoperator.k8s.io/v1beta2` CRDs, a Spark image baked
  with Iceberg + Hadoop-AWS bundles, AWS-credentials Secrets, and the
  Helm release at `infra/helm/infra/spark-jobs/`. Removing Spark from
  the platform requires removing this entire infrastructure layer.
- **Architectural drift against
  [`services/pipeline-build-service/CLAUDE.md`](../../../services/pipeline-build-service/CLAUDE.md).**
  The build service already declares two execution paths —
  `DISTRIBUTED` (Spark) and `FASTER` (in-process Go via
  `pipeline-expression`) — and the file explicitly forbids
  *"introducing a third execution path"*. The Spark path is the only
  one that requires an out-of-process JVM; collapsing it into the
  existing `FASTER` runtime is the policy-aligned simplification.

The technical premise that historically justified Spark — *"there is
no Go-native engine that can execute arbitrary SQL against an Iceberg
REST catalog"* — remains true. Apache `iceberg-go` (the upstream Go
client) provides catalog access, table reads and append writes, but
does **not** ship a SQL engine. Any pure-Go runtime must therefore
either (a) embed a SQL engine via cgo (DuckDB + its Iceberg extension
is the only credible option) or (b) **change the contract**: the
orchestrator stops accepting free-form SQL and instead accepts a
programmatic plan of relational operators (read_table → filter →
project → aggregate → write_table) that a Go interpreter executes
against `iceberg-go`. The `FASTER` runtime already speaks a variant of
this plan via `pipeline-expression`.

## Decision

1. **Delete `services/pipeline-runner-spark/`** (Scala 2.12 / sbt / fat
   JAR) and remove every reference to it from the Go orchestrator,
   Helm charts, dev manifests, docs, and ROADMAP.
2. **Collapse the `DISTRIBUTED` path into a Go-native runtime.** The
   `pipeline-build-service` stops composing free-form SQL; it emits a
   typed **operator plan** (`PipelineOpPlan`) that reuses the existing
   `pipeline-expression` evaluator that powers `FASTER`. The single
   surviving runtime executes both former paths.
3. **Break the free-form SQL contract.** The `--inline-sql` /
   `--inline-format` arguments are retired from
   [`services/pipeline-runner`](../../../services/pipeline-runner/) and
   from the `PipelineRunInput` struct in
   [`services/pipeline-build-service/internal/spark/spark.go`](../../../services/pipeline-build-service/internal/spark/spark.go).
   Pipelines that today rely on raw SQL must be re-expressed as
   operator DAGs; pipelines that are already authored as DAGs
   (the vast majority — `pipeline-build-service` derives SQL from a
   DAG node config) are unaffected at the authoring layer.
4. **Replace `IcebergToObjectStoreIndexer` with a Go indexer.** New
   binary `services/iceberg-object-indexer/` (or absorbed as a
   sub-command of an existing service — to be decided in Phase A) that
   reads an Iceberg table via `apache/iceberg-go` and emits the same
   `PUT /api/v1/object-database/objects/{tenant}/{id}` calls the
   Scala version produced. Same CLI surface (`--source-table`,
   `--target-tenant`, `--target-type-id`, `--id-column`,
   `--object-database-url`, `--internal-token`, `--limit`).
5. **Replace `ActionLogStreamSink` with a Go Kafka consumer**, written
   against `github.com/segmentio/kafka-go` (already a transitive
   dependency in the repo) or `github.com/twmb/franz-go`. The new
   binary appends to the Iceberg `action_log` table through
   `apache/iceberg-go`. **Streaming semantics are downgraded from
   Spark Structured Streaming exactly-once to at-least-once with
   downstream deduplication by `event_id`** (see
   [§ Consequences](#consequences)).
6. **Retire the Spark Operator footprint.** Delete the
   `infra/helm/infra/spark-jobs/` release, the
   `sparkoperator.k8s.io/v1beta2` CRD dependencies, the
   `openfoundry-iceberg` AWS Secret references that exist solely for
   Spark, and the dev manifests under
   `infra/dev/{spark-smoke,spark-ingest-online-retail,action-log-sink,indexer-online-retail}.yaml`.
   Replace each `SparkApplication` CR with a plain `Job` /
   `CronJob` / `Deployment` running the new Go binary.

The result: **the monorepo becomes 100% Go (plus React) with no JVM
in the runtime topology.** The single `pipeline-runner` binary
executes operator plans in-process against `iceberg-go` and the
`iceberg-catalog-service` REST API.

## Consequences

### Positive

- **Toolchain unification.** No more JDK / sbt / Scala 2.12 in CI,
  Dockerfiles or developer machines. `make ci` becomes the *only* gate
  the pipeline runtime needs to pass.
- **Audit cleanliness.** `services/*` becomes uniformly Go — the
  `services/template/` skeleton applies to every directory under
  `services/`. Repository-wide consolidation passes stop hitting
  `pipeline-runner-spark` as a false positive.
- **Smaller runtime footprint.** No Spark Operator, no Spark driver
  pods (cores=1, memory=1g per run), no AWS-credentials Secret
  plumbed solely for `spark.hadoop.fs.s3a.*`. The new Go binary runs
  as a normal `Job` with the standard service account.
- **One execution path.** The `DISTRIBUTED` / `FASTER` split that
  [`services/pipeline-build-service/CLAUDE.md`](../../../services/pipeline-build-service/CLAUDE.md)
  warns operators about disappears. The CLAUDE.md no-third-path rule
  is satisfied by reduction, not by addition.
- **Faster iteration.** A code change to the runtime no longer
  requires an sbt assembly + Docker image rebuild + Helm chart
  rebuild; the standard `go build` + `make build-services` flow
  applies.

### Negative — accepted trade-offs

- **Loss of arbitrary SQL.** Pipelines authored as free-form SQL (with
  `JOIN`, window functions, UDFs, multi-statement setup like
  `CREATE TEMP VIEW`) are no longer supported by the runtime. Authors
  must compose pipelines as DAGs of typed operators (the existing
  `pipeline-expression` vocabulary, extended as needed). A migration
  inventory must enumerate any production pipeline that depends on
  raw-SQL features before this ADR can move from Proposed → Accepted
  (see [§ Migration plan](#migration-plan), Phase 0).
- **Loss of distributed execution.** A single Go binary cannot scale
  across executors the way Spark does. Pipelines whose input dataset
  exceeds the working memory of a single pod must either be sharded
  upstream (partition-aware reads in `iceberg-go`) or rejected at
  build-validation time with a clear error. Today's
  `services/pipeline-build-service` already publishes the
  `pipeline_type=DISTRIBUTED` flag at the API layer; the field is
  preserved as a *capacity hint* but no longer dispatches to a
  different runtime.
- **Streaming semantics downgrade.** `ActionLogStreamSink`'s exactly-once
  guarantee (Spark Structured Streaming + S3 checkpoints) becomes
  at-least-once (Kafka consumer + offset commit after successful
  Iceberg append). Downstream queries on the `action_log` table must
  deduplicate by the immutable `event_id` field (already produced by
  `libs/ontology-kernel/handlers/actions/side_effects.go::publishActionAuditToKafka`).
  Acceptable for an audit timeline whose primary read pattern is
  point-in-time replay scoped by `event_id`; not acceptable for any
  downstream that aggregates by row count without dedup. The dedup
  contract must be documented at the `action_log` table definition.
- **`apache/iceberg-go` maturity risk.** The Go Iceberg client is
  newer and less battle-tested than Spark's Iceberg integration.
  Features we depend on (REST catalog auth, snapshot publication
  semantics, partition-aware reads, schema evolution) must be
  verified in Phase A before any further phases proceed. If a gap is
  discovered, Phase A returns to this ADR for re-evaluation rather
  than papering over it.

### Risks

- **Spec endpoint coupling.** `services/pipeline-build-service`
  currently serves the inline SQL via an HTTP endpoint the runner
  fetches at startup. The new operator-plan endpoint must be
  versioned (`/api/v1/pipelines/{id}/runs/{run_id}/plan` returning a
  `PipelineOpPlan` JSON) and gated behind a content-type negotiation
  so a partial rollout does not break in-flight runs.
- **Outbox compatibility.** Per
  [ADR-0037](./ADR-0037-foundry-pattern-orchestration.md), pipeline
  build events flow through the Postgres outbox + Debezium. Event
  shapes (`pipeline.build.started`, `pipeline.build.succeeded`,
  `pipeline.build.failed`) must remain wire-compatible across the
  migration — consumers downstream (audit-compliance-service,
  notifications) are unaware of the Spark→Go switch.
- **Helm chart split impact.** The Spark Operator lives in its own
  release per [ADR-0031](./ADR-0031-helm-chart-split-five-releases.md).
  Removing it requires updating the five-release manifest and the
  ArgoCD `Application` ordering so the runtime release does not
  reference the removed CRDs.

## Migration plan

Each phase is a separate PR. The next phase does not start until the
current one has merged with `make ci` + integration tests green.

### Phase 0 — Inventory (no code)

Before any code changes:

1. Enumerate every production pipeline whose DAG node config resolves
   to raw SQL beyond a simple `SELECT … FROM input WHERE … GROUP BY …`
   shape. Source: `pipeline_authoring.published_dag` JSON column in
   `pg-pipeline`.
2. For each, classify: (a) re-expressible as a typed operator DAG with
   the current `pipeline-expression` vocabulary, (b) requires extending
   the vocabulary (multi-table join, window function, UDF), or (c)
   blocked (depends on Spark-specific behaviour with no Go analogue).
3. Publish the inventory as `docs/migration/pipeline-runner-spark-to-go-inventory.md`.
   If any pipeline lands in bucket (c), this ADR is amended (or rejected)
   before Phase A starts.

### Phase A — Iceberg → object-database indexer in Go

Smallest, lowest-risk leaf node. Validates `apache/iceberg-go` against
production data without touching the runtime.

1. New module under `services/iceberg-object-indexer/` following the
   `services/template/` skeleton. CLI surface identical to the Scala
   `IcebergToObjectStoreIndexer`.
2. Reads via `iceberg.io/catalog/rest` against
   `iceberg-catalog-service` (ADR-0041).
3. Issues `PUT /api/v1/object-database/objects/{tenant}/{id}` per row
   using the standard `libs/auth-middleware` client.
4. Replace `infra/dev/indexer-online-retail.yaml` (SparkApplication CR)
   with a plain `Job` running the new binary.
5. Smoke: run against the `lakekeeper.default.online_retail_clean`
   table from the online-retail PoC; row count must match the Scala
   baseline.
6. **Exit criterion:** `apache/iceberg-go` handles every read path we
   need at production scale. If it doesn't, this ADR is revised.

### Phase B — Kafka → `action_log` sink in Go

Independent of Phase C. Replaces `ActionLogStreamSink`.

1. New module `services/action-log-sink/` (or fold into the existing
   audit pipeline if there is a natural home).
2. Kafka consumer with manual offset commit *after* successful Iceberg
   append. Document the at-least-once contract in the `action_log`
   table README.
3. Append via `iceberg-go` writeBuilder against the Iceberg catalog.
4. Replace `infra/dev/action-log-sink.yaml` (SparkApplication CR) with
   a `Deployment` running the new binary. Configure
   `terminationGracePeriodSeconds` so in-flight batches commit before
   shutdown.
5. **Exit criterion:** stream lag stays within the same envelope as
   the Spark version under representative `online_retail` action load;
   no event_id is observed missing across a controlled restart.

### Phase C — Pipeline runtime: operator plan in Go (the hard part)

The replacement of `PipelineRunner` and the contract break with
`pipeline-build-service`.

1. Define the `PipelineOpPlan` schema (proto under `proto/pipeline/v1/`
   if cross-service; Go struct if internal only). Operators in scope
   for v1: `read_table`, `filter`, `project`, `rename`, `cast`,
   `aggregate`, `union`, `limit`, `write_table`. `join` is explicitly
   deferred to v2 unless the Phase 0 inventory requires it.
2. Extend `pipeline-expression` (or stand up a thin sibling library)
   with an interpreter that takes a `PipelineOpPlan` and a row stream
   from `iceberg-go` and emits a row stream to `iceberg-go`'s
   write builder. Persist the result via the catalog's
   transaction-commit endpoint (ADR-0041, §2) so the snapshot
   publishes atomically — matching the previous
   `df.writeTo(...).createOrReplace()` semantics.
3. Rewrite
   [`services/pipeline-build-service/internal/spark/spark.go`](../../../services/pipeline-build-service/internal/spark/spark.go)
   to emit `PipelineOpPlan` instead of `SparkApplication` CRs. Rename
   the package from `spark` to `dispatch` (or similar) and delete the
   YAML template + K8s CR client. The new dispatcher submits a
   `Job` to K8s (or invokes the runner in-process for `FASTER`-class
   pipelines) and tracks status via the standard Kubernetes Job
   condition + the existing outbox events.
4. Rewrite
   [`services/pipeline-runner/internal/runner/run.go`](../../../services/pipeline-runner/internal/runner/run.go)
   to fetch a `PipelineOpPlan` from the build service, execute it
   in-process, and surface results / failures through the same log
   prefix format the orchestrator already uses.
5. Delete the `--inline-sql` / `--inline-format` arguments from the
   runner CLI, the build-service render path, the Helm template, the
   dev manifests, and `services/pipeline-runner/README.md`.
6. **Exit criterion:** the online-retail PoC pipeline produces an
   identical output Iceberg snapshot (schema + row count + value
   equivalence) under the new runtime. Existing unit + integration
   tests pass; new integration tests cover the
   `PipelineOpPlan` → snapshot path end-to-end.

### Phase D — Infra cleanup

Last phase, only after Phases A–C have been merged and stable for at
least one release cycle.

1. Delete `services/pipeline-runner-spark/` (Scala sources, build.sbt,
   project/, Dockerfile, README).
2. Delete `infra/helm/infra/spark-jobs/` (Spark Operator chart +
   `_pipeline-run-template.yaml`).
3. Update `infra/helm/apps/` and `infra/argocd/apps/` to drop the
   Spark Operator release from the dependency graph.
4. Remove `sparkoperator.k8s.io/v1beta2` CRD installation from any
   bootstrap manifest.
5. Strip the `OF_PIPELINE_RUNNER_SPARK_*` env vars from the runner
   docs and any sample configs.
6. Update `ROADMAP.md`, `PoC/*.md`, `docs/poc-online-retail/*.md`,
   `docs/reference/repository-layout.md`,
   `docs/architecture/services-and-ports.md`,
   `docs/architecture/runtime-topology.md`,
   `docs/data-connectivity/index.md`,
   `docs/ontology-building/indexing-and-materialization.md`, and any
   plugin SDK docs that reference Spark or the Scala runner. Each
   should reference this ADR.
7. Update the top-level [`CLAUDE.md`](../../../CLAUDE.md) sentence
   *"Single Go module (`github.com/openfoundry/openfoundry-go`) plus a
   React frontend"* — remove the implied "with one Scala module"
   caveat that the Scala directory created.

## Alternatives considered

### Alternative 1: Keep Scala, just relocate to `compute/spark-runner/`

The original ask. Solves the audit false-positive but keeps the
toolchain duplication, the Spark Operator dependency, and the
DISTRIBUTED/FASTER split. **Rejected** as a half-measure that
extends the technical debt without retiring it. If the platform's
identity is *"Go monorepo"*, having a Scala directory in any path is
the same drift.

### Alternative 2: DuckDB embedded via cgo (`github.com/marcboeker/go-duckdb`) with the Iceberg extension

Would preserve arbitrary SQL support. Single Go binary at the
artifact level. **Rejected** because:

- Not pure Go — cgo dependency reintroduces a non-Go build step,
  defeats `CGO_ENABLED=0` static builds, and complicates the
  multi-arch container build that the repo otherwise gets for free.
- DuckDB's Iceberg extension is read-only at the time of writing;
  write paths still require a separate writer (back to `iceberg-go`).
- The maintenance surface is *larger*, not smaller: now we depend on
  Spark-equivalent SQL semantics through a different engine with
  different edge cases.

### Alternative 3: Delegate SQL to an external Trino / Spark Connect server

Run a shared Trino cluster (or SparkConnect) and have the Go runner
issue SQL over HTTP/gRPC. **Rejected** because:

- Directly contradicts [ADR-0014 (retire Trino, single Flight SQL
  gateway)](./ADR-0014-retire-trino-flight-sql-only.md). Re-introducing
  Trino for the runtime path would reverse that decision.
- Moves the JVM out of the binary but back into the platform; the
  Spark Operator footprint is replaced by a Trino footprint, which is
  not a net simplification.

### Alternative 4: Delete the whole `DISTRIBUTED` path, keep only `FASTER`

`pipeline-build-service` already has a fully Go runtime under
`internal/handler/lightweight_runtime.go`. **Considered but
deferred** — it is the right end state, but cannot be adopted in one
shot without the Phase 0 inventory: any production pipeline currently
dispatched as `DISTRIBUTED` would break. This ADR's Decision is the
incremental version of this alternative: collapse the two paths into
one runtime that subsumes both surfaces.

### Alternative 5: Status quo

Keep `pipeline-runner-spark`. **Rejected** — the architectural drift
keeps compounding (every new audit, every new contributor onboarding,
every infra reconciliation pays the Scala tax), and the value Spark
delivers today (distributed SQL on Iceberg) is not exercised by any
known production pipeline at OpenFoundry's current scale. The
`FASTER` path's existence is evidence that the platform has already
moved away from distributed execution as a default.

## Open questions

These are flagged for the working group before this ADR can move to
Accepted:

1. **Phase 0 inventory result.** Without it, the bucket (c)
   pipelines that have no Go analogue are unknown. If bucket (c) is
   non-empty, this ADR is amended.
2. **Operator vocabulary scope for v1.** `join` is deferred above.
   Confirm with the pipeline authoring team that the published DAGs
   do not require it on the runtime side (joins resolved at the
   ontology query layer are out of scope).
3. **Streaming dedup contract.** Confirm with downstream consumers of
   the `action_log` table that at-least-once + `event_id` dedup is
   acceptable. The audit-compliance dashboards must be checked for
   any `COUNT(*)` aggregations without an upstream `DISTINCT
   event_id`.
4. **Distributed pipelines beyond single-pod memory.** Confirm there
   is no in-production pipeline whose dataset exceeds what a single
   Go pod can process. If there is, the runtime needs sharded
   partition reads (`iceberg-go` partition pruning) before Phase C
   ships.
