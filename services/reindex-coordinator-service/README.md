# `reindex-coordinator-service`

FASE 4 / Tarea 4.2 — Rust replacement for the Go
`workers-go/reindex` Temporal worker (per ADR-0021 and the
migration plan in
[`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../../docs/architecture/migration-plan-foundry-pattern-orchestration.md)).
The full Go-→-Rust mapping lives in
[`docs/architecture/refactor/reindex-worker-inventory.md`](../../docs/architecture/refactor/reindex-worker-inventory.md).

## Topology

```text
  ontology.reindex.requested.v1   (input,  ReindexRequestedV1)
                 │
                 ▼
    ┌──────────────────────────┐
    │ reindex-coordinator-svc  │ ── Cassandra (objects_by_type / objects_by_id)
    │  pg-runtime-config       │      page-by-page via `cassandra-kernel`
    │   .reindex_jobs (cursor) │
    └──────────────────────────┘
                 │
                 ├─ batches  ──▶  ontology.reindex.v1
                 │                (consumed by services/ontology-indexer)
                 │
                 └─ terminal ──▶  ontology.reindex.completed.v1
                                  (ReindexCompletedV1)
```

## Restart safety

The coordinator owns the resume cursor in
`pg-runtime-config.reindex_jobs.resume_token`. The Kafka offset on
`ontology.reindex.requested.v1` is committed only when the job
reaches a terminal status. On a crash mid-scan:

1. The service restarts and lists every job whose status is
   `queued` or `running`, kicking each of them back into the page
   loop with the persisted `resume_token`.
2. The original `requested.v1` message is redelivered, the job-id
   `INSERT … ON CONFLICT DO NOTHING` no-ops, and the duplicate
   `run_job` call sees the row is already terminal and returns
   without resurrecting it.
3. Any in-flight batch whose `resume_token` was published to
   Kafka but whose Postgres update did not commit is identified by
   the deterministic `event_id = uuid_v5(tenant||type||token)`
   stored in `reindex_coordinator.processed_events`; the second
   attempt sees `Outcome::AlreadyProcessed` and skips re-publishing
   while still advancing the row.

## Throttling

The legacy Go worker leaned on Temporal's exponential backoff
(`1s → 60s`, unlimited attempts) as an implicit rate-limiter; the
Rust port makes it explicit so a backfill cannot saturate
`objects_by_id`:

| Env var | Default | Purpose |
|---|---|---|
| `OF_REINDEX_PAGE_INTERVAL_MS` | `0` | Sleep between successive pages of the same job. |
| `OF_REINDEX_MAX_BATCHES_PER_SECOND` | `0` (unbounded) | Per-record sleep derived as `1000 / N` ms. |

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | _(required)_ | `pg-runtime-config` connection string with the `reindex_coordinator` schema. |
| `DATABASE_MAX_CONNECTIONS` | `10` | Pool size. |
| `KAFKA_BOOTSTRAP_SERVERS` | _(required)_ | Same shape as `ontology-indexer`. |
| `KAFKA_SASL_USERNAME` / `KAFKA_SASL_PASSWORD` / `KAFKA_SASL_MECHANISM` / `KAFKA_SECURITY_PROTOCOL` | _(unset)_ | Optional SASL/SCRAM. |
| `CASSANDRA_CONTACT_POINTS` | _(required)_ | Comma-separated seed list. |
| `CASSANDRA_KEYSPACE` | `ontology_objects` | Source keyspace. |
| `CASSANDRA_LOCAL_DC` | `dc1` | Local DC for `LOCAL_QUORUM`. |
| `CASSANDRA_USERNAME` / `CASSANDRA_PASSWORD` | _(unset)_ | Optional auth. |
| `CASSANDRA_CONNECT_TIMEOUT_SECS` / `CASSANDRA_REQUEST_TIMEOUT_SECS` | `10` / `30` | Driver timeouts. |
| `OF_OPENLINEAGE_NAMESPACE` | `openfoundry` | OpenLineage namespace stamped on every produced record. |
| `METRICS_ADDR` | `0.0.0.0:9090` | `/metrics` + `/health` listener. |
| `HEALTH_ADDR` | _(unset)_ | If set, a second listener for `/health` on a separate port. |

## Verification

The official, supported producer of `ontology.reindex.requested.v1`
is the `reindex-request` admin CLI shipped inside this same
container image (see [_Operations_](#operations) below). Hand-rolled
`kafkacat` invocations skip the Apicurio schema interceptor and the
OpenLineage headers, so they should be reserved for break-glass
debugging only:

```bash
# Trigger a per-type reindex (preferred — uses the supported CLI)
kubectl run -n openfoundry --rm -it --restart=Never reindex-request \
  --image=ghcr.io/openfoundry/reindex-coordinator-service:0.1.0 \
  --command -- /usr/local/bin/reindex-request \
    --tenant tenant-a --type-id users --page-size 500

# Watch batches land
kafkacat -C -t ontology.reindex.v1 -f '%k → %s\n'

# Watch the terminal event
kafkacat -C -t ontology.reindex.completed.v1
```

## Operations

The reindex pipeline is the **only** supported way to backfill the
search index for an existing tenant; it is intentionally distinct
from `ontology-funnel-service`, which owns the **batch ingestion**
plane (`/api/v1/ontology/funnel/*`) and never re-processes data
already on disk in Cassandra. A reindex is the operator's tool when
the index is suspected stale (schema upgrade, OpenSearch/Vespa
restore, search backend cut-over).

### Official flow

```
operator
   │
   ▼
reindex-request CLI ──▶ ontology.reindex.requested.v1
                                   │
                                   ▼
                        reindex-coordinator-service
                                   │
                       ┌───────────┴────────────┐
                       ▼                        ▼
              ontology.reindex.v1     reindex_coordinator.reindex_jobs
                       │                  (status, resume_token)
                       ▼
              ontology-indexer
                       │
                       ▼
                ontology.reindex.completed.v1
```

There is **no other supported producer** of
`ontology.reindex.requested.v1`. A future REST/UI surface should
delegate to the same CLI logic (`src/bin/reindex_request.rs`) so
the Apicurio schema and OpenLineage headers stay consistent.

### Dispatching a reindex

```bash
# All flags, the CLI prints --help with the same shape:
kubectl run -n openfoundry --rm -it --restart=Never reindex-request \
  --image=ghcr.io/openfoundry/reindex-coordinator-service:<TAG> \
  --command -- /usr/local/bin/reindex-request \
    --tenant <TENANT_ID> \
    --type-id <TYPE> \
    --page-size 500 \
    --request-id <ID>
# Optional flags:
#   --type-id <TYPE>     restrict to a single ontology type (omit for all-types scan)
#   --page-size 500      Cassandra page size override, 1..=10000 (default 1000)
#   --request-id <ID>    operator-supplied correlation id, echoed verbatim on completed.v1
#   --dry-run            print the JSON payload to stdout without publishing
```

The CLI logs the derived `job_id` (deterministic UUID v5 of
`tenant_id || type_id`); pair it with
`SELECT * FROM reindex_coordinator.reindex_jobs WHERE id = '<UUID>'`
to track progress in real time without waiting for the
`completed.v1` event.

### Cancelling a stuck job

The state machine forbids `running → cancelled` from outside
process state, so cancellation is a manual SQL operation followed
by a redelivery:

```sql
UPDATE reindex_coordinator.reindex_jobs
SET    status = 'cancelled',
       completed_at = now(),
       error = 'cancelled-by-operator'
WHERE  id = '<UUID>';
```

Then republish via the CLI; the new request goes through the
state machine cleanly.

### Alerts (`PrometheusRule reindex-coordinator`)

Defined in
[`infra/helm/infra/observability/files/prometheus-rules-reindex-coordinator.yaml`](../../infra/helm/infra/observability/files/prometheus-rules-reindex-coordinator.yaml):

| Alert | Severity | What it means |
|---|---|---|
| `ReindexCoordinatorRequestLag` | warning | Requests piling up on the input topic for >15m. |
| `ReindexCoordinatorNotConsuming` | critical | No `requests_total` samples for 30m — the Deployment is dead. |
| `ReindexCoordinatorNotProducingBatches` | critical | Jobs in flight but no `batches_total{outcome="published"}` for 15m. |
| `ReindexCoordinatorJobsStuck` | warning | At least one job has been in flight for >2h. |
| `ReindexCoordinatorDLQGrowth` | warning | Anything wrote to `__dlq.ontology.reindex.*` in the last 30m. |
| `ReindexCoordinatorDecodeErrors` | warning | Producer/consumer schema drift — investigate the CLI version. |

### Helm wiring

The Deployment, Service, NetworkPolicy and PDB are rendered by
the `services` map in
[`infra/helm/apps/of-ontology/values.yaml`](../../infra/helm/apps/of-ontology/values.yaml#L165)
(same shared template as `ontology-indexer`). The
`reindex_coordinator.reindex_jobs` + `processed_events` schema is
applied by a `pre-install,pre-upgrade` Job defined in
[`templates/reindex-coordinator-migrations.yaml`](../../infra/helm/apps/of-ontology/templates/reindex-coordinator-migrations.yaml),
which executes
[`migrations/0001_reindex_jobs.sql`](migrations/0001_reindex_jobs.sql)
against `pg-runtime-config` via `psql --single-transaction`.

## Layout

* `migrations/0001_reindex_jobs.sql` — `reindex_coordinator.reindex_jobs` + `processed_events`.
* `src/lib.rs` — pure-logic exports (state machine, decoders, event-id).
* `src/event.rs` — wire format + UUID v5 helpers.
* `src/state.rs` — `JobStatus` enum + Postgres repo (gated by `runtime`).
* `src/scan.rs` — pure record encoder + Cassandra paged scanner (gated by `runtime`).
* `src/topics.rs` — pinned Kafka topic constants.
* `src/runtime.rs` — Coordinator, consumer loop, Prometheus metrics, HTTP handler.
* `src/main.rs` — binary entry point (gated by `runtime`).
* `src/bin/reindex_request.rs` — admin CLI / official producer (gated by `runtime`).
* `tests/state_machine.rs` / `tests/event_id.rs` — pure-logic regression tests.
