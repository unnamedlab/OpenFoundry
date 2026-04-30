# Runtime Topology

The runtime shape of OpenFoundry is easiest to understand as a layered
service mesh behind a single gateway, organised into **five target
planes**: *storage*, *ingestion*, *compute*, *control* and *relational
state*. Each plane has an owning ADR (0008–0012) that fixes its
contracts, operators and SLOs; the diagram below replaces the older flat
fan-out under `gateway` with that target shape.

## High-Level Flow — target planes

```text
                                     External BI clients
                                  (Tableau / Superset / Arrow Flight SQL JDBC)
                                              |
                                              v
                                   +----------------------+
                                   | sql-bi-gateway       |   <-- single edge SQL
                                   |  service (:50133)    |       surface; real
                                   |  Apache Arrow        |       Flight SQL server
                                   |  Flight SQL server   |       (DataFusion +
                                   |  ADR-0014            |        multi-backend
                                   +----------------------+        routing)
                                              :
                                              : (per-statement routing into the
                                              :  COMPUTE plane: Iceberg, ClickHouse,
                                              :  Vespa, Postgres — see ADR-0014)
                                              :
Browser / API Client                          :
        |                                     :
        v                                     :
  apps/web or external client                 :
        |                                     :
        v                                     :
   gateway (HTTP entrypoint) -----------------+
        |
        |     ┌──────────────────────────── CONTROL PLANE ───────────────────────────┐
        +---> │ identity-federation-service · auth-service ·                         │
        |     │ tenancy-organizations-service · ontology-definition-service ·        │
        |     │ ontology-actions-service · ontology-security-service ·               │
        |     │ workflow-automation-service · pipeline-schedule-service ·            │
        |     │ notification-alerting-service · audit-service                        │
        |     │   ── async signalling on NATS JetStream (libs/event-bus-control)     │
        |     │      governed by ADR-0011 contract lint; SLOs in ADR-0012            │
        |     └──────────────────────────────────────────────────────────────────────┘
        |
        |     ┌────────────────────────── INGESTION PLANE ───────────────────────────┐
        +---> │ data-connector · connector-management-service ·                      │
        |     │ ingestion-replication-service · streaming-service ·                  │
        |     │ ontology-funnel-service                                              │
        |     │   ── durable streams on Apache Kafka (libs/event-bus-data),          │
        |     │      ack=all + idempotent producers; OpenLineage headers             │
        |     │      → lands data into the storage plane                             │
        |     └──────────────────────────────────────────────────────────────────────┘
        |                                     │
        |                                     v
        |     ┌─────────────────────────── STORAGE PLANE ────────────────────────────┐
        |     │ Object storage:  Rook Ceph (RBD-fast block + RGW S3 EC 4+2)          │
        |     │ Lakehouse:       Apache Iceberg + Lakekeeper REST Catalog            │
        |     │                   (single REST catalog — ADR-0008)                   │
        |     │ Streaming log:   Apache Kafka (Strimzi, rack-aware)                  │
        |     │ Time-series:     ClickHouse cluster (shards=2, replicas=3)           │
        |     │ Search/hybrid:   Vespa (production) + Vespa Lite (DX, ADR-0007)      │
        |     │ Embedded vec:    pgvector co-located with relational state           │
        |     │ Maintenance:     Flink jobs for Iceberg rewrite / expire / orphans   │
        |     └──────────────────────────────────────────────────────────────────────┘
        |                ^                    ^                    ^
        |                | (catalog/scan)     | (CDC/firehose)     | (writes)
        |                |                    |                    |
        |     ┌─────────────────────────── COMPUTE PLANE ────────────────────────────┐
        +---> │ sql-bi-gateway-service  (Flight SQL gateway, :50133 — ADR-0014)       │
        |     │   ├── sql-warehousing-service (DataFusion / Iceberg, Flight SQL P2P, │
        |     │   │     :50123) — official internal compute pool (ADR-0009)          │
        |     │   ├── ClickHouse  (time-series queries)                              │
        |     │   ├── Vespa       (search / hybrid retrieval, ADR-0007)              │
        |     │   └── Postgres    (OLTP reference catalogue — CNPG)                  │
        |     │ ml-service · ai-service · ontology-functions-service ·               │
        |     │ ontology-query-service · notebook-runtime-service ·                  │
        |     │ report-service · geospatial-service · pipeline-build-service ·       │
        |     │ pipeline-authoring-service · lineage-service                         │
        |     │   ── service-to-service SQL travels exclusively over Flight SQL P2P  │
        |     │      (ADR-0009); end-to-end latency budgets in ADR-0012              │
        |     └──────────────────────────────────────────────────────────────────────┘
        |
        |     ┌──────────────────── RELATIONAL STATE PLANE ──────────────────────────┐
        +---> │ Service-owned PostgreSQL databases, one per bounded service          │
        |     │ Operator:  CloudNativePG (CNPG) — single Postgres operator           │
        |     │            (ADR-0010), HA with synchronous replicas                  │
        |     │ Backups:   barman-cloud → RGW (storage plane), PITR                  │
        |     │ Extensions: pgvector for co-located embedded vector search           │
        |     └──────────────────────────────────────────────────────────────────────┘
        |
        +---> other bounded services (app-builder-service, marketplace-service,
              code-repo-service, fusion-service, document-reporting-service,
              nexus-service, …) — each anchored to one of the five planes above.
```

