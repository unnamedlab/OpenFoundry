# ADR-0034 — Datasets Foundry parity (5/5)

| Field | Value |
| --- | --- |
| Status | Accepted |
| Date | 2026-05-03 |
| Stream | D1.1.1 — Datasets surface |
| Related | [ADR-0033](ADR-0033-branching-foundry-parity.md), [ADR-0007](ADR-0007-search-engine-choice.md), [ADR-0023](ADR-0023-iceberg-cross-region-dr.md) |

## Context

The Datasets surface is the single most-trafficked entity in OpenFoundry.
Every section of the Foundry **Datasets** docs (Schema, Data, Files,
History, Retention, Permissions, Quality, Lineage, Compare, Open in…)
maps to a tab in `apps/web/src/routes/datasets/[id]/+page.svelte`. The
prior six-prompt arc (P1 → P5) closed the **functional** gaps. P6
closes the **operational** gap by adding what an SRE expects of a
Foundry-grade surface:

1. **Per-dataset observability.** A QualityDashboard that surfaces
   freshness, last build, schema drift, row/col counts, txn-failure
   rate, and policy hooks — backed by a `dataset-quality-service`
   that finally has a real Axum binary, not `fn main(){}`.
2. **API conformance with the Application reference.** Cursor
   pagination on every list endpoint, ETag/304 on resource GETs,
   207 Multi-Status batch reads, and a unified
   `{ code, message, details, request_id }` error envelope.
3. **End-to-end coverage.** Docker-gated integration tests for the
   conformance contracts and the health surface, plus a Playwright
   journey that walks every dataset tab in one sitting.

## Decision

Land four parallel changes that together raise the Datasets surface
to **5/5 parity** with Foundry.

### 1. dataset-quality-service binary

Wire the existing column-profile/lint handlers into a real Axum
router and add the Foundry **Data Health** surface on top:

```mermaid
flowchart LR
  subgraph "dataset-quality-service"
    BIN[main.rs<br/>Axum + Postgres] --> ROUTER[build_router]
    ROUTER --> Q[/api/v1/datasets/:id/quality*<br/>quality + lint]
    ROUTER --> H[/v1/datasets/:rid/health<br/>compute_health]
    ROUTER --> M[/metrics<br/>Prometheus]
  end
  H --> DB[(dataset_health<br/>+ dataset_health_policies)]
  H --> DVS[(dataset_transactions<br/>dataset_files<br/>dataset_view_schemas)]
  M --> P[Prometheus<br/>dataset_freshness_seconds<br/>dataset_row_count<br/>dataset_txn_failures_total]
```

`compute_health(rid)` derives the six signals from data already owned
by `dataset-versioning-service`:

| Signal | Source |
| --- | --- |
| `row_count` / `col_count` | `dataset_files` (size proxy) + `dataset_view_schemas.fields` |
| `freshness_seconds` | `now() - max(committed_at)` |
| `last_build_status` | derived from latest commit age (success / stale / unknown) |
| `txn_failure_rate_24h` | aborted vs. committed in the last 24 h |
| `schema_drift_flag` | latest two `dataset_view_schemas.content_hash` differ |
| `extras` | sparkline points + per-tx_type failure breakdown |

Persisted in `dataset_health` (rid PK) so the UI hits a single SELECT.

### 2. API conformance helpers in DVS

`services/dataset-versioning-service/src/handlers/conformance.rs`
introduces a small, dep-free toolkit shared by every handler:

* `PageQuery { cursor, limit }` + `Page<T> { data, next_cursor, has_more }`
  — base64url-encoded offset cursor, `limit` clamped to `[1, 500]`.
* `etag_for(value)` — sha256-hex of the canonical JSON, RFC 7232
  quoted; `if_none_match_matches` handles `*` and comma-separated
  candidate lists; `json_with_etag` returns 304 on match.
* `BatchItemResult<T> { status, id, data?, error? }` + `batch_response`
  for 207 Multi-Status responses.
* `ErrorEnvelope { code, message, details?, request_id? }` — matches
  the Application reference body shape.

Wired into `list_versions`, `list_branches`, `list_transactions` (page
envelope) and `get_branch`, `get_transaction` (ETag/304). New
endpoint `POST /v1/datasets/{rid}/transactions:batchGet` returns 207.

### 3. QualityDashboard component

`apps/web/src/lib/components/dataset/QualityDashboard.svelte` mounts
under the **Health** tab and renders the six Foundry-parity cards
self-fetched from `getDatasetHealth(rid)`. The card surface stays
visible with em-dash placeholders when the quality service is offline
so the UI never blanks out.

### 4. Tests + docs

* `services/dataset-versioning-service/tests/api_conformance_*.rs` —
  pagination, etag, 207 batch (Docker-gated).
* `services/dataset-quality-service/tests/health_freshness_sla.rs`
  + `schema_drift_detected.rs` — end-to-end against testcontainers.
* `apps/web/tests/e2e/dataset-quality-dashboard.spec.ts` +
  `dataset-full-journey-5x5.spec.ts` — Playwright walks every tab.
* This ADR + a parity matrix in the DVS README + a checklist tick.

## Consequences

* The Datasets surface is the first OpenFoundry domain that can
  honestly be marked **5/5 Foundry parity**. Every subsequent domain
  (Ontology, ML, Apps) inherits the conformance helpers as a
  template, so they'll arrive at parity faster and with the same
  shape on the wire.
* `dataset-quality-service` becomes a real service. It gains a Helm
  rollout, a Prometheus scrape target, and a place in the
  `of-data-engine` chart ([ADR-0031](ADR-0031-helm-chart-split-five-releases.md)).
* Cursor pagination is introduced as in-memory slicing first; SQL
  `LIMIT/OFFSET` push-down is a follow-up (see TODO in
  `handlers/foundry.rs::slice_into_page`). Acceptable because every
  list endpoint we touch in this ADR already returns ≤ a few hundred
  rows.
* The compute path on health is invoked synchronously by the GET
  handler today; the Kafka subscriber for
  `dataset.transaction.committed.v1` is left as a TODO so we can
  ship the surface and observe the cold-cache cost before adding a
  background pipeline.
