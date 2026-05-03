# ADR-0022: Transactional outbox on Postgres `pg-policy`, drained by Debezium

- **Status:** Accepted
- **Date:** 2026-05-02
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes / supplements:**
  - The "transactional outbox" mention in
    [docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md)
    that referenced the pattern in prose but did not pin an
    implementation.
  - The headers contract in
    [libs/event-bus-data/src/headers.rs](../../../libs/event-bus-data/src/headers.rs)
    (OpenLineage `ol-*` headers, already defined, not yet emitted by any
    producer).
- **Related ADRs:**
  - [ADR-0011](./ADR-0011-control-vs-data-bus-contract.md) — Control
    plane (NATS JetStream) vs data plane (Kafka) contract; this ADR
    only concerns the data plane.
  - [ADR-0013](./ADR-0013-kafka-kraft-no-spof-policy.md) — Kafka
    deployment posture; the outbox publishes to that Kafka.
  - [ADR-0020](./ADR-0020-cassandra-as-operational-store.md) —
    Authoritative state lives in Cassandra; Postgres is for declarative
    schema, policies and **the outbox**.
  - [ADR-0024](./ADR-0024-postgres-consolidation.md) — `pg-policy`
    is one of the 4 consolidated CNPG clusters and is the outbox host.

## Context

Once the platform pivots to Cassandra for hot operational state and to
Kafka as the active data-plane backbone, every mutation that other
services need to learn about (ontology object changes, applied
actions, identity events, dataset versioning, …) needs to be published
to Kafka **with at-least-once delivery and exactly-once-effective
semantics on the consumer side**.

Today there is no such mechanism wired:

- `libs/event-bus-data` is a `rdkafka` wrapper that compiles and is
  configured but is consumed by zero services in the data plane (audit
  in [docs/architecture/bus-audit.md](../bus-audit.md)).
- The mutation handlers write to Postgres only; if a handler tried to
  publish to Kafka after committing, a process crash between commit and
  publish would silently drop the event.

The classic fix is the **transactional outbox** pattern: the mutation
and the "to be published" record are inserted in the same atomic
transaction, and a separate relay drains the outbox to the broker.

We need to decide:

1. **Where** the outbox table lives (Cassandra vs Postgres).
2. **What** drains it to Kafka (a custom relay vs Debezium Connect).

## Options considered

### Option A — Outbox in Cassandra with LWT-based relay (rejected)

Tempting because mutations land primarily in Cassandra; co-locating
the outbox would give a single round-trip write path.

Why it does not work:

- **Cassandra has no cross-table atomicity.** A `BATCH LOGGED` only
  guarantees atomicity for **single-partition** batches; cross-partition
  batches are forbidden by [ADR-0020](./ADR-0020-cassandra-as-operational-store.md).
  An object write and an outbox write live in different partitions
  (different tables, different keys), so they cannot be in a logged
  batch. The result is **two independent writes**, one of which can
  succeed and the other fail — the exact problem the outbox pattern
  is supposed to eliminate.
- **LWT (`IF NOT EXISTS`) for outbox claim costs ≈4×** a normal write
  in round-trips and breaks the hot-path SLO (P99 write <50 ms) every
  time the relay claims an event. Multiplied by every mutation, this
  is a structural latency tax.
- **A Cassandra-native relay would be a new service we own.** It needs
  leader election, claim semantics, retry, dead-letter handling — all
  the things a CDC tool already provides for free.
- **Tombstones.** Outbox semantics require deletion after publish.
  Repeated `DELETE` on the same partition is the canonical way to fill
  a Cassandra cluster with tombstones and degrade reads. TWCS with
  TTL would help, but only if every event is published before TTL
  expires — back-pressure during a Kafka outage would silently drop
  unpublished events.

### Option B — Outbox in Postgres `pg-policy` drained by Debezium (chosen)

- **Real atomicity.** The handler opens one Postgres transaction in
  `pg-policy` that contains both the policy / metadata mutation
  (if any) and the outbox row. Cassandra writes are issued
  **idempotently** with a deterministic `event_id` (hash of payload +
  version) so that a retry after a partial failure converges. The
  pattern matches the published Microservices Outbox literature and
  is what Shopify, Uber and others run in production.