> `sql-bi-gateway-service` is the **single** edge SQL surface for
> external BI clients (Tableau, Superset, Arrow Flight SQL JDBC). It is
> a real Apache Arrow Flight SQL server (not a proxy) backed by
> DataFusion that classifies each statement and routes it to the
> appropriate compute-plane backend (Iceberg via `sql-warehousing-service`,
> ClickHouse, Vespa, Postgres). The retired Trino edge BI deployment is
> superseded — see
> [ADR-0014](./adr/ADR-0014-retire-trino-flight-sql-only.md).
> Internal services still reach data through Flight SQL P2P via
> `sql-warehousing-service` per
> [ADR-0009](./adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md).

### Plane → owning ADR

| Plane                | Owning ADR(s)                                                                                                                                                                                                                       |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Storage**          | [ADR-0008 — Iceberg REST Catalog (Lakekeeper only)](./adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md)                                                                                                                              |
| **Ingestion**        | [ADR-0011 — Control vs Data bus contract](./adr/ADR-0011-control-vs-data-bus-contract.md) (Kafka data plane)                                                                                                                        |
| **Compute**          | [ADR-0009 — Internal query fabric: DataFusion + Flight SQL](./adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md)                                                                                                           |
| **Control**          | [ADR-0011 — Control vs Data bus contract](./adr/ADR-0011-control-vs-data-bus-contract.md) (NATS JetStream control plane)                                                                                                            |
| **Relational state** | [ADR-0010 — CloudNativePG as the single Postgres operator](./adr/ADR-0010-cnpg-postgres-operator.md)                                                                                                                                |
| **Cross-plane SLOs** | [ADR-0012 — Data-plane SLOs, SLIs and error budgets](./adr/ADR-0012-data-plane-slos.md)                                                                                                                                             |

## Service Families

| Family | Services |
| --- | --- |
| Entry and experience | `gateway`, `app-builder-service`, `marketplace-service`, `notebook-runtime-service`, `document-reporting-service`, `notification-alerting-service` |
| Data plane | `data-connector`, `dataset-service`, `pipeline-service`, `sql-bi-gateway-service`, `streaming-service`, `report-service`, `geospatial-service`, `fusion-service`, `workflow-automation-service` |
| Governance and semantics | `auth-service`, `audit-service`, `ontology-definition-service`, `object-database-service`, `ontology-query-service`, `ontology-actions-service`, `ontology-security-service`, `ontology-funnel-service`, `ontology-functions-service`, `nexus-service` |
| Developer platform | `code-repo-service` |
| AI and ML | `ai-service`, `ml-service` |

