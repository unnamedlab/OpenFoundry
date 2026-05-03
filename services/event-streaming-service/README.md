# event-streaming-service

OpenFoundry's streaming control plane. Hosts the REST control plane
(`/api/v1/streaming/*`), the legacy gRPC routing facade, and the
push proxy used by Foundry-style push-based ingestion.

## Endpoints

### Authenticated control plane (`/api/v1/streaming/*`)

* CRUD for `streams`, `windows`, `topologies`.
* Reset stream + view history:
  * `POST /streams/{id}/reset` — rotate `viewRid`, optionally update
    schema/config (see "Reset stream" below).
  * `GET  /streams/{id}/views` — full history.
  * `GET  /streams/{id}/current-view` — the active view.
* Stream config: `GET /streams/{id}/config`, `PUT /streams/{id}/config`.

### Unauthenticated push proxy (`/streams-push/*`)

Mirrors Foundry's "Push data into a stream" workflow. Push consumers
authenticate with a bearer token issued by the platform's OAuth2
third-party-application flow — token validation is performed by the
platform gateway, not by this service.

* `POST /streams-push/{view_rid}/records`
* `GET  /streams-push/{stream_rid}/url`

#### Body shapes

The push endpoint accepts both Foundry-style records and a bare
`values[]` array so SDK examples translate cleanly:

```jsonc
// Foundry shape — preferred
{ "records": [{ "value": { "sensor_id": "s1", "temperature": 4.1 } }] }

// SDK-friendly shape
{ "values": [ { "sensor_id": "s1", "temperature": 4.1 } ] }
```

Optional per-record `event_time` (RFC3339) and `key` (used for Kafka
partitioning) are honoured when present.

#### Error codes

| HTTP | Code                                  | Meaning                                                                 |
|------|---------------------------------------|-------------------------------------------------------------------------|
| 401  | `PUSH_MISSING_BEARER_TOKEN`           | `Authorization: Bearer …` header missing.                              |
| 404  | `PUSH_VIEW_RETIRED`                   | View has been rotated. Re-fetch via `GET /streams-push/{rid}/url`.      |
| 422  | `PUSH_SCHEMA_VALIDATION_FAILED`       | One or more records violate the active view's schema.                   |
| 503  | `PUSH_RATE_LIMITED`                   | Per-view RPS cap exceeded (default 200 r/s; tune `STREAMING_PUSH_RPS`). |
| 502  | (no code)                             | Upstream broker (Kafka / NATS) rejected the publish.                    |

#### Retry behaviour

Push consumers should follow the Foundry-recommended retry policy:

1. On `503 PUSH_RATE_LIMITED`, back off with exponential jitter
   (start at ~250 ms, double up to 4 s) and retry the **same** batch.
2. On `502`, retry the batch with the same exponential policy. The
   service is idempotent at the Kafka producer layer (`acks=all`,
   idempotent producer enabled) so duplicate publishes are
   deduplicated by the broker.
3. On `404 PUSH_VIEW_RETIRED`, do **not** retry against the same
   `view_rid`. Re-fetch the active URL and resume against the new
   `view_rid`. Records produced before the retire instant are not
   recoverable — the operator chose the reset.
4. On `422 PUSH_SCHEMA_VALIDATION_FAILED`, fail loud — the agent must
   reconcile its schema. Retrying is pointless until the schema or
   payload is fixed.

### Reset stream (`POST /streams/{id}/reset`)

Mirrors the Foundry "Reset stream" workflow. Only available on streams
with `kind = INGEST` — downstream/derived streams cannot be reset
(returns `422 STREAM_RESET_ONLY_INGEST_KIND`).

```jsonc
// Body (all fields optional)
{
  "new_schema": { /* StreamSchema-shaped JSON; optional */ },
  "new_config": { /* StreamConfig patch; optional */    },
  "force":      false  // override the downstream-active guard
}
```

Returns the rotated `view_rid`, the retired `old_view_rid`, the new
`generation`, and the rotated POST URL push consumers should switch
to. The handler emits a `stream.reset.v1` event over the
transactional outbox + an `audit` tracing event with
`{stream_rid, old_view_rid, new_view_rid, generation, schema_changed,
config_changed, forced}` so SREs can prove the chain of custody.

If a downstream pipeline is still active and `force=false`, the call
returns `409 STREAM_RESET_DOWNSTREAM_PIPELINES_ACTIVE`. Pass
`force=true` after acknowledging the replay requirement.

### Streaming profiles (P3)

Foundry's "Streaming profiles" workflow lives at
`POST /api/v1/streaming/streaming-profiles` for CRUD,
`POST /api/v1/streaming/projects/{project_rid}/streaming-profile-refs/{profile_id}`
to import a profile into a project, and
`POST /api/v1/streaming/pipelines/{pipeline_rid}/streaming-profiles` to
attach an imported profile to a pipeline. The runtime composes the
effective Flink config via
`GET /api/v1/streaming/pipelines/{pipeline_rid}/effective-flink-config`.

#### Authorization

