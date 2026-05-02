# CNPG Poolers — PgBouncer in transaction mode

Each consolidated CNPG cluster (S6.1.b) is fronted by a CNPG `Pooler`
CRD that the operator renders into a PgBouncer Deployment + Service:

| Cluster              | Pooler Service (rw)                                                    | Pool size / svc | Mode        |
| -------------------- | ---------------------------------------------------------------------- | --------------- | ----------- |
| `pg-schemas`         | `pg-schemas-pooler-rw.openfoundry.svc.cluster.local:5432`              | 50              | transaction |
| `pg-policy`          | `pg-policy-pooler-rw.openfoundry.svc.cluster.local:5432`               | 50              | transaction |
| `pg-runtime-config`  | `pg-runtime-config-pooler-rw.openfoundry.svc.cluster.local:5432`       | 50              | transaction |
| `pg-lakekeeper`      | _(no pooler — Lakekeeper holds a small fixed pool itself)_             | n/a             | n/a         |

Application services connect through the Pooler service via
`DATABASE_URL` (writer) and through the underlying CNPG `<cluster>-ro`
service via `DATABASE_READ_URL` (reader); see ADR-0010 and the dual-pool
helper in [`libs/db-pool`](../../../../libs/db-pool/).

Transaction-mode constraints — services hosted here MUST NOT rely on:

* session-scoped advisory locks,
* `SET LOCAL` outside an explicit `BEGIN/COMMIT`,
* `LISTEN/NOTIFY`,
* server-side prepared statements without `prepared_statements=false`
  in the sqlx connect options.

This is the same contract that `cnpg-kernel` documents for the legacy
per-service Poolers; the only change is that the back-end is now one
of four consolidated clusters instead of one cluster per bounded
context.