## Shared Runtime Dependencies

The repo and workflows indicate a consistent set of platform dependencies,
each anchored to one of the planes above:

- **Postgres** for service-owned relational state — provisioned and
  operated exclusively through CloudNativePG, see
  [ADR-0010](./adr/ADR-0010-cnpg-postgres-operator.md).
- Redis for gateway-oriented caching and coordination
- **NATS JetStream** for async messaging (control plane — see
  "Control vs Data" below and
  [ADR-0011](./adr/ADR-0011-control-vs-data-bus-contract.md))
- **Apache Kafka** for high-throughput streaming (data plane — see
  "Control vs Data" below and
  [ADR-0011](./adr/ADR-0011-control-vs-data-bus-contract.md))
- **Object storage** for datasets, archives, reports, and repository
  payloads — Rook Ceph (RBD-fast block + RGW S3 EC 4+2). The lakehouse
  layer on top of this storage uses Apache Iceberg through a single
  Lakekeeper REST Catalog, see
  [ADR-0008](./adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md).
- Vespa for production search **and** local DX (single-node
  `vespaengine/vespa` container in `infra/docker-compose.yml`); pgvector
  for embedded vector search co-located with relational state.
  Meilisearch is no longer a default DX dependency — it is kept as an
  opt-in first-run demo under `--profile demo` of
  `infra/docker-compose.dev.yml`. See
  [ADR-0007](./adr/ADR-0007-search-engine-choice.md).

The CI smoke job creates multiple service-specific databases, which strongly suggests database-per-service isolation rather than a shared operational schema.

## Control Plane vs Data Plane (Event Bus split)

Events on the platform travel over **two distinct buses**, each tuned for very
different workloads. Services pick the one that matches the message they
emit; many services touch both. The contract that prevents accidental
cross-plane coupling is enforced by `tools/bus-lint/check_bus.py` and
formalised in
[ADR-0011](./adr/ADR-0011-control-vs-data-bus-contract.md); end-to-end
latency budgets for both buses live in
[ADR-0012](./adr/ADR-0012-data-plane-slos.md).

| Plane             | Crate                | Transport          | Latency  | Retention   | Throughput | Typical traffic                                                     |
| ----------------- | -------------------- | ------------------ | -------- | ----------- | ---------- | ------------------------------------------------------------------- |
| **Control plane** | `libs/event-bus-control` | NATS JetStream | µs–ms    | hours/days  | MB/s       | RPC-ish events, signals, fan-out, notifications, workflow triggers  |
| **Data plane**    | `libs/event-bus-data`    | Apache Kafka   | ms       | weeks–PB    | GB–PB/s    | CDC streams, ingestion firehoses, lineage, analytics, audit archive |

### Why two buses

- **Control traffic** is dominated by small, latency-sensitive messages
  (e.g. "refresh dataset quality", "workflow trigger requested",
  "notification updated"). It needs sub-millisecond fan-out, ephemeral
  consumers, and short retention. NATS JetStream is the right shape for
  this.
- **Data traffic** is dominated by large volumes that must be replayable
  for hours or days (CDC, ingestion, lineage events feeding the catalog),
  and is consumed by both online services and batch/analytics jobs.
  Kafka's partitioned, long-retention log model is the right shape for
  this — and most third-party data tooling (Flink, Spark, Iceberg
  ingest paths) expects Kafka.

Splitting the buses also gives us independent operational envelopes:
control-plane outages don't block data ingestion, and a runaway data
producer can't starve control signals.

### Delivery semantics

- `event-bus-control` (NATS JetStream): durable streams, at-least-once
  with consumer ack windows. Defaults to 7-day retention and 1M-message
  caps per stream — see `libs/event-bus-control/src/subscriber.rs`.
