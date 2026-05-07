# identity-federation-service (Go)

Identity & federation: register, login, JWT issuance, refresh-token
rotation. **Slice 1 of 8** in the migration plan documented at
[`openfoundry-go/INVENTORY-identity-federation-service.md`](../../INVENTORY-identity-federation-service.md).

## Implemented surface

In addition to the original auth bootstrap/login endpoints, the Go service now
contains the identity-admin surfaces from later slices, including RBAC, API keys,
SCIM, and restricted views.

### Auth bootstrap/login

Endpoints (all under `/api/v1/auth`, no bearer required):

| Method | Path                       | Purpose                                |
| ------ | -------------------------- | -------------------------------------- |
| GET    | `/bootstrap-status`        | Returns whether the first-admin path is still open |
| POST   | `/register`                | Argon2id-hashed user creation; first user becomes admin |
| POST   | `/login`                   | Password verification → access JWT + refresh token (+MFA stub for slice 3) |
| POST   | `/token/refresh`           | One-time exchange + family-replay detection |
| GET    | `/healthz`                 | liveness                               |
| GET    | `/metrics`                 | Prometheus                             |

Schema (slice 1 only — wider schema lands in later slices):
- `users` (id, email, name, password_hash, is_active, auth_source, mfa_enforced, organization_id, attributes, created_at, updated_at)
- `roles` (id, name, description, created_at) — seeded with admin / editor / viewer
- `user_roles` (user_id, role_id, assigned_at)
- `refresh_tokens` (id, user_id, token_hash, family_id, issued_at, expires_at, revoked_at)

Refresh tokens are stored in **Postgres** for slice 1; slice 2 ports
the Rust crate's Cassandra-backed `auth_runtime.refresh_tokens` table.

## Slice 2 status (this commit)

`libs/cassandra-kernel` (gocql + idempotent migration applier) and
`internal/sessionscassandra/` (user_session + refresh_token DDL +
adapter, auth_runtime keyspace, ported verbatim from Rust) are
**scaffolded but NOT yet wired**: the active `Issuer` still keeps
refresh tokens in Postgres. Flipping the active backend is a one-
line swap in `cmd/.../main.go`; landing it requires a Cassandra /
Scylla instance in the dev environment so the binary smoke-tests
against real CQL. Both backends sit side-by-side until then.

## Remaining follow-up work

- Cassandra-backed sessions are scaffolded but not the active refresh-token backend.
- Control panel + scoped-session governance + ABAC administration still need a
  dedicated current-state audit. Restricted-view CRUD is implemented here and
  consumed by authorization-policy-service evaluations.
- Vault Transit signing integration remains deferred.
- Rate limiting (the Rust crate uses Redis per-(user,IP)) remains deferred.
- Audit publishing to Kafka `audit.identity.v1` remains deferred.

## Restricted views ownership

`identity-federation-service` is the canonical Go owner for restricted-view CRUD
(`GET|POST /api/v1/restricted-views`, `GET|PUT|PATCH|DELETE
/api/v1/restricted-views/{id}`). This consolidates the Rust
`authorization-policy-service` CRUD handler with identity-side CBAC/session
configuration so SCIM groups, scoped sessions, and identity claims share one
source of truth. `authorization-policy-service` remains the evaluator: its
`POST /api/v1/policy-evaluations` endpoint reads enabled `restricted_views` rows
and applies their row filters, hidden columns, allowed org IDs, allowed markings,
guest-access, and consumer-mode flags.

## Wire-format invariants preserved

- `User` JSON (snake_case fields, password_hash never emitted)
- `LoginResponse` discriminated union (`status: "authenticated" | "mfa_required"`)
- `TokenResponse` (access_token, refresh_token, token_type, expires_in)
- Argon2id PHC encoding (`$argon2id$v=19$m=...,t=...,p=...$<salt>$<hash>`) so a hash issued by either implementation validates against the other — critical for the cutover.
- JWT claims via `libs/auth-middleware` (already locked).

## Configuration

| Variable                             | Required | Purpose                              |
| ------------------------------------ | :------: | ------------------------------------ |
| `DATABASE_URL`                       | ✅       | Postgres connection string           |
| `JWT_SECRET` (or `OPENFOUNDRY_JWT_SECRET`) | ✅ | HS256 secret                |
| `HOST` / `PORT`                      |          | default `0.0.0.0:50112`              |
| `ACCESS_TOKEN_TTL`                   |          | default `1h`                         |
| `REFRESH_TOKEN_TTL`                  |          | default `168h` (7 days)              |
| `METRICS_ADDR`                       |          | default `0.0.0.0:9090`               |
| `OTEL_TRACES_EXPORTER=none`          |          | disable tracing                      |

## Build / run

```sh
make build-services
DATABASE_URL=postgres://localhost/identity JWT_SECRET=$(openssl rand -hex 32) \
OTEL_TRACES_EXPORTER=none ./bin/identity-federation-service
```
