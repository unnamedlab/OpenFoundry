# ai-sink (Go)

Kafka → Iceberg consumer for `ai.events.v1`. Batches `AiEventEnvelope`
records (default 100k OR 60s) and routes each one to one of four
Iceberg tables under `lakekeeper/of_ai/`:

| `kind` token   | Iceberg table |
| -------------- | ------------- |
| `prompt`       | `prompts`     |
| `response`     | `responses`   |
| `evaluation`   | `evaluations` |
| `trace`        | `traces`      |

Same architecture as `audit-sink`; the only difference is the per-table
routing inside `internal/runtime`. The default writer is the productive
OpenFoundry Iceberg table-writer adapter and JSONL is only an explicit
dev/staging fallback. The adapter is used because `apache/iceberg-go`
still lacks a stable end-to-end write API matching Rust's
`append_record_batches`; it writes one durable append per non-empty table
group and reports typed errors for empty batches, missing tables, schema
mismatches, and commit failures.

The Go writer sends the adapter both the Lakekeeper REST catalog URL and
the Iceberg table spec (`lakekeeper.of_ai.<table>`). The adapter is
responsible for loading the Lakekeeper table, writing Parquet data files,
and committing the Iceberg snapshot atomically before the sink commits
Kafka offsets.

## Configuration

Identical to `audit-sink` but with AI-specific knobs:

| Variable                          | Required | Purpose                                |
| --------------------------------- | :------: | -------------------------------------- |
| `KAFKA_BOOTSTRAP_SERVERS`         | ✅       | broker list                            |
| `ICEBERG_CATALOG_URL`             | ✅*      | Lakekeeper REST catalog URL used by the table-writer adapter; required unless `AI_SINK_JSONL_DIR` selects JSONL dev mode |
| `AI_SINK_TABLE_WRITER_URL`         |          | OpenFoundry HTTP table-writer adapter URL. Defaults to `ICEBERG_TABLE_WRITER_URL`, then `ICEBERG_CATALOG_URL` for co-located/proxied deployments |
| `ICEBERG_TABLE_WRITER_URL`         |          | Backward-compatible alias for `AI_SINK_TABLE_WRITER_URL` |
| `ICEBERG_WAREHOUSE`                |          | Optional Lakekeeper warehouse identifier passed through to the adapter |
| `AI_SINK_BATCH_MAX_RECORDS`       |          | Default 100k                           |
| `AI_SINK_BATCH_MAX_WAIT_SECONDS`  |          | Default 60s                            |
| `AI_SINK_JSONL_DIR`               |          | Directory for one `<table>.jsonl` per Iceberg table; selects the JSONL fallback when set |
| `METRICS_ADDR`                    |          | Default `0.0.0.0:9090`                 |

Plus the same `KAFKA_SASL_*` set documented in `audit-sink/README.md`.

## Endpoints

- `GET /healthz` — liveness payload (Rust-compatible).
- `GET /metrics` — Prometheus scrape; metrics labelled by table:
  - `ai_sink_lag_seconds{table=...}`
  - `ai_sink_records_total{table=...}`
  - `ai_sink_batch_size_records{table=...}`
  - `ai_sink_commits_total{table=...,outcome=success|failure|poison}`

## Build / run

```sh
make build-services
KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
ICEBERG_CATALOG_URL=http://localhost:8181 \
AI_SINK_TABLE_WRITER_URL=http://localhost:8088 \
ICEBERG_WAREHOUSE=local \
OTEL_TRACES_EXPORTER=none \
./bin/ai-sink

# Dev/staging JSONL fallback:
mkdir -p /var/log/ai-sink
KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
AI_SINK_JSONL_DIR=/var/log/ai-sink \
OTEL_TRACES_EXPORTER=none \
./bin/ai-sink
```


## Iceberg contract

The sink targets exactly these Lakekeeper tables:

- `lakekeeper.of_ai.prompts`
- `lakekeeper.of_ai.responses`
- `lakekeeper.of_ai.evaluations`
- `lakekeeper.of_ai.traces`

All four tables use the Rust-compatible schema: `event_id`, `at`,
`kind`, `run_id`, `trace_id`, `producer`, `schema_version`, and
`payload`, with field IDs 1 through 8, partition transform `day(at)`,
and sort order `at ASC`. The writer appends only non-empty per-table
groups and returns successfully only after the adapter reports a durable
Iceberg commit.
