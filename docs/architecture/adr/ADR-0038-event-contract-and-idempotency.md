# ADR-0038: Event contract and idempotency for Foundry-pattern orchestration

- **Status:** Accepted
- **Date:** 2026-05-04
- **Deciders:** OpenFoundry platform architecture group
- **Related ADRs:**
  - [ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md) —
    defines the transactional outbox on Postgres and Debezium as the
    publication path to Kafka.
  - [ADR-0037](./ADR-0037-foundry-pattern-orchestration.md) — makes every
    cross-service workflow event-driven and requires idempotent consumers.
    This ADR is the concrete event contract that operationalises rule 3 of
    ADR-0037.

## Context

After [ADR-0037](./ADR-0037-foundry-pattern-orchestration.md), OpenFoundry's
workflow substrate is no longer a central orchestrator but a network of
service-owned state machines, outbox writes and Kafka consumers. That choice
only works if every producer emits events in a uniform shape and every consumer
can safely tolerate at-least-once delivery, connector retries and replay from
Kafka offsets.

[ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md) already
standardised the publication path (`Postgres outbox` → `Debezium` → `Kafka`)
and introduced deterministic event identifiers at the outbox level. What is
still missing is a platform-level contract that all event-driven workflows must
follow: envelope fields, schema validation, retry boundaries, dead-letter
handling and the mandatory consumer-side idempotency store.

Without this ADR, teams would re-invent event shapes and duplicate handling per
service, which would make saga choreography fragile and operational replay
unsafe.

## Decision

OpenFoundry adopts a single event contract for every Kafka event emitted as
part of the Foundry-pattern orchestration substrate introduced by
[ADR-0037](./ADR-0037-foundry-pattern-orchestration.md).

### 1. Schema contract

Every event **MUST** be validated against an Avro or JSON Schema registered in
Apicurio Registry before it is inserted into the outbox or published by any
non-outbox producer.

### 2. Common event envelope

Every event **MUST** expose the following top-level fields:

- `event_id`
- `event_type`
- `aggregate_id`
- `aggregate_type`
- `occurred_at`
- `correlation_id`
- `causation_id`
- `payload`

Canonical example:

```json
{
  "event_id": "uuid-v5(aggregate_type, aggregate_id, version, event_type)",
  "event_type": "dataset.version.published",
  "aggregate_id": "dataset_123",
  "aggregate_type": "dataset",
  "occurred_at": "2026-05-04T07:53:31Z",
  "correlation_id": "req_abc",
  "causation_id": "cmd_def",
  "payload": {}
}
```

### 3. Deterministic `event_id`

`event_id` **MUST** be a UUID v5 computed from:

`(aggregate_type, aggregate_id, version, event_type)`

This makes duplicates produced by outbox retries, Debezium retries or consumer
replay detectable without payload comparison. It also aligns the consumer
contract with the deterministic identity requirement already implied by
[ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md) and mandated
by [ADR-0037](./ADR-0037-foundry-pattern-orchestration.md).

### 4. Consumer idempotency

Every consumer **MUST** be idempotent.

The default implementation is a Postgres table owned by the consuming service:

```sql
CREATE TABLE processed_events (
    event_id uuid PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);
```

The consumer records `event_id` before, or atomically with, any externally
visible side effect. If the insert fails because the key already exists, the
event is treated as already processed and the handler exits successfully.

For high-volume consumers that already use Cassandra as their hot operational
store, a `processed_events` table with TTL is acceptable, provided the TTL is
strictly longer than the topic retention and replay window.

### 5. Retry and dead-letter handling

- In-application retries are **finite**, with exponential backoff.
- The standard retry budget is **5 attempts maximum**.
- After the retry budget is exhausted, the event is published to a dead-letter
  topic named `__dlq.<original-topic>`.
- Dead-letter events require on-call review; they are not silently dropped.

This policy applies to consumers in the choreography described by
[ADR-0037](./ADR-0037-foundry-pattern-orchestration.md), including pipeline
outcomes, approval flows and automation steps.

## Consequences

### Positive

- Consumers become safe under at-least-once delivery, connector retries and
  replay.
- Cross-service workflows share one envelope and one deduplication rule instead
  of service-specific conventions.
- Audit, observability and runbook authoring become simpler because every event
  exposes the same identity and trace fields.

### Negative

- Every consumer must provision and maintain a `processed_events` store.
- Storage and retention costs increase, especially for high-volume consumers
  that keep long replay windows.
- Event producers must version their schemas deliberately because the registry
  contract is now mandatory rather than advisory.