- **Debezium is the standard.** `debezium-connector-postgres` reads
  the WAL via logical decoding, supports the official **Outbox Event
  Router SMT** (which routes by a `topic` column and can delete the
  row after the offset is committed in Kafka), and is operated as a
  standard Strimzi `KafkaConnect` cluster.
- **Kafka exactly-once is intact.** Debezium produces with the
  idempotent producer enabled and integrates with Kafka transactions
  for the connector offsets, so the only delivery semantic the
  consumer needs to handle is "at least once with idempotency by
  `event_id`" — exactly what the consumers already need to handle.
- **No bespoke relay.** The thing that polls / claims / publishes
  / deletes is Debezium. We own configuration, not code.

### Option C — Outbox in Postgres + custom Rust relay (rejected)

- A Rust relay would re-implement what Debezium does — leader
  election, ordering, replay from offset, schema registry integration,
  back-pressure — with strictly less production hardening. Debezium
  has been in production for almost a decade across thousands of
  deployments; we would be one team trying to match that.
- Operational footprint is similar (a stateful HA service to operate
  either way), but Debezium ships with monitoring, runbooks and a
  Grafana dashboard the community already maintains.

### Option D — "Listen / notify on Postgres only" (rejected)

- `LISTEN` / `NOTIFY` is in-memory, not durable, and not a feed.
  Any subscriber outage drops events permanently.

### Option E — Direct Kafka publish from the handler (rejected — current
implicit state)

- Two separate side effects (Postgres commit + Kafka publish) without
  a coordinator. A crash between them silently drops the event. This
  is the failure mode the outbox pattern exists to eliminate.

## Decision

We adopt **Option B**: the transactional outbox lives in the
**`pg-policy.outbox.events`** table, written by the mutation handler
inside the same Postgres transaction as any policy / metadata write,
and is drained to Kafka by a **Strimzi-managed Debezium Kafka Connect
cluster** running the official **Outbox Event Router SMT**.

Cassandra writes from the same handler are issued **idempotently**
with a deterministic `event_id` derived from the mutation payload and
the optimistic version, so that a retry after a partial failure
converges to the same state.

## Schema

```sql
CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE outbox.events (
    event_id      uuid        PRIMARY KEY,
    aggregate     text        NOT NULL,
    aggregate_id  text        NOT NULL,
    topic         text        NOT NULL,
    headers       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    payload       jsonb       NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX outbox_events_created_at_idx
    ON outbox.events (created_at);
```

- `event_id` is a **deterministic UUIDv5** computed from
  `(aggregate, aggregate_id, version, payload_hash)` so that a
  retried handler converges and Debezium / consumers deduplicate
  trivially.
- `topic` is the destination Kafka topic, inspected by the Outbox
  Event Router SMT to route the record.
- `headers` carries OpenLineage `ol-*` headers
  ([libs/event-bus-data/src/headers.rs](../../../libs/event-bus-data/src/headers.rs))
  and any tracing context (`traceparent`, `tracestate`).
- `payload` is the canonical event body, validated against an Apicurio
  Schema Registry schema before being inserted.

The table is short-lived: rows are deleted by Debezium **after** the
Kafka offset for the produced record has been committed.

## Postgres configuration (`pg-policy`)

The CNPG `Cluster` for `pg-policy` is configured with:

```yaml
postgresql:
  parameters:
    wal_level: logical
    max_wal_senders: "10"
    max_replication_slots: "8"
    max_logical_replication_workers: "4"
```

- A dedicated logical replication slot per Debezium connector
  (`outbox_slot_pg_policy`).
- A dedicated `REPLICATION` role (`debezium`) used only by the
  connector.

These settings are encoded in the consolidated
`infra/k8s/platform/manifests/cnpg/clusters/pg-policy.yaml` (see
[ADR-0024](./ADR-0024-postgres-consolidation.md)).

## Debezium configuration

Strimzi `KafkaConnect` cluster under `infra/k8s/platform/manifests/debezium/`:

- 2 Connect workers (`replicas: 2`) for HA.
- Image: `quay.io/debezium/connect:2.7-final` plus the Apicurio
  registry converters baked in.
