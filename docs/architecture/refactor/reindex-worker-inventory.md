# `reindex` worker functional inventory

> **Migration context.** FASE 4 / Tarea 4.1 of
> [`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../migration-plan-foundry-pattern-orchestration.md).
> Per ADR-0021 the reindex worker is the documented exception to the
> "no direct DB access from workers" rule (it talks Cassandra and
> Kafka directly), so the workflow body is essentially a deterministic
> page-loop wrapped in Temporal for retry/replay. FASE 4 unwraps that
> wrapping: the same loop becomes a plain Rust Kafka consumer
> (`services/reindex-coordinator-service`, Tarea 4.2) with the page
> cursor persisted in `pg-runtime-config` instead of Temporal workflow
> state. **No code is being moved here**; this document is the
> read-only baseline used to plan the refactor.

---

## 1. Worker process topology

| Field | Value | Source |
|---|---|---|
| Worker binary | `workers-go/reindex/main.go` | [main.go](../../../workers-go/reindex/main.go) |
| Temporal task queue | `openfoundry.reindex` (constant `contract.TaskQueue`) | [contract.go:12](../../../workers-go/reindex/internal/contract/contract.go) |
| Registered workflows | **`OntologyReindex`** (only one) | [main.go:38](../../../workers-go/reindex/main.go) |
| Registered activities | `(*Activities).ScanCassandraObjects`, `(*Activities).PublishReindexBatch` | [activities.go:57,94](../../../workers-go/reindex/activities/activities.go) |
| Direct datastores | Cassandra `ontology_objects.*` (gocql), Kafka `ontology.reindex.v1` (franz-go) | [activities.go:127-160](../../../workers-go/reindex/activities/activities.go), [contract.go:32](../../../workers-go/reindex/internal/contract/contract.go) |
| Cross-language contract | Mirrors `libs/temporal-client::task_queues::REINDEX` and `services/ontology-indexer::topics::ONTOLOGY_REINDEX_V1` | [contract.go:12,32](../../../workers-go/reindex/internal/contract/contract.go), [temporal-client lib.rs:1631](../../../libs/temporal-client/src/lib.rs), [ontology-indexer lib.rs:44](../../../services/ontology-indexer/src/lib.rs) |
| Health / metrics | `:9090/healthz`, `:9090/metrics` (placeholder, mirrors `workflow-automation`) | [main.go:54-66](../../../workers-go/reindex/main.go) |

Configuration env (worker → datastores; **no Rust services are
called**, unlike `pipeline` / `workflow-automation`):

| Env var | Default | Purpose |
|---|---|---|
| `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Temporal frontend gRPC |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `CASSANDRA_CONTACT_POINTS` | _(required)_ | Comma-separated seed list |
| `CASSANDRA_KEYSPACE` | `ontology_objects` | Keyspace for `objects_by_type` / `objects_by_id` |
| `CASSANDRA_LOCAL_DC` | _(unset)_ | Enables DC-aware host policy |
| `CASSANDRA_USERNAME` / `CASSANDRA_PASSWORD` | _(unset)_ | Optional `PasswordAuthenticator` |
| `KAFKA_BOOTSTRAP_SERVERS` | _(required)_ | Seed brokers for franz-go |
| `KAFKA_CLIENT_ID` | `workers-go-reindex` | Producer client id |
| `KAFKA_SECURITY_PROTOCOL` | _(unset)_ | TLS dial when contains `SSL` |
| `KAFKA_SASL_USERNAME` / `KAFKA_SASL_PASSWORD` / `KAFKA_SASL_MECHANISM` | _(unset)_ / _(unset)_ / `SCRAM-SHA-512` | Optional SASL/SCRAM |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

Notable: there is **no caller in-tree that starts `OntologyReindex`**
today. `libs/temporal-client::task_queues::REINDEX` is exported but
no Rust service uses it; runs are kicked off externally (admin /
script). This makes Tarea 4.2's switch to a Kafka trigger
(`ontology.reindex.requested.v1`) a strict superset rather than a
breaking change for any current code path.

---

## 2. Workflow inventory

### 2.1 `OntologyReindex` (the only workflow)

**Body** ([reindex.go:25-95](../../../workers-go/reindex/workflows/reindex.go)):

```text
loop:
  page  = ScanCassandraObjects({tenant_id, type_id, page_size, resume_token})
  if page.records empty -> break
  publishResult = PublishReindexBatch({topic="ontology.reindex.v1", records: page.records})
  if page.next_token == "" -> break
  resume_token = page.next_token
  if scanned % 50_000 == 0 -> ContinueAsNew(input{resume_token})
return {Scanned, Published, Status="completed"}
```

* **Determinism**: the page-token chain reproduces identically on
  replay (gocql `PageState` is treated as opaque bytes); the publish
  activity is idempotent because every record carries a deterministic
  `event_id` (UUID v5 of `aggregate || aggregate_id || version` —
  stamped downstream by `ontology-indexer`'s decoder, which is shared
  with the live `ontology.object.changed.v1` topic).
* **Retry / backoff**: `workflow.ActivityOptions{ StartToCloseTimeout: 2m,
  HeartbeatTimeout: 30s, RetryPolicy: { Initial: 1s, BackoffCoeff: 2.0,
  Max: 1m, MaximumAttempts: 0 } }` ([reindex.go:33-42](../../../workers-go/reindex/workflows/reindex.go)).
  Unlimited attempts with backoff is the rate-limiter — Temporal is
  carrying both the resume-on-failure semantics and the throttling
  envelope.
* **Continue-as-new**: every 50k scanned records the workflow
  re-enters with the same input shape plus the latest `ResumeToken`.
  This is the *only* reason the workflow exists as more than a single
  activity invocation — it caps history size for long backfills.
* **Failure model**: any activity error sets `res.Status = "failed"`,
  attaches `Error`, and returns; cancel is reported as `"cancelled"`
  per the `Status` enum doc ([contract.go:61](../../../workers-go/reindex/internal/contract/contract.go))
  but is not currently emitted by the workflow body itself.

### 2.2 Inputs / outputs

`OntologyReindexInput` ([contract.go:45-54](../../../workers-go/reindex/internal/contract/contract.go)):

| Field | Type | Required | Notes |
|---|---|---|---|
| `tenant_id` | `string` | yes | One workflow execution per tenant; system-wide reindex fans out one workflow per tenant. |
| `type_id` | `string` | no | Empty ⇒ scan all types via `objects_by_type` with `ALLOW FILTERING`; non-empty ⇒ partition-keyed scan. |
| `page_size` | `int` | no | Defaults to `1000` at activity boundary if zero. |
| `resume_token` | `string` | no | Base64-encoded gocql `PageState`. Carrier of cursor across continue-as-new — **the artifact this refactor moves to Postgres.** |

`OntologyReindexResult` ([contract.go:57-63](../../../workers-go/reindex/internal/contract/contract.go)):

| Field | Type | Notes |
|---|---|---|
| `tenant_id` | `string` | Echo of input. |
| `scanned` | `int64` | Sum across all pages of this execution **and** any continue-as-new chain (chain executions return their own counter; the aggregate is the union of run results). |
| `published` | `int64` | Sum of records actually produced to Kafka (`ProduceSync` first-error semantics — partial batches do not increment). |
| `status` | `string` | `"completed"` \| `"failed"` \| `"cancelled"`. |
| `error` | `string` | Set iff `status == "failed"`. |

---

## 3. Activity inventory

### 3.1 `ScanCassandraObjects` ([activities.go:57-92](../../../workers-go/reindex/activities/activities.go))

* **Input**: `{ tenant_id, type_id?, page_size, resume_token? }` (validated:
  `tenant_id` required, `page_size <= 0` re-defaulted to 1000).
* **Output**: `{ records: []map[string]any, next_token: string }`.
* **Two scan paths**:
  - `scanByType` ([activities.go:172](../../../workers-go/reindex/activities/activities.go)):
    `SELECT object_id FROM ontology_objects.objects_by_type WHERE tenant=? AND type_id=?`
    paged by `gocql.PageState`.
  - `scanAllTypes` ([activities.go:206](../../../workers-go/reindex/activities/activities.go)):
    `SELECT type_id, object_id FROM objects_by_type WHERE tenant=? ALLOW FILTERING`
    (cluster-wide scan; same paging mechanism).
* **Hydration** (`fetchObject`, [activities.go:237](../../../workers-go/reindex/activities/activities.go)):
  per id, `SELECT type_id, properties, revision_number, deleted FROM
  objects_by_id WHERE tenant=? AND object_id=?`. Deleted rows are
  filtered out of the published batch; `properties` is JSON-decoded
  and re-emitted as `payload`; an `embedding` field is extracted
  separately if present.
* **Output record shape** (mirrors `ontology.object.changed.v1`):
  `{ tenant, id, type_id, version, payload, deleted: false[, embedding] }`.
* **Cursor encoding**: `encodePageState` is `base64.StdEncoding`
  ([activities.go:371](../../../workers-go/reindex/activities/activities.go)) — opaque
  bytes, both for input (`resume_token`) and output (`next_token`).

### 3.2 `PublishReindexBatch` ([activities.go:94-117](../../../workers-go/reindex/activities/activities.go))

* **Input**: `{ topic, records: []map[string]any }`.
* **Output**: `{ published: int64 }` (= `len(records)` on success).
* **Producer**: franz-go `kgo.Client` with `RequiredAcks(AllISRAcks)`,
  optional TLS / SASL-SCRAM-256/512.
* **Partition key**: `<tenant>/<id>` per record
  ([activities.go:289-301](../../../workers-go/reindex/activities/activities.go)). This is
  what guarantees same-object ordering across `ontology.object.changed.v1`
  and `ontology.reindex.v1` for the indexer's compaction logic.
* **Atomicity**: `ProduceSync(ctx, ...).FirstErr()` — the activity
  fails on the **first** record error and lets Temporal retry the
  whole batch. With deterministic `event_id` downstream, replays are
  safe.

### 3.3 Lazy initialisation

`ensureInitialized` ([activities.go:119-163](../../../workers-go/reindex/activities/activities.go)) opens
the gocql session and the franz-go client once per process under a
`sync.Once`. There is no graceful shutdown — Temporal worker
interrupt handles it.

---

## 4. Kafka / Cassandra topology that the worker observes

| Surface | Value | Source |
|---|---|---|
| Output topic | `ontology.reindex.v1` (declared in helm) | [contract.go:32](../../../workers-go/reindex/internal/contract/contract.go), [kafka-cluster values.yaml:85](../../../infra/helm/infra/kafka-cluster/values.yaml) |
| DLQ for output topic | `__dlq.ontology.reindex.v1` (14 d retention) | [kafka-cluster values.yaml:103](../../../infra/helm/infra/kafka-cluster/values.yaml) |
| Downstream consumer | `services/ontology-indexer` (single consumer group, reads both `ontology.object.changed.v1` and `ontology.reindex.v1`) | [services/ontology-indexer/src/lib.rs:41-44](../../../services/ontology-indexer/src/lib.rs), [reindex README.md:9-10](../../../workers-go/reindex/README.md) |
| Cassandra source tables | `ontology_objects.objects_by_type`, `ontology_objects.objects_by_id` | [activities.go:174,239](../../../workers-go/reindex/activities/activities.go) |

Topic separation rationale (preserved by FASE 4): backfill traffic on
`ontology.object.changed.v1` would starve the live consumer group;
same payload shape, separate topic, one consumer group reads both
([README.md:12-16](../../../workers-go/reindex/README.md)).

---

## 5. Cursor persistence — today vs. FASE 4

| Concern | Today (Temporal) | FASE 4 (Rust consumer) |
|---|---|---|
| Where the cursor lives | `ResumeToken` field of `OntologyReindexInput`, kept in workflow state and re-emitted via `workflow.NewContinueAsNewError` every 50k records ([reindex.go:46,82-89](../../../workers-go/reindex/workflows/reindex.go)). | Row in `pg-runtime-config.reindex_jobs(id, tenant_id, type_id, status, resume_token, started_at, completed_at)` (schema per migration plan §4.2). |
| Resume semantics on crash | Temporal replays from the last activity boundary; the `PageState` already returned by `ScanCassandraObjects` is in workflow event history. | Coordinator commits Kafka offset **after** persisting `(status, resume_token)` to Postgres in the same idempotent unit (UUID v5 of `tenant||type||token`); restart re-reads the row by job id. |
| History bound | Continue-as-new every 50k records caps Temporal history. | No equivalent needed — Postgres row is O(1) per job; consumer is stateless across restarts. |
| Cancellation | Temporal cancel signal (not currently surfaced in workflow body — `Status="cancelled"` is documented but unreachable). | Postgres `status` transition `running → cancelled` driven by control-plane HTTP route; consumer polls between batches. |
| Trigger | External (manual / unscoped — no in-tree caller of `task_queues::REINDEX`). | Kafka event on `ontology.reindex.requested.v1`. |
| Completion signal | Workflow result returned via Temporal client (no event published). | New event `ontology.reindex.completed.v1` (introduced in Tarea 4.2). |

---

## 6. Activity → consumer mapping

This is the table that drives the Rust port in Tarea 4.2.

| Go construct | Rust replacement | Notes |
|---|---|---|
| `contract.TaskQueue = "openfoundry.reindex"` | Kafka consumer group `reindex-coordinator-service` on `ontology.reindex.requested.v1` | Drops the Temporal task queue entirely. |
| `OntologyReindexInput` (workflow input) | Event payload on `ontology.reindex.requested.v1` (`{tenant_id, type_id?, page_size?}` — **no `resume_token` from the caller**, the coordinator owns it) | Caller no longer supplies cursor; the coordinator looks up / inserts `reindex_jobs` row. |
| `workflow.ExecuteActivity(ScanCassandraObjects, ...)` | Direct gocql call inside the consumer loop, paginating with `cassandra-kernel` lib | Same `objects_by_type` + `objects_by_id` queries; same opaque base64 `PageState`. |
| `workflow.ExecuteActivity(PublishReindexBatch, ...)` | Direct produce via `event-bus-data` (rdkafka) | Same `<tenant>/<id>` partition key; same JSON shape. **No payload change** — `ontology-indexer` decoder stays untouched. |
| `ResumeToken` in workflow state + `ContinueAsNew` every 50k | Row update `UPDATE pg-runtime-config.reindex_jobs SET resume_token=$1, scanned=scanned+$2 WHERE id=$3` after each successfully published batch | Removes the 50k continue-as-new; Postgres row is the single source of truth. |
| `OntologyReindexResult` (return value) | `ontology.reindex.completed.v1` event `{job_id, tenant_id, type_id?, scanned, published, status, error?}` + `reindex_jobs.completed_at` | Result is now an event, not an RPC return; status enum unchanged. |
| `workflow.ActivityOptions.RetryPolicy` (1s → 60s exp) | Consumer-side `tokio::time::sleep` with the same envelope on transient gocql / kafka errors; dead-letter to `__dlq.ontology.reindex.v1` after N attempts | Makes the rate-limiter explicit instead of implicit-via-Temporal-backoff. |
| Per-record idempotency via downstream UUID v5 | Same downstream contract; coordinator additionally stamps a per-batch `event_id = UUID v5(tenant||type||resume_token)` for `idempotency` lib | Allows safe re-publish of the same page after a crash before the row update committed. |
| `sync.Once` lazy init of gocql + franz-go | Service `AppState` constructed in `main.rs`, shared via `Arc` | Standard pattern across `services/*`. |

---

## 7. Risks / non-functional notes

* **Cassandra throughput** — the Temporal backoff (1s → 60s,
  unlimited attempts) is the only rate-limit today. The Rust
  coordinator must re-introduce an explicit `OF_REINDEX_PAGE_INTERVAL_MS`
  / `OF_REINDEX_MAX_INFLIGHT_BATCHES` so a full backfill cannot
  saturate `objects_by_id` reads (called once per id).
* **`ALLOW FILTERING` path** — `scanAllTypes` is a cluster-wide scan
  with `ALLOW FILTERING`, kept for the "reindex-everything" admin
  call. Behaviour preserved by Tarea 4.2; no schema change.
* **Restart safety** — today, restart safety comes from Temporal
  history replay. After Tarea 4.2 it comes from the
  `reindex_jobs.resume_token` row + Kafka consumer offset on
  `ontology.reindex.requested.v1`; the offset must only be committed
  **after** the row update, otherwise a crash between the two would
  silently drop the job.
* **Continue-as-new removal** — the 50k boundary disappears. The
  coordinator's job loop is `while let Some(token) = next_token { ... }`
  with the row updated each iteration; there is no analogous bound to
  enforce.
* **Empty `task_queues::REINDEX` callers** — confirmed via
  `grep -rn 'task_queues::REINDEX' libs/ services/`: the only hit is
  the constant definition itself ([temporal-client lib.rs:1631](../../../libs/temporal-client/src/lib.rs)).
  Tarea 4.3 (`Eliminar workers-go/reindex/`) can therefore drop both
  the Go binary and the Rust constant in the same change without a
  caller-side migration.

---

## 8. Acceptance for Tarea 4.1

* This document exists at `docs/architecture/refactor/reindex-worker-inventory.md`.
* Section 2.2 documents inputs (`TenantID`, `TypeID?`, `PageSize?`,
  `ResumeToken?`) and outputs (`Scanned`, `Published`, `Status`, `Error?`).
* Section 5 identifies the cursor's current home (workflow
  `ResumeToken` + `ContinueAsNew`) and its FASE 4 destination
  (`pg-runtime-config.reindex_jobs.resume_token`).
* Section 6 maps every Go activity / Temporal primitive to its Rust
  consumer counterpart, ready to drive Tarea 4.2.