- `event-bus-data` (Kafka): at-least-once with **explicit commits**.
  `enable.auto.commit=false` and consumers must call
  `DataMessage::commit()` (or `DataSubscriber::commit_offsets()`) once a
  record is durably processed. Producers run with `acks=all` and
  idempotence enabled.

### Topic / subject governance

- Both buses **disable broker-level auto-creation** of topics/subjects.
  Topics are provisioned out of band by the platform's topic registry so
  ownership, retention, partitions, and ACLs are managed as code.
- On Kafka, each service authenticates with its own SASL principal
  (`ServicePrincipal::scram_sha_512`). Broker ACLs grant
  `Allow Read`/`Allow Write` on topic prefixes by service identity rather
  than by IP or shared credentials.
- On NATS, equivalent isolation is enforced via per-account credentials
  and subject permissions configured in the JetStream account graph.

### OpenLineage propagation

Every record published through `event-bus-data` carries a small set of
well-known Kafka headers modelling the OpenLineage facets the platform
propagates through pipelines:

| Header         | Meaning                                                  |
| -------------- | -------------------------------------------------------- |
| `ol-namespace` | OpenLineage `namespace` (e.g. `of://datasets`)           |
| `ol-job-name`  | OpenLineage `job.name` of the producing job              |
| `ol-run-id`    | OpenLineage `run.runId`                                  |
| `ol-event-time`| RFC 3339 timestamp for this record                       |
| `ol-producer`  | Producer identity URL (per OpenLineage spec)             |
| `ol-schema-url`| Optional schema/contract URL for the payload (when known)|

This lets `lineage-service` and downstream consumers reconstruct dataset
lineage without a separate side-channel. See
`libs/event-bus-data/src/headers.rs`.

### Picking the right bus

A quick rule of thumb when adding a new event:

- If the message represents a **command, signal, or short-lived
  notification** that should be acted on immediately and discarded, use
  `event-bus-control`.
- If the message represents **durable state change in a dataset or pipeline**
  that downstream analytics/lineage/audit consumers need to replay, use
  `event-bus-data`.

## Internal query fabric

Service-to-service SQL inside the platform runs over **Flight SQL P2P**, not
through a central federated coordinator. See
[ADR-0009](./adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md).

- `libs/query-engine/` provides `FlightSqlTableProvider`, which lets any
  service consume another service's result set as a DataFusion table over
  Arrow Flight SQL.
- `services/sql-warehousing-service` (port `50123`) is the official
  DataFusion **compute pool** for shared analytical execution (Iceberg
  scans, larger joins).
- `services/sql-bi-gateway-service` (port `50133`) is the **edge SQL
  router** that fans out external SQL to the right backend:
  - `sql-warehousing-service` for Iceberg / lakehouse SQL,
  - ClickHouse for time-series,
  - Vespa for search / hybrid retrieval (cf.
    [ADR-0007](./adr/ADR-0007-search-engine-choice.md)),
  - `sql-bi-gateway-service` for **external BI** (Tableau, Superset,
    heterogeneous Arrow Flight SQL JDBC/ODBC clients) — see
    [ADR-0014](./adr/ADR-0014-retire-trino-flight-sql-only.md).
- The previous Trino edge BI deployment under `infra/k8s/trino/` has
  been removed; new services must not depend on Trino for service-to-service
  SQL or for BI access.

## Frontend Coupling

`apps/web/src/routes/*` and `apps/web/src/lib/api/*` mirror the runtime surface area of the platform. Route families such as `datasets`, `pipelines`, `ontology`, `geospatial`, `marketplace`, `code-repos`, `ai`, `ml`, and `nexus` map cleanly onto the backend service topology.

## Why The Gateway Matters

The gateway is the main control-plane entrypoint:

- it exposes health and API paths
- it centralizes cross-cutting middleware such as auth, CORS, request IDs, rate limiting, and audit hooks
- it routes downstream traffic to specialized services instead of collapsing everything into a single backend

That keeps the browser client simpler while preserving service autonomy behind the edge.
