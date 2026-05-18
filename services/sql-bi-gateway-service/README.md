# `sql-bi-gateway-service`

## LLM quick context (current code)

Owns SQL/BI gateway surfaces: saved queries, tabular jobs/results, warehouse jobs/transformations/artifacts, FlightSQL, and Postgres wire config.

Agent note: bridges query UX to Iceberg/catalog and wire-protocol backends.

Current surface:
- `/api/v1/queries/saved*`
- `/api/v1/tabular/jobs*`
- `/api/v1/warehouse/jobs|transformations|artifacts*`
- `FlightSQL/Postgres wire listeners by config`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- No SQL migration files live under this service directory.
- Main internal packages: `audit`, `auth`, `catalog`, `config`, `flightsql`, `handler`, `models`, `routing`, `server`, `tabular`, `warehousing`, `wire`.
- Local service files present: `config.yaml`, `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `ALLOW_ANONYMOUS`, `DATABASE_URL`, `HEALTHZ_PORT`, `HOST`, `ICEBERG_CATALOG_URL`, `JWT_SECRET`, `PORT`, `POSTGRES_FLIGHT_SQL_URL`
- `POSTGRES_WIRE_PORT`, `SERVICE_VERSION`, `TRINO_FLIGHT_SQL_URL`, `VESPA_FLIGHT_SQL_URL`, `WAREHOUSING_FLIGHT_SQL_URL`

Keep this section in sync when changing routes, config, or persistence behavior.

Edge SQL gateway. Caveat: the Flight SQL gRPC surface is
**substrate-only** today — a literal-SELECT evaluator answers BI
client probes, anything richer is delegated to the configured
warehousing endpoint. The HTTP side router (saved queries,
warehousing, tabular) is fully wired.

## Surfaces

| Port | Protocol | Status |
|---|---|---|
| 50133 | Arrow Flight SQL gRPC | 🟡 substrate only — TCP listener accepts and logs (see [`internal/flightsql`](internal/flightsql/server.go)) |
| 50134 | HTTP REST | ✅ saved-queries, warehousing, tabular routes mounted (handler bodies stubbed where the kernel slice is missing) |

## Routing model

`internal/routing` ports `routing.rs` 1:1: catalog-prefix dispatch
(`trino.<...>`, `vespa.<...>`, `postgres.<...>`, otherwise local
DataFusion or `sql-warehousing-service`). Statements that target an
unconfigured backend fail with a typed `ErrBackendUnavailable` —
matching the Rust `RoutingError::BackendUnavailable` shape.

## Audit

`internal/audit` ports `audit.rs` 1:1, including the FNV-1a-64 SQL
fingerprint that lets us log SQL identity without leaking content.
The unit tests assert byte-for-byte parity with the Rust output.

## Build & run

```sh
go build -o bin/sql-bi-gateway-service ./services/sql-bi-gateway-service/cmd/sql-bi-gateway-service
go test ./services/sql-bi-gateway-service/...
```

## Configuration

| Variable | Default |
|---|---|
| `HOST` | `0.0.0.0` |
| `PORT` | `50133` (Flight SQL) |
| `HEALTHZ_PORT` | `50134` (HTTP side router) |
| `DATABASE_URL` | unset (saved-queries CRUD becomes stub) |
| `JWT_SECRET` | (required for non-anonymous mode) |
| `WAREHOUSING_FLIGHT_SQL_URL` | unset |
| `VESPA_FLIGHT_SQL_URL` | unset |
| `POSTGRES_FLIGHT_SQL_URL` | unset |
| `TRINO_FLIGHT_SQL_URL` | unset |
| `ICEBERG_CATALOG_URL` | unset — when set, `GetSchemas` / `GetTables` proxy to iceberg-catalog-service and forward the BI client's bearer token |
| `ALLOW_ANONYMOUS` | `false` (set to `true` for local dev only) |