| Action                              | Required permission / role                          |
|-------------------------------------|----------------------------------------------------|
| List profiles                       | (any authenticated caller)                         |
| Create / update / restrict          | `admin`, `streaming_admin`, or `streaming:profile-write` |
| Import (`POST .../streaming-profile-refs/...`) | `compass:import-resource-to`                |
| Import a `restricted` profile       | additionally requires `enrollment_resource_administrator` |
| Attach to pipeline                  | `streaming:write`                                  |

`LARGE` size-class profiles default to `restricted = true`; an admin
can explicitly opt-out at create/patch time.

#### Flink config-key whitelist

Only the keys below may appear in `config_json`. Everything else is
rejected at write-time with `STREAMING_PROFILE_INVALID_FLINK_KEY`. The
whitelist intentionally mirrors the surface Foundry exposes — the
platform manages the rest of the Flink runtime for you.

* **TaskManager resources**: `taskmanager.memory.process.size`,
  `taskmanager.memory.flink.size`, `taskmanager.memory.task.heap.size`,
  `taskmanager.memory.managed.fraction`,
  `taskmanager.numberOfTaskSlots`.
* **JobManager resources**: `jobmanager.memory.process.size`,
  `jobmanager.memory.flink.size`.
* **Parallelism**: `parallelism.default`, `pipeline.max-parallelism`.
* **Network**: `taskmanager.network.memory.fraction`,
  `taskmanager.network.memory.min`, `taskmanager.network.memory.max`,
  `taskmanager.network.numberOfBuffers`.
* **Checkpointing**: `execution.checkpointing.interval`,
  `execution.checkpointing.timeout`,
  `execution.checkpointing.min-pause`,
  `execution.checkpointing.max-concurrent-checkpoints`.
* **State backend**: `state.backend.type`,
  `state.backend.incremental`,
  `state.backend.rocksdb.timer-service.factory`.
* **Restart strategy**: `restart-strategy`,
  `restart-strategy.fixed-delay.attempts`,
  `restart-strategy.fixed-delay.delay`.

#### Built-in profiles (seeded by `20260504000003_streaming_profiles.sql`)

| Name                | Category                | Size class | Restricted | Defaults                                   |
|---------------------|-------------------------|------------|------------|--------------------------------------------|
| `Default`           | TASKMANAGER_RESOURCES   | SMALL      | no         | `process.size=2048m`, `slots=2`, `parallelism=2` |
| `High Parallelism`  | PARALLELISM             | MEDIUM     | no         | `parallelism.default=8`, `slots=4`         |
| `Large State`       | TASKMANAGER_RESOURCES   | LARGE      | **yes**    | `process.size=8192m`, `state.backend=rocksdb`, incremental |
| `Large Records`     | NETWORK                 | LARGE      | **yes**    | network buffers fraction `0.2`, min `256mb`, max `2gb` |

#### Effective config resolution

`GET /pipelines/{pipeline_rid}/effective-flink-config` composes the
union of every attached profile using two ordered keys:

1. **Category specificity** (lower = more specific): TaskManager →
   JobManager → Parallelism → Network → Checkpointing → Advanced. More
   specific categories run first, so a follow-up Advanced profile can
   override them by design.
2. **`attached_order`** (later wins) for ties within a category.

Every overridden key emits a `tracing::warn!` event and a `warnings[]`
entry in the response so operators can audit conflicts.

#### Error codes

| HTTP | Code                                                       | Meaning                                                          |
|------|------------------------------------------------------------|------------------------------------------------------------------|
| 422  | `STREAMING_PROFILE_INVALID_FLINK_KEY`                      | `config_json` references a key outside the whitelist.            |
| 403  | `STREAMING_PROFILE_RESTRICTED_REQUIRES_ENROLLMENT_ADMIN`   | Restricted profile import attempted by a non-Enrollment-Admin.   |
| 412  | `STREAMING_PROFILE_NOT_IMPORTED`                           | Pipeline attach without a project ref. Import via Control Panel. |

## Environment

| Variable                        | Default                  | Purpose                                                |
|---------------------------------|--------------------------|--------------------------------------------------------|
| `STREAMING_PUBLIC_BASE_URL`     | `http://localhost:8080`  | Base URL surfaced to push consumers in `push_url`.     |
| `STREAMING_PUSH_RPS`            | `200`                    | Per-view records/sec cap for the push proxy.           |
| `KAFKA_BOOTSTRAP_SERVERS`       | (unset)                  | Enables the `KafkaHotBuffer` backend.                  |
| `FLINK_*`                       | (see `runtime/flink/`)   | Flink runtime knobs (gated by `flink-runtime` feature).|

See `migrations/README.md` for the migration ordering policy.

## Foundry parity matrix

Each row points at the file in this repo that implements the
corresponding Foundry doc surface. ✅ = implemented and tested,
🟡 = partial / stub (typically because an external dependency is
swapped for a mock), ❌ = not implemented.

