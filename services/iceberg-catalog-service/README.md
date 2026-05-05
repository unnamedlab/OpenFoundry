# iceberg-catalog-service

Foundry-flavoured implementation of the [Apache Iceberg REST Catalog
specification][rest-catalog-spec]. The service runs alongside the rest
of the OpenFoundry stack and exposes the catalog endpoints external
Iceberg clients (PyIceberg, Spark, Trino, Snowflake) consume.

[rest-catalog-spec]: https://github.com/apache/iceberg/blob/main/open-api/rest-catalog-open-api.yaml

## Status — D1.1.8 4/5 (Beta)

| Phase | Scope                                            | State    |
|-------|--------------------------------------------------|----------|
| P1    | Service skeleton + spec endpoints + auth surface | ✅ done   |
| P2    | Foundry-pattern transactions (all-or-nothing)    | ✅ done   |
| P3    | Markings + Cedar enforcement + diagnose          | ✅ done   |
| P4    | Compaction + maintenance worker                  | ⏳ pending |
| P5    | BYOB + at-rest encryption                        | ⏳ pending |
| P6    | Credential vending + 3rd-party catalogs          | ⏳ pending |

## Endpoints

The full Apache Iceberg REST Catalog spec surface is implemented under
`/iceberg/v1/...`. The Foundry admin UI talks to a parallel set of
endpoints under `/api/v1/iceberg-tables/...` (see
[`src/handlers/admin.rs`](./src/handlers/admin.rs)).

### Spec endpoints (`/iceberg/v1`)

| Method | Path                                                              | Purpose                          |
|--------|-------------------------------------------------------------------|----------------------------------|
| GET    | `/config`                                                         | Catalog defaults                 |
| GET    | `/namespaces`                                                     | List namespaces                  |
| POST   | `/namespaces`                                                     | Create namespace                 |
| GET    | `/namespaces/{ns}`                                                | Load namespace                   |
| DELETE | `/namespaces/{ns}`                                                | Drop namespace                   |
| GET/POST | `/namespaces/{ns}/properties`                                   | Properties CRUD                  |
| GET    | `/namespaces/{ns}/tables`                                         | List tables                      |
| POST   | `/namespaces/{ns}/tables`                                         | Create table                     |
| GET    | `/namespaces/{ns}/tables/{tbl}`                                   | Load table (config + metadata)   |
| HEAD   | `/namespaces/{ns}/tables/{tbl}`                                   | Existence check                  |
| POST   | `/namespaces/{ns}/tables/{tbl}`                                   | Commit table                     |
| DELETE | `/namespaces/{ns}/tables/{tbl}?purgeRequested=`                   | Drop table                       |
| POST   | `/namespaces/{ns}/tables/{tbl}/alter-schema`                      | **P2** explicit schema mutation  |
| POST   | `/transactions/commit`                                            | **P2** atomic multi-table commit |
| GET    | `/namespaces/{ns}/markings`                                       | **P3** namespace markings        |
| POST   | `/namespaces/{ns}/markings`                                       | **P3** replace ns markings       |
| GET    | `/namespaces/{ns}/tables/{tbl}/markings`                          | **P3** table markings (3 buckets)|
| PATCH  | `/namespaces/{ns}/tables/{tbl}/markings`                          | **P3** explicit override         |
| POST   | `/diagnose`                                                       | **P3** connection diagnostic     |
| POST   | `/oauth/tokens`                                                   | OAuth2 token endpoint            |

### Auth

* **Bearer / API tokens** — long-lived `ofty_*` tokens minted via
  `POST /v1/iceberg-clients/api-tokens`. Stored as SHA-256 hashes.
* **OAuth2 client credentials** — minted via
  `POST /iceberg/v1/oauth/tokens`. The catalog validates the client
  pair against `oauth-integration-service`.

Both flows surface `api:iceberg-read` and `api:iceberg-write` scopes
that the bearer extractor maps onto Cedar entity attributes (P3).

## Markings (P3)

Markings on Iceberg resources mirror the dataset model (D1.1.4 P4):

* `iceberg_namespace_markings` — explicit markings on the namespace.
* `iceberg_table_markings` — per-table markings split into
  `inherited_from_namespace` (snapshot at create time) and `explicit`.
* `iceberg_marking_names` — id ↔ name projection seeded with the
  default ladder (`public`, `confidential`, `pii`, `restricted`,
  `secret`).

Inheritance follows Foundry **snapshot semantics**: a namespace
marking change does **not** retroactively propagate to existing
tables. Re-applying the snapshot is a `manage_markings` operation on
the table.

Cedar policies are bundled by `authz_cedar::iceberg_policies` and live
in [`libs/authz-cedar/src/iceberg_policies.rs`](../../libs/authz-cedar/src/iceberg_policies.rs).
The schema entities (`IcebergNamespace`, `IcebergTable`) are defined
in [`libs/authz-cedar/cedar_schema.cedarschema`](../../libs/authz-cedar/cedar_schema.cedarschema).

## Configuration

| Variable                                  | Purpose                                          |
|-------------------------------------------|--------------------------------------------------|
| `DATABASE_URL`                            | Postgres URL (required)                          |
| `ICEBERG_CATALOG_HOST` / `_PORT`          | Bind address (default `0.0.0.0:8197`)            |
| `ICEBERG_CATALOG_WAREHOUSE_URI`           | URI returned in `/iceberg/v1/config`             |
| `ICEBERG_CATALOG_DEFAULT_TENANT`          | Default tenant for principals (default `default`) |
| `IDENTITY_FEDERATION_URL`                 | Identity service base URL                        |
| `OAUTH_INTEGRATION_URL`                   | OAuth integration base URL                       |
| `OPENFOUNDRY_JWT_SECRET` / `JWT_SECRET`   | Shared HMAC secret                               |

## Running locally

```bash
docker compose -f docker-compose.dev.yml up -d \
    postgres minio identity-federation-service \
    oauth-integration-service iceberg-catalog-service
```

## Tests

```bash
# 1. Unit + Rust contract tests (no Docker)
cargo test -p iceberg-catalog-service --lib

# 2. Docker-bound integration suite
cargo test -p iceberg-catalog-service --include-ignored

# 3. PyIceberg integration suite
ICEBERG_CATALOG_URL=http://localhost:8197 \
  python -m pytest tests/integration/pyiceberg/
```

CI gates: see [`.github/workflows/iceberg-integration.yml`](../../.github/workflows/iceberg-integration.yml).
