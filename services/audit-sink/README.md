# audit-sink (Go)

## LLM quick context (current code)

Consumes audit events, persists them to Postgres/Iceberg/JSONL, and exposes audit-event query/export APIs.

Agent note: both a sink runtime and a read/write HTTP service for /api/v1/audit/events.

Current surface:
- `GET/POST /api/v1/audit/events`
- `GET /api/v1/audit/events/export`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `envelope`, `handlers`, `repo`, `runtime`, `server`, `writer`.
- Local service files present: `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `AUDIT_SINK_JSONL_PATH`, `DATABASE_URL`, `ICEBERG_CATALOG_URL`, `ICEBERG_WAREHOUSE`, `KAFKA_BOOTSTRAP_SERVERS`, `KAFKA_CLIENT_ID`, `KAFKA_SASL_MECHANISM`, `KAFKA_SASL_PASSWORD`
- `KAFKA_SASL_USERNAME`, `KAFKA_SECURITY_PROTOCOL`, `METRICS_ADDR`, `SERVICE_VERSION`

Keep this section in sync when changing routes, config, or persistence behavior.

Kafka → Iceberg consumer. Subscribes to `audit.events.v1`, batches the
`AuditEnvelope` records (default: 100k OR 60s, whichever first) and
appends them to `lakekeeper/of_audit/events`. Closes the loop on
everything `edge-gateway-service` and `libs/audit-trail` publish.

## At-least-once semantics

`Writer.Append` happens BEFORE the Kafka offset commit. A crash between
the Iceberg append and the commit replays the batch on restart;
downstream dedup (Iceberg primary key by `event_id`, or an
`idempotency.Store` if one is wired in) handles duplicates. Same
trade-off — see ADR-0022 § Consumer-side contract.

## Writer backends

`internal/writer.Writer` interface, two implementations:

| Implementation   | Trigger                              | Status                      |
| ---------------- | ------------------------------------ | --------------------------- |
| `IcebergWriter`  | (default — when JSONL path is unset) | Production path via the OpenFoundry Iceberg table-writer adapter |
| `JSONLWriter`    | `AUDIT_SINK_JSONL_PATH` set          | Explicit dev/staging fallback for local observability |

The Iceberg writer follows the canonical contract (`of_audit.events`,
`day(at)`, `at ASC`, and one durable append per flushed batch).
Because `apache/iceberg-go` still does not expose a stable end-to-end
writer for batched appends, the production path is an explicit HTTP
adapter at `ICEBERG_CATALOG_URL/openfoundry/iceberg/v1/append`.
That adapter is responsible for writing Parquet data files and committing
the Iceberg snapshot atomically. It fails loudly: empty batches,
missing tables, schema mismatches, and commit failures all return typed
writer errors; JSONL is never selected unless `AUDIT_SINK_JSONL_PATH` is
set.

## Configuration

Operator-facing env contract — variable names are pinned so a single
Helm `values.yaml` drives every audit-sink deployment.

| Variable                            | Required | Purpose                                   |
| ----------------------------------- | :------: | ----------------------------------------- |
| `KAFKA_BOOTSTRAP_SERVERS`           | ✅       | CSV `host:port` list of brokers          |
| `KAFKA_SASL_USERNAME`               |          | SASL principal (default: `KAFKA_CLIENT_ID` or `audit-sink`) |
| `KAFKA_SASL_PASSWORD`               |          | When unset → PLAINTEXT (dev / docker-compose)            |
| `KAFKA_SASL_MECHANISM`              |          | Default `SCRAM-SHA-512`                   |
| `KAFKA_SECURITY_PROTOCOL`           |          | Default `SASL_SSL`                        |
| `ICEBERG_CATALOG_URL`               | ✅ in Iceberg mode | Lakekeeper REST URL and base URL for the table-writer adapter. Optional only when `AUDIT_SINK_JSONL_PATH` selects explicit JSONL dev mode |
| `ICEBERG_WAREHOUSE`                 |          | Warehouse location override               |
| `AUDIT_SINK_BATCH_MAX_RECORDS`      |          | Default 100k                              |
| `AUDIT_SINK_BATCH_MAX_WAIT_SECONDS` |          | Default 60s                               |
| `AUDIT_SINK_JSONL_PATH`             |          | Path to JSONL output file (`-` = stdout). When set, JSONLWriter is used instead of the Iceberg adapter |
| `METRICS_ADDR`                      |          | Default `0.0.0.0:9090`                    |
| `OTEL_TRACES_EXPORTER`              |          | `none` to disable tracing                 |
| `LOG_FORMAT`                        |          | `json` for production                     |

## Endpoints

- `GET /healthz` — liveness payload (Rust-compatible).
- `GET /metrics` — Prometheus scrape (`audit_sink_lag_seconds`,
  `audit_sink_records_total`, `audit_sink_batch_size_records`,
  `audit_sink_commits_total{outcome=success|failure|poison}`).

## Failure mode

The Iceberg adapter returns typed errors so the runtime fails loud and leaves
Kafka offsets uncommitted for replay:

- `ErrTableNotFound` for adapter `404` responses.
- `ErrSchemaMismatch` for adapter `409` / `422` responses.
- `ErrCommitFailed` for network failures and other non-2xx responses.

Adapter response bodies are preserved in the wrapped error (bounded to 4 KiB)
for operator diagnostics. JSONL mode is intentionally opt-in; production should
leave `AUDIT_SINK_JSONL_PATH` unset so selecting Iceberg cannot fall back to a
dev file sink. See `openfoundry-go/docs/migration/audit-sink-iceberg.md` for
the Rust contract review and cutover details.

## Build / run

```sh
make build-services
KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
ICEBERG_CATALOG_URL=http://localhost:8181 \
OTEL_TRACES_EXPORTER=none \
./bin/audit-sink

# Dev/staging JSONL fallback:
AUDIT_SINK_JSONL_PATH=/var/log/audit.jsonl ./bin/audit-sink
```
