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

```bash
# Trigger a per-type reindex
echo '{"tenant_id":"tenant-a","type_id":"users","page_size":500}' \
  | kafkacat -P -t ontology.reindex.requested.v1

# Watch batches land
kafkacat -C -t ontology.reindex.v1 -f '%k → %s\n'

# Watch the terminal event
kafkacat -C -t ontology.reindex.completed.v1
```

## Layout

* `migrations/0001_reindex_jobs.sql` — `reindex_coordinator.reindex_jobs` + `processed_events`.
* `src/lib.rs` — pure-logic exports (state machine, decoders, event-id).
* `src/event.rs` — wire format + UUID v5 helpers.
* `src/state.rs` — `JobStatus` enum + Postgres repo (gated by `runtime`).
* `src/scan.rs` — pure record encoder + Cassandra paged scanner (gated by `runtime`).
* `src/topics.rs` — pinned Kafka topic constants.
* `src/runtime.rs` — Coordinator, consumer loop, Prometheus metrics, HTTP handler.
* `src/main.rs` — binary entry point (gated by `runtime`).
* `tests/state_machine.rs` / `tests/event_id.rs` — pure-logic regression tests.
