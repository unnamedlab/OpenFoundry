# `action-log-sink` (Go)

## LLM quick context (current code)

Consumes action/audit-style event streams and persists action-log envelopes to Iceberg/table-writer or local JSONL for auditability.

Agent note: background sink; no product REST API; exposes only health/metrics.

Current surface:
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- No SQL migration files live under this service directory.
- Main internal packages: `config`, `envelope`, `runtime`, `server`, `writer`.
- Local service files present: `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `ACTION_LOG_SINK_JSONL_PATH`, `ACTION_LOG_SINK_TABLE_WRITER_URL`, `ICEBERG_CATALOG_URL`, `ICEBERG_TABLE_WRITER_URL`, `ICEBERG_WAREHOUSE`, `KAFKA_BOOTSTRAP_SERVERS`, `KAFKA_CLIENT_ID`, `KAFKA_SASL_MECHANISM`
- `KAFKA_SASL_PASSWORD`, `KAFKA_SASL_USERNAME`, `KAFKA_SECURITY_PROTOCOL`, `METRICS_ADDR`, `SERVICE_VERSION`

Keep this section in sync when changing routes, config, or persistence behavior.

Kafka → Iceberg consumer. Subscribes to `ontology.actions.applied.v1`,
batches `ActionEnvelope` records (default: 100k OR 60s, whichever
first) and appends them to `lakekeeper.default.action_log` via the
OpenFoundry Iceberg HTTP append adapter
(`POST /openfoundry/iceberg/v1/append`, served by
`services/iceberg-catalog-service`).

Phase B of [ADR-0045](../../docs/architecture/adr/ADR-0045-eliminate-pipeline-runner-spark-pure-go-runtime.md) —
replaces the Scala `com.openfoundry.audit.ActionLogStreamSink`
shipped in `services/pipeline-runner-spark/`. CLI surface is replaced
by env vars; the `SparkApplication` CR at
`infra/dev/action-log-sink.yaml` is rewritten as a plain `Deployment`.

## At-least-once semantics

`Writer.Append` runs BEFORE the Kafka offset commit. A crash between
the Iceberg append and the offset commit replays the batch on
restart; downstream dedup by the immutable `event_id` (produced by
`libs/ontology-kernel/handlers/actions/side_effects.go::publishActionAuditToKafka`)
handles duplicates. Same trade-off as `audit-sink` and `ai-sink`; see
ADR-0045 § Consequences.

This is **a deliberate downgrade** from the Scala sink's exactly-once
semantics (Spark Structured Streaming + S3 checkpoints). Any query
against `action_log` that aggregates without `DISTINCT event_id` must
be reviewed before this sink ships to production.

## Topic, table, schema

| Concern | Value |
|---|---|
| Source topic | `ontology.actions.applied.v1` |
| Consumer group | `action-log-sink` |
| Target catalog | `lakekeeper` |
| Target namespace | `default` |
| Target table | `action_log` |
| Partition | `day(applied_at_ms)` |
| Sort order | `applied_at_ms ASC` |

Schema (16 columns, see [`internal/writer/iceberg.go`](internal/writer/iceberg.go) `ActionSchema()`):

| # | Name | Type | Required |
|---:|---|---|---|
| 1 | event_id | string | ✓ |
| 2 | action_type_id | string | ✓ |
| 3 | action_name | string | ✓ |
| 4 | object_type_id | string | ✓ |
| 5 | object_id | string | |
| 6 | tenant | string | ✓ |
| 7 | actor_sub | string | ✓ |
| 8 | actor_email | string | |
| 9 | organization_id | string | |
| 10 | status | string | ✓ |
| 11 | parameters | string (JSON-encoded) | |
| 12 | previous_state | string (JSON-encoded) | |
| 13 | new_state | string (JSON-encoded) | |
| 14 | target_classification | string | |
| 15 | applied_at_ms | long (epoch ms) | ✓ |
| 16 | kafka_ts | timestamptz (set by sink at flush time) | |

The DDL for first-time table creation lives in the header comment of
[`infra/dev/action-log-sink.yaml`](../../infra/dev/action-log-sink.yaml).

## Configuration

Operator-facing env contract — names pinned to keep a single Helm
`values.yaml` template across the three sinks.

| Variable | Required | Purpose |
|---|:---:|---|
| `KAFKA_BOOTSTRAP_SERVERS` | ✅ | CSV `host:port` list of brokers |
| `KAFKA_SASL_USERNAME` | | SASL principal (default: `KAFKA_CLIENT_ID` or `action-log-sink`) |
| `KAFKA_SASL_PASSWORD` | | When unset → PLAINTEXT (dev / docker-compose) |
| `KAFKA_SASL_MECHANISM` | | Default `SCRAM-SHA-512` |
| `KAFKA_SECURITY_PROTOCOL` | | Default `SASL_SSL` |
| `ICEBERG_CATALOG_URL` | ✅ in Iceberg mode | Lakekeeper REST URL forwarded to the HTTP append adapter. Optional only when `ACTION_LOG_SINK_JSONL_PATH` selects the dev fallback |
| `ICEBERG_TABLE_WRITER_URL` | | Override for the HTTP adapter base URL; defaults to `ICEBERG_CATALOG_URL` when same origin |
| `ACTION_LOG_SINK_TABLE_WRITER_URL` | | Per-sink override of the above |
| `ICEBERG_WAREHOUSE` | | Iceberg warehouse identifier (e.g. `openfoundry`) |
| `ACTION_LOG_SINK_BATCH_MAX_RECORDS` | | Default 100k |
| `ACTION_LOG_SINK_BATCH_MAX_WAIT_SECONDS` | | Default 60s |
| `ACTION_LOG_SINK_JSONL_PATH` | | Path to JSONL output file (`-` = stdout). When set, JSONLWriter is used instead of the Iceberg adapter |
| `METRICS_ADDR` | | Default `0.0.0.0:9090` |
| `OTEL_TRACES_EXPORTER` | | `none` to disable tracing |
| `LOG_FORMAT` | | `json` for production |

## Endpoints

- `GET /healthz` — liveness JSON (`core_models::health` payload).
- `GET /metrics` — Prometheus scrape from a service-local registry.

## Metrics

| Name | Type | Labels | Meaning |
|---|---|---|---|
| `action_log_sink_lag_seconds` | histogram | — | seconds between action `applied_at_ms` and successful Iceberg append |
| `action_log_sink_records_total` | counter | — | total records appended |
| `action_log_sink_batch_size_records` | histogram | — | records per successful Iceberg append |
| `action_log_sink_commits_total` | counter | `outcome=success\|failure\|poison` | append attempts by outcome |

## Failure model

| Outcome | Trigger | Sink behaviour |
|---|---|---|
| `success` | adapter 2xx | offsets committed, metrics updated |
| `failure` | adapter 4xx (non-422), 5xx, or transport error | offsets **NOT** committed, runtime returns the wrapped error, supervisor restarts the pod, batch replays |
| `poison` | JSON decode or missing-required-field validate | the bad record is **not** appended; its offset **is** committed alongside the next successful flush so the consumer does not loop |

Typed sentinels for callers (see `internal/writer/writer.go`):

- `ErrEmptyBatch` — sink guards against zero-length batches.
- `ErrTableNotFound` — HTTP 404 from the adapter (table missing — apply the DDL).
- `ErrSchemaMismatch` — HTTP 409 / 422 (row shape diverged from table schema).
- `ErrCommitFailed` — everything else.

## Build / run

```sh
make build-services

KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
ICEBERG_CATALOG_URL=http://iceberg-catalog-service:8080 \
ICEBERG_WAREHOUSE=openfoundry \
OTEL_TRACES_EXPORTER=none \
./bin/action-log-sink

# Dev JSONL fallback:
ACTION_LOG_SINK_JSONL_PATH=/var/log/action_log.jsonl ./bin/action-log-sink
```

## Test

```sh
go test ./services/action-log-sink/...
go test -race ./services/action-log-sink/...
```

Unit tests cover envelope decode + required-field validation, the
HTTP adapter against `httptest.Server` (happy path + 404 + 409 +
422 + 5xx + empty-batch), env config parsing, and the runtime loop
with a fake subscriber + fake writer for flush-by-records, poison
handling, writer-error-preserves-batch, and ctx-cancel final-flush.
End-to-end smoke against a live Kafka + Lakekeeper + iceberg-catalog-service
is the exit criterion of ADR-0045 Phase B.
