# `ontology-indexer`

> Stream: S4 · Tarea S4.3
> Backends: Vespa (prod) / OpenSearch (dev) via [`libs/search-abstraction`](../../libs/search-abstraction)
> Kafka: [`libs/event-bus-data`](../../libs/event-bus-data) consumer

Stateless Kafka consumer that materialises ontology mutations into
the search backend. Keeps the search index eventually consistent with
Cassandra ontology storage.

## Topics

| Topic | Purpose |
|-------|---------|
| `ontology.object.changed.v1` | Live object upserts / deletes. |
| `ontology.action.applied.v1` | Action effects that mutate objects. |
| `ontology.reindex.v1` | Backfill / re-index runs driven by the `workers-go/reindex` workflow. Separate topic so backfill does not starve the live consumer group. |

## Idempotency

Per [ADR-0028](../../docs/architecture/adr/ADR-0028-search-backend-abstraction.md)
the backend is the authority on staleness:

* **Vespa** uses `condition=<type>.version<N` — stale `PUT` returns
  HTTP 412 and is silently dropped.
* **OpenSearch** uses `version_type=external` with `if_seq_no` /
  `if_primary_term` — stale `index` returns HTTP 409.

The consumer is therefore allowed to be at-least-once. The
`(tenant, id, version)` tuple is the de-duplication key.

## Backend selection

`SEARCH_BACKEND` environment variable:

* `vespa` (default) — production.
* `opensearch` — dev / CI.

Anything else fails loudly (`panic!`) at startup.

## Replicas & SLO

| Setting | Value |
|---------|-------|
| Replicas | 3 (stateless, one per zone) |
| Kafka consumer group | `ontology-indexer` |
| Lag SLO | P99 < 5 s (`ontology_indexer_lag_seconds`) |
| Alert | [`prometheus-rules-indexer.yaml`](../../infra/k8s/platform/manifests/observability/prometheus-rules-indexer.yaml) |

## Runtime

This crate ships both:

1. **Pure logic** (always compiled) — payload decoder
   ([`decode_object_changed`](src/lib.rs)), `BackendKind` selector,
   topic + metric name constants.
2. **Runtime wiring** behind feature `runtime` — Kafka consumer
   loop that calls `SearchBackend::index` / `SearchBackend::delete`
   and commits Kafka offsets only after the backend write succeeds.
   The binary requires `runtime`.
