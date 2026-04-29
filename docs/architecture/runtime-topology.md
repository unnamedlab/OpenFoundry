# Runtime Topology

The runtime shape of OpenFoundry is easiest to understand as a layered service mesh behind a single gateway.

## High-Level Flow

```text
                                     External BI clients
                                  (Tableau / Superset / JDBC / ODBC)
                                              |
                                              v
                                   +----------------------+
                                   |  Trino (edge BI)     |   <-- outside the
                                   |  infra/k8s/trino     |       internal
                                   |  Iceberg / PG / CH   |       fan-out;
                                   |  ANSI-SQL only       |       see ADR-0009
                                   +----------------------+
                                              :
                                              : (read-only catalog access:
                                              :  Polaris/Iceberg, CNPG, Kafka,
                                              :  ClickHouse — never internal RPC)
                                              :
Browser / API Client                          :
        |                                     :
        v                                     :
  apps/web or external client                 :
        |                                     :
        v                                     :
   gateway (HTTP entrypoint) -----------------+
        |
        +--> auth-service
        +--> data-connector
        +--> dataset-service
        +--> pipeline-service
        +--> sql-bi-gateway-service ===========> Flight SQL P2P fan-out
        |        (edge SQL router)              (internal query fabric,
        |                                        ADR-0009)
        |                                          |
        |                                          +--> sql-warehousing-service
        |                                          |    (DataFusion / Iceberg,
        |                                          |     Flight SQL :50123)
        |                                          +--> ClickHouse (time-series)
        |                                          +--> Vespa (search / hybrid)
        |
        +--> streaming-service
        +--> ontology-definition-service
        +--> object-database-service
        +--> ontology-query-service
        +--> ontology-actions-service
        +--> ontology-security-service
        +--> ontology-funnel-service
        +--> ontology-functions-service
        +--> audit-service
        +--> app-builder-service
        +--> code-repo-service
        +--> marketplace-service
        +--> ai-service
        +--> ml-service
        +--> geospatial-service
        +--> report-service
        +--> nexus-service
        +--> other bounded services
```

> Trino is intentionally drawn **outside** the gateway fan-out: it is an
> edge BI gateway for external JDBC/ODBC clients, not a participant in
> service-to-service traffic. Internal services reach data through Flight
> SQL P2P via `sql-bi-gateway-service` / `sql-warehousing-service` — see
> [ADR-0009](./adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md).

## Service Families

| Family | Services |
| --- | --- |
| Entry and experience | `gateway`, `app-builder-service`, `marketplace-service`, `notebook-runtime-service`, `document-reporting-service`, `notification-alerting-service` |
| Data plane | `data-connector`, `dataset-service`, `pipeline-service`, `sql-bi-gateway-service`, `streaming-service`, `report-service`, `geospatial-service`, `fusion-service`, `workflow-automation-service` |
| Governance and semantics | `auth-service`, `audit-service`, `ontology-definition-service`, `object-database-service`, `ontology-query-service`, `ontology-actions-service`, `ontology-security-service`, `ontology-funnel-service`, `ontology-functions-service`, `nexus-service` |
| Developer platform | `code-repo-service` |
| AI and ML | `ai-service`, `ml-service` |

## Shared Runtime Dependencies

The repo and workflows indicate a consistent set of platform dependencies:

- Postgres for service-owned relational state
- Redis for gateway-oriented caching and coordination
- NATS for async messaging (control plane — see "Control vs Data" below)
- Apache Kafka for high-throughput streaming (data plane — see "Control vs Data" below)
- object storage for datasets, archives, reports, and repository payloads
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
emit; many services touch both.

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
  this — and most third-party data tooling (Flink, Trino, Spark, Iceberg
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
  - Trino for **external BI only** (Tableau, Superset, heterogeneous
    JDBC/ODBC clients).
- Trino under `infra/k8s/trino/` is retained as the **edge BI** surface.
  It is not used as an internal query hub; new services must not depend on
  Trino for service-to-service SQL.

## Frontend Coupling

`apps/web/src/routes/*` and `apps/web/src/lib/api/*` mirror the runtime surface area of the platform. Route families such as `datasets`, `pipelines`, `ontology`, `geospatial`, `marketplace`, `code-repos`, `ai`, `ml`, and `nexus` map cleanly onto the backend service topology.

## Why The Gateway Matters

The gateway is the main control-plane entrypoint:

- it exposes health and API paths
- it centralizes cross-cutting middleware such as auth, CORS, request IDs, rate limiting, and audit hooks
- it routes downstream traffic to specialized services instead of collapsing everything into a single backend

That keeps the browser client simpler while preserving service autonomy behind the edge.
