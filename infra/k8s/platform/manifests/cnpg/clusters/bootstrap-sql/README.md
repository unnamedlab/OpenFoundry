# Shared bootstrap SQL fragments

CloudNativePG `bootstrap.initdb.postInitApplicationSQLRefs.configMapRefs`
is a **list** — entries are concatenated and applied in order, exactly
once, after `initdb` finishes and before the cluster accepts client
connections. We exploit that to factor the bits that every CNPG cluster
needs (the `outbox.*` schema and the shared `debezium_cdc` role) into
two reusable ConfigMaps in this directory, and keep only the
cluster-specific work (per-bounded-context `svc_<bc>` roles, schema
ownership, and any cluster-private grants) in the
`pg-<cluster>-bootstrap-sql.yaml` ConfigMap that lives next to the
cluster manifest.

## Shared fragments

| ConfigMap                          | Purpose                                                               | Source file                              |
| ---------------------------------- | --------------------------------------------------------------------- | ---------------------------------------- |
| `cnpg-common-outbox-schema`        | `CREATE SCHEMA outbox` + `outbox.events` + `outbox.heartbeat` + REPLICA IDENTITY FULL | [`_common-outbox-schema.yaml`](_common-outbox-schema.yaml)               |
| `cnpg-common-debezium-cdc-role`    | `CREATE ROLE debezium_cdc IF NOT EXISTS` (LOGIN, REPLICATION) + grants on `outbox.*` | [`_common-debezium-cdc-role.yaml`](_common-debezium-cdc-role.yaml)       |

## Composition order (every cluster)

```yaml
bootstrap:
  initdb:
    postInitApplicationSQLRefs:
      configMapRefs:
        - { name: cnpg-common-outbox-schema,     key: bootstrap.sql }  # 1
        - { name: cnpg-common-debezium-cdc-role, key: bootstrap.sql }  # 2
        - { name: pg-<cluster>-bootstrap-sql,    key: bootstrap.sql }  # 3
```

1. `outbox.events` and `outbox.heartbeat` exist.
2. `debezium_cdc` role exists and can read the outbox tables.
3. Per-bounded-context `svc_<bc>` roles and schemas exist; each
   `svc_<bc>` is granted `INSERT/SELECT/DELETE` on `outbox.events` so
   the service's outbox publisher can enqueue events. Any extra
   cluster-private wiring (e.g. `pg-policy` granting Debezium SELECT on
   every audit schema) is appended to step 3.

The cluster-specific fragment relies on the schema and role created in
steps 1 and 2 — never reorder.

## Adding a new CNPG cluster

1. Create `pg-<cluster>.yaml` with the three `configMapRefs` above.
2. Create `pg-<cluster>-bootstrap-sql.yaml` with **only** the per-BC
   role/schema loop and the outbox-svc grant loop. Do not redeclare the
   `outbox.*` schema or the `debezium_cdc` role.

## Why not a generator / Helm chart?

Two reusable ConfigMaps cover the only two blocks that were
byte-identical across all clusters. The remaining variation
(BC list, role flags such as `REPLICATION`, cluster-private grants)
stays inline in plain YAML, keeping the bootstrap path readable and
diff-friendly with no build step.