| Foundry doc section                  | Status | Implementation                                                                                  |
|--------------------------------------|:------:|--------------------------------------------------------------------------------------------------|
| Streams › Hot buffer                 | ✅     | `src/domain/hot_buffer/{kafka,nats}.rs`                                                          |
| Streams › Cold buffer / Read         | ✅     | `src/handlers/streams.rs::read_stream` + `runtime_store::cold_watermark`                         |
| Streams › Stream types               | ✅     | `proto/streaming/streams.proto`, `src/models/stream.rs::StreamType`                              |
| Streams › Partitions                 | ✅     | `migrations/20260504000001_stream_config.sql` CHECK 1..50, `KafkaHotBuffer::ensure_topic`        |
| Streams › Supported field types      | ✅     | `libs/core-models::dataset::schema::FieldType` + `src/models/schema_bridge.rs` round-trip        |
| Streams › Streaming Jobs / Job graphs| ✅     | `src/runtime/flink/job_graph.rs` + `apps/web/src/lib/components/streaming/JobGraph.svelte`       |
| Streams › Checkpointing              | ✅     | `src/handlers/checkpoints.rs`, `src/domain/checkpoints.rs`                                       |
| Streams › Consistency guarantees     | ✅     | `StreamConsistency` enum + `effective_exactly_once`                                              |
| Reset stream                         | ✅     | `src/handlers/stream_views.rs` + `migrations/20260504000002_stream_views.sql`                    |
| Push data into a stream              | ✅     | `src/handlers/push_proxy.rs` (HTTP); see retry table above                                       |
| Streaming profiles                   | ✅     | `src/handlers/profiles.rs` + `src/models/profile.rs::compose_effective_config`                   |
| Stream monitoring                    | ✅     | `monitoring-rules-service::evaluator` + `src/handlers/streams.rs::get_stream_metrics`            |
| Streaming compute usage              | ✅     | `migrations/20260504000005_streaming_compute.sql` + `src/handlers/usage.rs`                      |
| Streaming pipelines › Performance    | 🟡     | Stream profiles + Flink runtime; full Pipeline-Builder integration tracked separately            |
| Streaming pipelines › Stateful       | ✅     | `migrations/20260504000006_streaming_stateful.sql` + `models::window::key_prefix_for`            |
| Streaming pipelines › Streaming keys | ✅     | `key_columns` + `keyed` window flag; UI in `WindowConfig.svelte`                                 |
| Streaming pipelines › vs. batch      | 🟡     | Doc references; runtime is split between Flink (real) and the in-process engine (dev)            |
| Streaming pipelines › Overview       | ✅     | `runtime/flink/sql.rs` SQL emitter; topology CRUD wired                                          |
| Source connectors › Kafka            | ✅     | `src/domain/connectors/kafka_source.rs`                                                          |
| Source connectors › Kinesis          | ✅     | `src/domain/connectors/kinesis.rs` (HTTP/SigV4-shaped, mockable)                                 |
| Source connectors › SQS              | ✅     | `src/domain/connectors/sqs.rs` (long-poll + per-message ack)                                     |
| Source connectors › Aveva PI         | ✅     | `src/domain/connectors/aveva_pi.rs` (PI Web API polling)                                         |
| Source connectors › Pub/Sub          | ✅     | `src/domain/connectors/pubsub.rs` (REST pull + ack-deadline extend)                              |
| External transforms (ActiveMQ, MQTT…)| ✅     | `src/domain/connectors/external.rs` (Magritte agent webhook hook)                                |
| Debug a failing stream               | ✅     | See "Debug a failing stream" below                                                               |

## Debug a failing stream

Mirrors Foundry's "Debug a failing stream" runbook. The flow assumes
the operator has access to the streaming UI + Prometheus.

1. **Inspect the checkpoint timeline.** Open the stream's `Job
   Details` tab — the "Last checkpoints" card auto-refreshes every
   10 s. A run of `FAILED`/`TIMED_OUT` rows is the strongest signal
   that the runtime is unhealthy. The same data is available over
   REST at `GET /api/v1/streaming/topologies/{id}/checkpoints?last=20`.
2. **Check the dead-letter queue.** The streaming detail page now has
   a `Dead letters` tab. Each row carries the rejection reason
   (`schema_validation_failed`, `avro_validation_failed`,
   `hot_buffer_publish_failed`) plus the original payload so the
   operator can replay or fix the producer. The same data is
   available over REST at `GET /api/v1/streaming/streams/{id}/dead-letters`
   and replay via `POST /dead-letters/{id}/replay`.
3. **Look at `stream_lag_ms` and `streaming_compute_seconds_total`.**
   Both metrics are exposed on the admin port's `/metrics` endpoint.
   A growing `stream_lag_ms` plus a flat or zero `streaming_records_ingested_total`
   is the canonical "ingest is broken" pattern. A spike in
   `streaming_topology_restarts_total` points at a poison-pill
   record.
4. **Reset the stream when records are unrecoverable.** Foundry's
   "Reset stream" workflow rotates the `viewRid` so push consumers
   must update their POST URL. Either trigger the reset via the
   `Settings` tab → "Reset stream" modal, or call
   `POST /api/v1/streaming/streams/{id}/reset`. The handler emits
   `stream.reset.v1` over the outbox so downstream pipelines can
   replay automatically.

