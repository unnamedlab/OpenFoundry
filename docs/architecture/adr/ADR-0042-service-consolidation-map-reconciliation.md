# ADR-0042 — Service consolidation map reconciliation (4 undocumented directories)

* **Status:** Accepted (2026-05-05)
* **Amends:** [ADR-0030](ADR-0030-service-consolidation-30-targets.md)
  ("95 dirs → 33 ownership boundaries + 3 sinks")
* **Related:** [ADR-0021](ADR-0021-temporal-on-cassandra-go-workers.md)
  (Temporal workers, eventually replaced by Rust),
  [ADR-0036](ADR-0036-builds-foundry-parity.md) (builds / SparkApplication CRs),
  [ADR-0039](ADR-0039-media-sets-architecture.md) (media sets architecture),
  [ADR-0041](ADR-0041-iceberg-catalog-service.md) (Iceberg REST catalog).
* **Companion document:** [`docs/architecture/service-consolidation-map.md`](../service-consolidation-map.md)

## Context

The audit performed against `docs/architecture/service-consolidation-map.md`
on 2026-05-05 found that the map declared **95 directories** under
`services/`, but `ls services/ | wc -l` returns **99**. The four
directories the map did not enumerate are:

1. `iceberg-catalog-service`
2. `media-transform-runtime-service`
3. `pipeline-runner` (Spark/Scala image, not Rust)
4. `reindex-coordinator-service`

All four were already accepted by other ADRs or migration-plan tasks
that landed after ADR-0030 was written; only the consolidation map
was stale. None of them is a candidate for merger into an existing
ownership boundary, but two of them require classification choices
that the map's existing legend (`keep` / `merge` / `merged` /
`delete` / `sink`) does not unambiguously cover, hence this mini-ADR.

The decisions here are **documentation-only**: no code, no Cargo
workspace, no Helm chart, no Kafka topic and no Postgres schema is
moved as a result of this ADR. We are reconciling the map with the
state of the tree as it already exists.

## Decision

### 1. `iceberg-catalog-service` → `keep`

Active Foundry-native implementation of the Apache Iceberg REST
Catalog spec, currently at D1.1.8 phase 4/5 (Beta). Owns the
Foundry-side guarantees (all-or-nothing transactions across multiple
writes, marking inheritance with snapshot semantics, explicit schema
evolution) that ADR-0041 explicitly carved out from Lakekeeper's
external surface. It is its own ownership boundary, owns its own
Postgres schema and Cedar policy surface, and is consumed by
PyIceberg / Spark / Trino / Snowflake clients. There is no other
service in the consolidation map whose runtime owns the
`/iceberg/v1/...` surface, so a merge target does not exist.

### 2. `media-transform-runtime-service` → `keep`

ADR-0039 deliberately splits the media-sets domain into a metadata
plane (`media-sets-service`, owns transactions / item metadata /
presigned URLs) and a compute plane
(`media-transform-runtime-service`, executes the typed access
patterns: image / audio / video / document / spreadsheet, bills
compute-seconds via `libs/observability`, emits the
`media_set.access_pattern_invoked` audit envelope). Folding the
runtime into `media-sets-service` would couple media-set CRUD
latency to the heavyweight image/audio/video transform pool and
break the cost-attribution model that backs the Foundry usage-cost
table. The split is intentional and matches Foundry's own topology;
keep both as separate ownership boundaries.

### 3. `pipeline-runner` → new `image` classification

`pipeline-runner` is **not** a Rust workspace member. It is a
Scala 2.12 / SBT project (FASE 3 / Tarea 3.3) whose only output is
a Docker image, layered on top of `apache/spark:3.5.4`, that ships
the Iceberg-Spark binding and an entry-point JAR. Each pipeline run
is a fresh `SparkApplication` CR (rendered by
`infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml`)
that runs this image; the JVM `main` fetches the resolved transform
plan from `pipeline-build-service` over HTTP, executes it as Spark
SQL against the Iceberg/Lakekeeper catalog, and exits.

The map's existing statuses do not fit:

* `keep` and `merge` describe **ownership boundaries** in the Rust
  service taxonomy; `pipeline-runner` has no service binary, no
  Helm Deployment, no Kafka surface, no Postgres schema. It is a
  build artifact.
* `delete` would be wrong — the image is actively required by
  `pipeline-build-service` (FASE 3 / Tarea 3.4).
* `sink` is reserved for Kafka consumers that drain a topic into a
  storage tier, which is not what this image does.
* The "Retired service directories" section is for stubs that no
  longer exist on disk; `pipeline-runner` does exist on disk.

We therefore introduce a new `image` status in the legend, applied
exclusively to `pipeline-runner` for now. It is counted separately
from ownership boundaries (just like `sink`) and matches the
existing carve-out in `tools/regenerate_service_dockerfiles.py`'s
`NON_RUST_SERVICES` skip set, which already treats `pipeline-runner`
as a non-Rust directory.

### 4. `reindex-coordinator-service` → `keep`

FASE 4 / Tarea 4.2 Rust replacement for the Go `workers-go/reindex`
Temporal worker (per ADR-0021 and the Foundry-pattern orchestration
migration plan). It owns its own restart-safe state — the resume
cursor in `pg-runtime-config.reindex_jobs.resume_token` — and drives
Cassandra page-by-page scans via `cassandra-kernel`, fanning batches
out over `ontology.reindex.v1` to the `ontology-indexer` sink.

Because the coordinator owns Postgres state and Temporal-replacement
semantics that are independent from the indexer's stateless
batch-apply loop, it is **not** a `sink` (the existing label for
`ontology-indexer`) and **not** mergeable into `ontology-indexer`
without re-coupling the coordinator and the writer. It stays as its
own ownership boundary, mirroring how `ingestion-replication-service`
is kept distinct from its downstream `audit-sink` /
`event-streaming-service` consumers.

## Consequences

* `docs/architecture/service-consolidation-map.md` is updated to:
  * 99 directories on disk, 36 ownership boundaries (was 95 / 33).
  * New `image` status in the legend.
  * Four new alphabetically-placed rows.
  * Refreshed "Summary by status" totals
    (36 keep + 56 merge + 3 delete + 3 sink + 1 image = 99).
* The aggregate metric is now
  **36 ownership boundaries + 3 sinks + 1 non-Rust runtime image
  across 5 Helm releases**.
* ADR-0030's body (which references "95 dirs / 33 boundaries") is
  preserved as the historical record of the original consolidation
  decision. This ADR is the authoritative amendment to those numbers
  and the consolidation map links to both.
* No code, Cargo workspace member, Helm chart, Kafka topic or
  Postgres schema is changed by this ADR.

## Not chosen

* **Adding `pipeline-runner` to "Retired service directories".**
  Rejected: the directory is not retired, it is actively built and
  shipped as an image consumed by SparkApplication CRs.
* **Folding `media-transform-runtime-service` into
  `media-sets-service`.** Rejected: ADR-0039 explicitly separates the
  metadata plane from the compute plane so that transforms scale
  (and bill) independently of media-set CRUD.
* **Reclassifying `reindex-coordinator-service` as a `sink`.**
  Rejected: it owns Postgres state (the resume cursor) and is the
  Rust replacement for a Temporal worker, not a Kafka relay.
* **Folding `iceberg-catalog-service` under
  `dataset-versioning-service`.** Rejected: ADR-0041 carves the
  Foundry Iceberg semantics out of Lakekeeper precisely because they
  cannot be expressed at the dataset-versioning layer; the catalog
  needs its own ownership boundary.