- Distributed mode; Connect cluster topics created by Strimzi.
- `KafkaConnector` CR `outbox-pg-policy`:

```yaml
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaConnector
metadata:
  name: outbox-pg-policy
  labels:
    strimzi.io/cluster: debezium-connect
spec:
  class: io.debezium.connector.postgresql.PostgresConnector
  tasksMax: 1
  config:
    plugin.name: pgoutput
    database.hostname: pg-policy-rw.cnpg.svc
    database.port: "5432"
    database.user: debezium
    database.password: ${secrets:debezium/pg-policy:password}
    database.dbname: of_policy
    slot.name: outbox_slot_pg_policy
    publication.name: outbox_publication
    publication.autocreate.mode: filtered
    table.include.list: outbox.events
    tombstones.on.delete: "false"
    transforms: outbox
    transforms.outbox.type: io.debezium.transforms.outbox.EventRouter
    transforms.outbox.table.field.event.id: event_id
    transforms.outbox.table.field.event.key: aggregate_id
    transforms.outbox.table.field.event.payload: payload
    transforms.outbox.route.by.field: topic
    transforms.outbox.route.topic.replacement: ${routedByValue}
    transforms.outbox.table.field.event.timestamp: created_at
    transforms.outbox.table.fields.additional.placement: "headers:header"
    outbox.event.deletion.policy: delete
    key.converter: io.apicurio.registry.utils.converter.AvroConverter
    value.converter: io.apicurio.registry.utils.converter.AvroConverter
    key.converter.apicurio.registry.url: http://apicurio.apicurio.svc:8080/apis/registry/v2
    value.converter.apicurio.registry.url: http://apicurio.apicurio.svc:8080/apis/registry/v2
```

Salient configuration choices:

- `outbox.event.deletion.policy: delete` so the outbox table stays
  bounded.
- `tombstones.on.delete: "false"` so consumer compaction strategies
  are not surprised by tombstones for outbox events (the topic is
  not log-compacted; it is delete-policy with retention).
- Apicurio Avro converters so payloads carry a schema id and consumers
  can evolve schemas without coordination.

The full publication contract for the outbox topics is summarised in
the migration plan, §3.6 of
[docs/architecture/migration-plan-cassandra-foundry-parity.md](../migration-plan-cassandra-foundry-parity.md).

## Producer-side library

A new workspace crate `libs/outbox` exposes the only sanctioned way
for handlers to enqueue events:

```rust
pub struct OutboxEvent {
    pub aggregate: &'static str,
    pub aggregate_id: String,
    pub topic: &'static str,
    pub headers: serde_json::Value,
    pub payload: serde_json::Value,
}

impl OutboxEvent {
    pub fn event_id(&self, version: u64) -> Uuid { /* deterministic UUIDv5 */ }
}

pub async fn enqueue<'c>(
    tx: &mut sqlx::PgTransaction<'c>,
    event: &OutboxEvent,
    version: u64,
) -> Result<()>;
```

Rules of use:

- `enqueue` **must** be called inside the same `sqlx::PgTransaction`
  as any policy / metadata write.
- A handler that mutates Cassandra must compute `event_id` before
  calling Cassandra, write Cassandra idempotently with that id, then
  call `enqueue` and commit Postgres. A retry of the whole handler
  after any partial failure converges to the same state.
- Direct `INSERT INTO outbox.events` from anywhere other than
  `libs/outbox::enqueue` is a CI check failure.

## Consumer-side contract

Consumers are required to:

- Deduplicate by `event_id` (carried as the Kafka record key for
  routed records, also present in headers).
- Be idempotent for at-least-once delivery.
- Honour the OpenLineage `ol-*` headers
  ([libs/event-bus-data/src/headers.rs](../../../libs/event-bus-data/src/headers.rs))
  for lineage propagation.

## Topic conventions

- Naming: `<domain>.<entity>.<event>.v<N>` (e.g.
  `ontology.object.changed.v1`).
- Schemas registered in Apicurio under the same name as the topic.
- Provisioned via Strimzi `KafkaTopic` CRs under
  `infra/k8s/platform/manifests/strimzi/topics/`.

## Operational consequences

