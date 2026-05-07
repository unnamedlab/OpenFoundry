# `sql-bi-gateway-service` (Go)

Edge SQL gateway: ports the Rust crate of the same name with a
substantial caveat — Go has no first-class DataFusion equivalent, so
the Flight SQL gRPC surface is **substrate-only** until the proxy
bindings land. The HTTP side router (saved queries, warehousing,
tabular) is a 1:1 port.

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
| `ALLOW_ANONYMOUS` | `false` (set to `true` for local dev only) |
