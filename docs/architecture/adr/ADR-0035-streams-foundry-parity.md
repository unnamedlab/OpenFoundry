# ADR-0035 — Streams Foundry parity (D1.1.2 closure)

* Status: Accepted
* Date: 2026-05-04
* Deciders: streaming + ingest WG
* Supersedes: —

## Context

D1.1.2 covers the streaming subsystem and aims at functional parity
with Palantir Foundry's "Data connectivity & integration → Streams"
documentation set. The work is split across six blocks (P1–P6) that
reused a single Postgres-backed control plane (`event-streaming-service`)
with optional Kafka + Flink runtime backends.

This ADR captures the resulting architecture so future contributors
have a single source of truth for the wiring, the consistency matrix
and the verification surface.

## Decision

We ship a **single control plane** (`event-streaming-service`) plus
**three companion services** that own well-defined slices:

* `monitoring-rules-service` — rule evaluator + scheduler (P4).
* `notification-alerting-service` — fan-out to email / WS / NATS.
* `connector-management-service` — typed wizard + virtual-table
  discovery for streaming sync sources (P5).

Schema parity rides on `libs/core-models::dataset::schema::Schema`
through the new `models::schema_bridge` (P6) so a stream's
`schema_avro` is interoperable with a dataset's `schema`.

## Architecture

```mermaid
flowchart LR
    src[External source<br/>Kafka / Kinesis / SQS / Pub-Sub /<br/>Aveva PI / Magritte agent]:::source
    proxy[(stream proxy<br/><code>POST /streams-push/{view}</code>)]:::svc
    hot[(Hot buffer<br/>Kafka topic)]:::store
    flink[Flink topology<br/>+ checkpoints]:::svc
    cold[(Cold archive<br/>Iceberg / Parquet)]:::store
    dataset[Dataset view<br/>data-asset-catalog]:::svc
    pipeline[Pipeline Builder<br/>streaming pipeline]:::svc

    src -- pull / push --> proxy
    proxy -- AT_LEAST_ONCE --> hot
    hot -- consumer-group --> flink
    flink -- exactly-once<br/>commit --> cold
    cold --> dataset
    pipeline -- attach profiles --> flink

    %% Cross-cutting planes
    profiles[Streaming profiles<br/>Control Panel]:::cross
    monitors[Stream monitors<br/>Data Health]:::cross
    usage[Compute usage<br/>Usage tab]:::cross
    markings[Markings<br/>RBAC clearances]:::cross

    profiles -. "import to project" .-> flink
    monitors -. evaluate metrics .-> flink
    monitors -. evaluate metrics .-> hot
    usage    -. record per checkpoint .-> flink
    markings -. filter list / push .-> proxy

    classDef source fill:#fff8e1,stroke:#d97706
    classDef svc    fill:#eef5ff,stroke:#2a6df0
    classDef store  fill:#fde7e9,stroke:#b00020
    classDef cross  fill:#f0fdf4,stroke:#1f5631
```

## Consistency matrix

The Foundry docs distinguish between three planes; the streaming
service enforces the rules below.

| Plane                    | Allowed values                  | Enforced by                                                                                       |
|--------------------------|---------------------------------|----------------------------------------------------------------------------------------------------|
| **Ingest** (sources)     | `AT_LEAST_ONCE` only            | `streaming_streams.ingest_consistency` CHECK + handler 422 `STREAM_INGEST_EXACTLY_ONCE_NOT_SUPPORTED` |
| **Pipeline** (Flink)     | `AT_LEAST_ONCE` / `EXACTLY_ONCE`| Topology config + `effective_exactly_once(topology, streams)` resolver                            |
| **Export / extract**     | `AT_LEAST_ONCE` only            | Same column as ingest (no separate flag — the docs are explicit)                                  |

`EXACTLY_ONCE` requires the `flink-runtime` feature; without it, a
deploy attempt returns `401 STREAM_EXACTLY_ONCE_REQUIRES_FLINK_RUNTIME`
so callers cannot silently degrade to `AT_LEAST_ONCE`.

## Block-by-block summary

| Block | Surface                                               | Migration(s)                                       |
|-------|-------------------------------------------------------|----------------------------------------------------|
| P1    | StreamType / Consistency / partitions / config       | `20260504000001_stream_config.sql`                 |
| P2    | Reset stream + viewRid rotation                       | `20260504000002_stream_views.sql`                  |
| P3    | Streaming profiles + project refs                     | `20260504000003_streaming_profiles.sql`            |
| P4    | Monitoring views / rules / evaluations                | `monitoring-rules-service/migrations/20260504000004_streaming_monitors.sql` |
| P5    | Connector trait + Kinesis/SQS/PubSub/AvevaPI/External | (no migration — config carried in source_binding)  |
| P6    | Compute usage + stateful keys + schema parity        | `20260504000005_streaming_compute.sql`, `20260504000006_streaming_stateful.sql` |

## Verification

* `cargo test -p event-streaming-service`: 69 lib tests + 30+
  integration tests (kinesis/pubsub/preview/profiles/monitors/reset
  view/streaming-config/stateful/schema-bridge).
* `cargo test -p monitoring-rules-service`: evaluator + dedup
  contract.
* `pnpm check`: zero new errors / warnings from the streaming
  surfaces.
* `pnpm test:e2e --grep stream`: full suite covers the Foundry
  docs' user-visible flows (Settings tab, Reset, Profiles import,
  Add monitor, LiveDataView toggles, Job Details, Usage, Dead
  letters).

## Consequences

* The streaming surface is now self-describing: every Foundry doc
  section maps to a file in this repo (see README parity matrix).
* Operators can hot-tune Kafka producer settings + reset streams
  without redeploying the binary.
* Cost projections live next to the stream (Usage tab) so the
  platform team can swap `compute_seconds_to_cost_factor` per
  enrollment without code changes.
* Future work: Pipeline Builder UX integration (P7+), graceful
  Flink failover (savepoints), and cross-region replication of
  cold-tier archives.
