# `audit-sink`

> Stream: S4 · Tarea S4.5 (substrate) · S5.1 (Iceberg writer)
> Source: Kafka topic `audit.events.v1`
> Target: Iceberg `lakekeeper.of_audit.events` (partition `day(at)`)

Stateless Kafka consumer → Iceberg batch writer. Owns the long-term
durability of audit events: the topic itself keeps 10 years (see
[`infra/k8s/strimzi/topics-domain-v1.yaml`](../../infra/k8s/strimzi/topics-domain-v1.yaml)),
but Iceberg becomes the system of record once the sink is live.

## Batch policy

Flush a snapshot when **either**:

* 100 000 records have accumulated, **or**
* 60 seconds have elapsed since the previous flush.

(Exactly the plan's S5.1.b numbers.)

## Snapshot retention

Per S5.1.c: `expire_snapshots` is **disabled forever**. Audit
history is append-only; no compaction touches old snapshots.

## Replicas & SLO

| Setting | Value |
|---------|-------|
| Replicas | 2 (HA, partition rebalance handles failover) |
| Consumer group | `audit-sink` |
| Sink lag SLO | P99 < 90s |

## Runtime

* **Pure logic** (always compiled): envelope decoder, batch policy,
  Iceberg target constants.
* **Runtime wiring** behind feature `runtime`: Kafka consumer loop,
  Arrow batch builder and Iceberg `append_record_batches`. Offsets are
  committed only after the Iceberg append succeeds.
