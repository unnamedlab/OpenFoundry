# Decision: keep `event-streaming-service` (S5.5)

- **Date:** 2025-S5 (week 12)
- **Verdict:** **KEEP** — with scope reduction.
- **Owner:** data-platform squad.
- **Plan reference:** S5.5 of [migration-plan-cassandra-foundry-parity.md](../migration-plan-cassandra-foundry-parity.md).

## Question

> "Validar si `event-streaming-service` legacy sigue cumpliendo
> función única o ha sido reemplazado por audit-sink + lineage-sink +
> ontology-indexer."

## Function catalogue

`event-streaming-service` exposes three distinct, in-use surfaces:

| # | Surface | Port | Replaces? |
|---|---------|------|-----------|
| 1 | gRPC `Publish` / `Subscribe` over `EventStream` | `:50221` | No equivalent in any sink. |
| 2 | REST `POST /streams/{id}/events`, `GET /streams/{id}/events` | `:50121` | No equivalent — sinks are pull-only. |
| 3 | Declarative routing table (`data.>` → Kafka, `ctrl.*` → fallback) | n/a | No equivalent — sinks subscribe to fixed topics. |

The three new sinks delivered in S5.1 / S5.3 are pure **downstream
consumers**:

- `audit-sink` consumes `audit.events.v1` → Iceberg `of_audit.events`.
- `lineage-service` (sink path) consumes `lineage.events.v1` →
  Iceberg `of_lineage.runs/events/datasets_io`.
- `ontology-indexer` consumes `ontology.events.v1` → Vespa.
- `ai-sink` (S5.3.b) consumes `ai.events.v1` → Iceberg `of_ai.*`.

None of these accept an inbound publish from a producer; none route
between subjects; none expose a streaming `Subscribe` RPC. The two
surfaces are complementary — a sink replaces the bespoke "write to
storage" path that used to live inside `event-streaming-service`,
**not** the Publish/Subscribe ingress.

## Coverage gap analysis

| Function | Covered by sinks? | Gap |
|----------|-------------------|-----|
| Producer-facing gRPC `Publish` (with backpressure + retry semantics) | No | `libs/event-bus-data` is the right replacement long-term but adoption is at ~40% across services. |
| Producer-facing REST `POST /streams/{id}/events` | No | Used by external integrations and the K6 smoke harness. |
| Subject routing (`data.>` → Kafka cluster, `ctrl.*` → control-plane NATS) | No | Sinks subscribe to fixed topics; nothing fans out. |
| Streaming `Subscribe` RPC for non-storage consumers (UI tail, debug CLI) | No | Sinks write to Iceberg/Vespa, not to a stream. |

## Decision

**KEEP** `event-streaming-service`, with the following scope
reduction (executed in a follow-up PR — out of S5.5 scope):

1. Remove the now-dead "write to ClickHouse / write to Postgres"
   storage backends from the routing table — those paths are
   superseded by the new sinks.
2. Mark the `legacy-storage-fanout` feature flag deprecated.
3. Cap memory + replicas — the service is now a thin
   ingress + dispatch layer, not a storage gateway.

## Conditions to revisit (decommission criteria)

`event-streaming-service` becomes a candidate for decommission when
**all four** of the following are true:

1. 100% of internal producers use `libs/event-bus-data` directly
   (current: ~40%).
2. The transactional outbox covers every control-plane `ctrl.*`
   subject (`libs/outbox` currently covers 6 of 11).
3. External integrations and K6 ingest paths are migrated to the
   `data-ingest-edge-service` (S6 candidate).
4. A streaming-debug CLI exists that doesn't depend on the
   `Subscribe` RPC.

## Sign-off

- data-platform: ✅
- platform-eng: ✅
- security: ✅ (no change to ACLs; ingress posture unchanged)
