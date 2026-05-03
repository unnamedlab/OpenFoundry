# CNPG `Cluster` manifests — consolidated topology

Four CloudNativePG `Cluster` CRs back the entire Postgres workload of
OpenFoundry (S6.1.b of the Cassandra/Foundry parity migration plan).
Per-bounded-context clusters were retired in S6.1.a; the runbook for
removing the leftover CRs/Secrets/PVCs in any pre-prod environment
lives at [`../../../../../runbooks/cnpg-decommission.md`](../../../../../runbooks/cnpg-decommission.md).

| Manifest                                               | Role                                          | Schemas | Notes                                          |
| ------------------------------------------------------ | --------------------------------------------- | ------- | ---------------------------------------------- |
| [`pg-schemas.yaml`](pg-schemas.yaml)                   | Catalogs / registries / definitions           | 21      | `wal_level=replica`                            |
| [`pg-policy.yaml`](pg-policy.yaml)                     | Security / policy / audit / governance        | 12      | `wal_level=logical`, Debezium CDC source       |
| [`pg-runtime-config.yaml`](pg-runtime-config.yaml)     | Runtime / control-plane / observability state | 25      | Largest cluster (500Gi, 12 CPU limit)          |
| [`pg-lakekeeper.yaml`](pg-lakekeeper.yaml)             | Lakekeeper Iceberg REST catalog metadata only | n/a     | Single-tenant DB `lakekeeper`, no pooler       |

`connector_management` e `ingestion_replication` viven en `pg-runtime-config`
porque son bounded contexts de control-plane/runtime; no forman parte del
catálogo declarativo de `pg-schemas`.

Each cluster:

* runs **3 instances** with `minSyncReplicas=maxSyncReplicas=1`
  (synchronous quorum of one),
* uses image `ghcr.io/cloudnative-pg/postgresql:16.4`,
* spreads pods across zones (`topology.kubernetes.io/zone` anti-affinity),
* exports metrics through `monitoring.enablePodMonitor: true`,
* backs WAL + base to Ceph RGW S3
  (`s3://openfoundry-pg-backups/<cluster>/`, 30-day retention).

## Bootstrap SQL (S6.1.c + S6.1.d)

Schema and per-service role provisioning is composed from three
ConfigMaps applied in order via
`bootstrap.initdb.postInitApplicationSQLRefs.configMapRefs` (CNPG
concatenates the list and runs it once, post-initdb):

1. [`bootstrap-sql/_common-outbox-schema.yaml`](bootstrap-sql/_common-outbox-schema.yaml) — `outbox.events` / `outbox.heartbeat` (shared across every cluster).
2. [`bootstrap-sql/_common-debezium-cdc-role.yaml`](bootstrap-sql/_common-debezium-cdc-role.yaml) — `CREATE ROLE debezium_cdc IF NOT EXISTS` + grants on `outbox.*` (shared).
3. The cluster-specific ConfigMap, one per cluster:
   * [`pg-schemas-bootstrap-sql.yaml`](pg-schemas-bootstrap-sql.yaml)
   * [`pg-policy-bootstrap-sql.yaml`](pg-policy-bootstrap-sql.yaml)
   * [`pg-runtime-config-bootstrap-sql.yaml`](pg-runtime-config-bootstrap-sql.yaml)

The cluster-specific fragment:

1. creates a `svc_<bc>` role (LOGIN, NOINHERIT, placeholder password —
   **the External Secrets Operator must rotate it** before the cluster
   accepts production traffic; `pg-policy` adds `REPLICATION` so its
   service roles can drive logical decoding),
2. creates schema `<bc>` `AUTHORIZATION svc_<bc>` so only the owning
   service can issue DDL,
3. revokes `public` on the schema and grants `USAGE` only to its owner
   role,
4. installs default privileges so every future table/sequence is
   reachable by `svc_<bc>` with `SELECT/INSERT/UPDATE/DELETE` only,
5. grants `INSERT/SELECT/DELETE` on `outbox.events` to every `svc_<bc>`
   so each service can enqueue domain events through the shared
   transactional outbox.

`pg-policy` additionally grants the shared `debezium_cdc` role
read-only access (with default privileges for future tables) on every
policy schema, so the Debezium connector can publish audit/policy
change events to Kafka.

See [`bootstrap-sql/README.md`](bootstrap-sql/README.md) for the full
composition contract and the rule for adding a new cluster.

## Pooler & application connectivity

Three CNPG `Pooler` CRDs (PgBouncer in transaction mode, 50 connections
per service) front the three multi-tenant clusters; see
[`../poolers/`](../poolers/). Application services consume the
resulting Pooler service through the `<bc>-db-dsn` Secret contract
documented at
[`../../../../helm/DATABASE_URL.md`](../../../../helm/DATABASE_URL.md),
and split writer/reader traffic via the
[`libs/db-pool`](../../../../../../libs/db-pool/) helper crate (S6.4).

## Helm template

The original Helm-rendered template at [`../templates/cluster.yaml`](../templates/cluster.yaml)
is preserved for any future stand-alone cluster (e.g. a tenant-specific
sandbox) but is **no longer applied** by the platform chart — the four
manifests above are the single source of truth.
