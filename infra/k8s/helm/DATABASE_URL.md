# `<bc>-db-dsn` Secret contract

S6.3 + S6.4 — every service that talks to one of the four consolidated
CNPG clusters consumes a Kubernetes Secret named `<bc>-db-dsn` (where
`<bc>` is the bounded context, kebab-case). The Secret is **not**
created by CNPG; it is sync-ed by the External Secrets Operator from
HashiCorp Vault (or sealed via SealedSecrets in dev) and contains
exactly two keys:

| Key          | Purpose                                          | Maps to env var       |
| ------------ | ------------------------------------------------ | --------------------- |
| `writer_url` | Pooler endpoint (transaction mode), search_path  | `DATABASE_URL`        |
| `reader_url` | CNPG `<cluster>-ro` endpoint, search_path        | `DATABASE_READ_URL`   |

Both URLs follow the contract enforced by [`libs/db-pool`](../../../libs/db-pool/src/lib.rs):

```text
postgresql://svc_<bc>:<password>@<cluster>-pooler-rw.openfoundry.svc.cluster.local:5432/app
    ?sslmode=require
    &options=-c%20search_path%3D<bc>
```

* `<bc>` in the URL is **snake_case** (Postgres identifier), not
  kebab-case — the bootstrap SQL ConfigMaps (S6.1.c/d) create
  schemas/roles using snake_case.
* The `options=-c search_path=<bc>` parameter is mandatory: without it,
  transaction-mode pooling would forbid `SET search_path` and the
  service would default to `public` (which is empty by design — see
  `REVOKE ALL ON SCHEMA public FROM PUBLIC` in the bootstrap SQL).
* `sslmode=require` is mandatory; CNPG ships TLS by default.

## Cluster ↔ schema mapping

The exhaustive routing table lives in the four bootstrap-SQL
ConfigMaps:

* [`pg-schemas-bootstrap-sql.yaml`](../platform/manifests/cnpg/clusters/pg-schemas-bootstrap-sql.yaml) — 21 schemas
* [`pg-policy-bootstrap-sql.yaml`](../platform/manifests/cnpg/clusters/pg-policy-bootstrap-sql.yaml) — 12 schemas + Debezium role
* [`pg-runtime-config-bootstrap-sql.yaml`](../platform/manifests/cnpg/clusters/pg-runtime-config-bootstrap-sql.yaml) — 25 schemas
* `pg-lakekeeper` — single tenant; database `lakekeeper`, no per-schema
  multi-tenancy, no `<bc>-db-dsn` Secret needed.

## Why a separate Secret instead of CNPG's `<cluster>-app`

The CNPG-managed `<cluster>-app` Secret holds the DSN of the cluster's
**superuser** (`app` role) on the `app` database without any
`search_path` and without going through the Pooler. That is the wrong
identity, host and connection string for any application service.

`<bc>-db-dsn` is therefore an out-of-band Secret built by the platform
team / External Secrets sync that:

1. uses the per-bounded-context `svc_<bc>` role (least privilege —
   only `USAGE` on its schema, no DDL),
2. targets the Pooler service for `writer_url` and the `<cluster>-ro`
   service for `reader_url`,
3. embeds the `search_path` query option so transaction-mode pooling
   never leaks across schemas.