- New stateful release `infra/k8s/platform/manifests/debezium/` (Strimzi
  `KafkaConnect` + `KafkaConnector` CRs).
- New `libs/outbox` workspace crate with a tested `enqueue` helper.
- New schema `outbox` in `pg-policy` (see
  [ADR-0024](./ADR-0024-postgres-consolidation.md)).
- New CI checks:
  - Postgres logical decoding settings present in
    `infra/k8s/platform/manifests/cnpg/clusters/pg-policy.yaml`.
  - No direct `INSERT INTO outbox.events` outside `libs/outbox`.
  - Every Kafka topic produced by Debezium has an Apicurio schema.
- New runbook `infra/runbooks/debezium.md` covering: replication
  slot lag, connector restart, schema evolution, dead-letter topic
  handling, and the recovery procedure if the slot is dropped or
  corrupted.

## Failure modes and recoveries

| Failure                                                       | Behaviour                                                                                                                   | Recovery                                                                                       |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Handler crashes after Cassandra write, before Postgres commit | No outbox row, transaction rolled back. Cassandra carries the new state but no event is published.                          | Caller retries with the same request id → handler computes the same `event_id`, writes Cassandra idempotently, commits outbox. |
| Handler crashes after Postgres commit                         | Outbox row present, Cassandra state present (idempotently). Debezium publishes when it next polls.                          | None needed.                                                                                   |
| Debezium Connect pod crashes                                  | Outbox row stays in `outbox.events`. Replication slot retains WAL on the Postgres side.                                     | Strimzi reschedules the pod; connector resumes from the last committed offset.                 |
| Kafka cluster unavailable                                     | Connector fails, replication slot grows. Alarm at slot >100 MB.                                                             | Bring Kafka back; connector resumes; slot drains.                                              |
| Replication slot dropped accidentally                         | Outbox events accumulated since slot loss are unpublished. WAL has been recycled.                                           | Stop producers, run a backfill workflow that re-emits every outbox row (the table is the source of truth until deletion). Restore slot. |
| Schema mismatch in producer                                   | Apicurio rejects the converter output → record goes to the dead-letter topic configured per connector.                      | Inspect DLT, fix schema, replay if needed.                                                     |
| Outbox row never published (TTL expired in TWCS — N/A)        | Not applicable. The outbox is in Postgres; rows are deleted only after Kafka commit.                                        | —                                                                                              |

## Consequences

### Positive

- True atomicity between policy / metadata writes and event
  enqueueing.
- Idempotent convergence between Cassandra and the outbox via
  deterministic `event_id`.
- No bespoke relay code to maintain.
- Production-grade CDC with all the operational tooling (Strimzi,
  Apicurio, dead-letter topics, Grafana dashboards) we already use
  for the rest of the data plane.
- Consumer story is uniform: dedupe by `event_id`, honour
  `ol-*` headers, idempotent processing.

### Negative

- Mutation handlers must hold a Postgres transaction even when their
  primary store is Cassandra. This is the cost of correctness; the
  transaction is a few microseconds of `BEGIN` / `COMMIT` plus one
  `INSERT`, dwarfed by network latency.
- We introduce Debezium as a new HA component. Mitigated by Strimzi's
  CRD-based operation and by the fact that we already operate Strimzi
  for Kafka.
- Logical replication slots can fill `pg_wal` if Debezium is offline
  for long enough. Mitigated by alerting and by capping
  `max_slot_wal_keep_size` on the Postgres side.

### Neutral

- The pattern requires `event_id` determinism and consumer
  idempotency. Both are best-practice anyway and are required by the
  consumer-side contract above.
- Cassandra never participates in the atomicity guarantee; it is the
  idempotent target of an idempotent write whose intent is captured
  by the outbox.

## Follow-ups

- Implement migration plan task **S0.9** (`libs/outbox`, schema,
  Debezium connector, end-to-end test).
- Implement migration plan task **S4.1** (production roll-out of
  Debezium against `pg-policy` with monitoring and chaos tests).
- Add the CI check that forbids direct writes to `outbox.events`.
- Document the deterministic `event_id` algorithm in
  `libs/outbox/README.md` and pin it as part of the public API
  (changing it would break consumer dedupe).
