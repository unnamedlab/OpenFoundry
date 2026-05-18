# telemetry-governance-service (Go)

## LLM quick context (current code)

Owns telemetry governance and monitoring/health-check/streaming-monitor surfaces.

Agent note: generic feature handlers are mounted under /api/v1/<feature>; monitor-rules/views are explicit.

Current surface:
- `/api/v1/health-checks*`
- `/api/v1/monitoring-views*`
- `/api/v1/monitor-rules*`
- `/api/v1/streaming-monitors*`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `5` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `handlers`, `models`, `repo`, `server`, `streamingmonitors`.
- Local service files present: `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `METRICS_ADDR`, `OPENFOUNDRY_JWT_SECRET`, `PORT`, `SERVICE_VERSION`

Keep this section in sync when changing routes, config, or persistence behavior.

Telemetry permissions, log/metric/event export and governance policies.
Hosts four foundation CRUDs (parent + child collections) under one
binary, consolidated per ADR-0030 (S8.1.a).

> **Scope of this iteration**: the four foundation features below are
> ported. The streaming-monitor / monitor-rule / evaluator surface
> (~1000 LOC of Rust under `monitoring_rules/streaming_*` and
> `monitoring_rules/evaluator.rs`) is **deferred** to a follow-up
> iteration — see TODO at the bottom.

## Endpoints

For each feature `<f>` mounted under `/api/v1/<f>`:

| Method | Path                              | Purpose          |
| ------ | --------------------------------- | ---------------- |
| GET    | `/api/v1/<f>`                     | list (limit 200) |
| POST   | `/api/v1/<f>`                     | create parent    |
| GET    | `/api/v1/<f>/{id}`                | fetch by id      |
| GET    | `/api/v1/<f>/{id}/<children>`     | list children    |
| POST   | `/api/v1/<f>/{id}/<children>`     | create child     |

The four feature triplets (canonical order — pinned in `models.AllFeatures`):

| `<f>`              | parent table       | child table              | `<children>` |
| ------------------ | ------------------ | ------------------------ | ------------ |
| `telemetry-exports`| telemetry_exports  | telemetry_policies       | policies     |
| `health-checks`    | health_checks      | health_check_results     | results      |
| `execution-runs`   | execution_runs     | execution_logs           | logs         |
| `monitoring-rules` | monitoring_rules   | monitoring_subscribers   | subscribers  |

Plus `GET /healthz` (Rust-compatible liveness payload) and
`GET /metrics` (Prometheus). All `/api/v1/*` routes are bearer-JWT
protected.

## Schema

Four migrations embedded under `internal/repo/migrations/`. Idempotent
DDL (`CREATE TABLE IF NOT EXISTS`) so re-running on a populated DB is
safe.

## Configuration

| Variable                       | Required | Purpose                              |
| ------------------------------ | :------: | ------------------------------------ |
| `DATABASE_URL`                 | ✅       | Postgres connection string           |
| `JWT_SECRET` (or `OPENFOUNDRY_JWT_SECRET`) | ✅ | HS256 secret                |
| `HOST` / `PORT`                |          | default `0.0.0.0:50153`              |
| `METRICS_ADDR`                 |          | default `0.0.0.0:9090`               |
| `OTEL_TRACES_EXPORTER=none`    |          | disable tracing                      |

## Build / run

```sh
make build-services
DATABASE_URL=postgres://localhost/telemetry JWT_SECRET=$(openssl rand -hex 32) \
OTEL_TRACES_EXPORTER=none ./bin/telemetry-governance-service
```

## TODO — streaming monitor surface (deferred)

The streaming monitor surface (`monitoring_rules/streaming_*`, ~1000 LOC)
lands in a follow-up iteration. Surface it adds:

- Tables: `monitoring_views`, `monitor_rules`, `monitor_evaluations`
  (typed schema, not the generic payload-jsonb shape).
- Endpoints: monitor view CRUD, monitor rule CRUD with the typed
  `resource_type` / `monitor_kind` / `comparator` enums, evaluation
  history queries.
- Logic: `monitoring_rules/evaluator.rs` (355 LOC) — pure scheduler
  arithmetic comparing observed values against `threshold` over
  `window_seconds` and emitting alerts to `notification-alerting-service`.

The migration `20260504000004_streaming_monitors.sql` is intentionally
not yet copied to `internal/repo/migrations/` to keep the schema scope
of this iteration limited.
