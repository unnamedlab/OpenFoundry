# CNPG `Cluster` manifests — consolidated topology

Four CloudNativePG `Cluster` CRs back the entire Postgres workload of
OpenFoundry (S6.1.b of the Cassandra/Foundry parity migration plan).
Per-bounded-context clusters were retired in S6.1.a; the runbook for
removing the leftover CRs/Secrets/PVCs in any pre-prod environment
lives at [`../../runbooks/cnpg-decommission.md`](../../runbooks/cnpg-decommission.md).

| Manifest                                               | Role                                          | Schemas | Notes                                          |
| ------------------------------------------------------ | --------------------------------------------- | ------- | ---------------------------------------------- |
| [`pg-schemas.yaml`](pg-schemas.yaml)                   | Catalogs / registries / definitions           | 23      | `wal_level=replica`                            |
| [`pg-policy.yaml`](pg-policy.yaml)                     | Security / policy / audit / governance        | 12      | `wal_level=logical`, Debezium CDC source       |
| [`pg-runtime-config.yaml`](pg-runtime-config.yaml)     | Runtime / control-plane / observability state | 23      | Largest cluster (500Gi, 12 CPU limit)          |
| [`pg-lakekeeper.yaml`](pg-lakekeeper.yaml)             | Lakekeeper Iceberg REST catalog metadata only | n/a     | Single-tenant DB `lakekeeper`, no pooler       |

Each cluster:

* runs **3 instances** with `minSyncReplicas=maxSyncReplicas=1`
  (synchronous quorum of one),
* uses image `ghcr.io/cloudnative-pg/postgresql:16.4`,
* spreads pods across zones (`topology.kubernetes.io/zone` anti-affinity),
* exports metrics through `monitoring.enablePodMonitor: true`,
* backs WAL + base to Ceph RGW S3
  (`s3://openfoundry-pg-backups/<cluster>/`, 30-day retention).

## Bootstrap SQL (S6.1.c + S6.1.d)

Schema and per-service role provisioning is delegated to ConfigMaps
referenced from `bootstrap.initdb.postInitApplicationSQLRefs`:

* [`pg-schemas-bootstrap-sql.yaml`](pg-schemas-bootstrap-sql.yaml)
* [`pg-policy-bootstrap-sql.yaml`](pg-policy-bootstrap-sql.yaml)
* [`pg-runtime-config-bootstrap-sql.yaml`](pg-runtime-config-bootstrap-sql.yaml)

Each bootstrap script:

1. creates a `svc_<bc>` role (LOGIN, NOINHERIT, placeholder password —
   **the External Secrets Operator must rotate it** before the cluster
   accepts production traffic),
2. creates schema `<bc>` `AUTHORIZATION svc_<bc>` so only the owning
   service can issue DDL,
3. revokes `public` on the schema and grants `USAGE` only to its owner
   role,
4. installs default privileges so every future table/sequence is
   reachable by `svc_<bc>` with `SELECT/INSERT/UPDATE/DELETE` only.

`pg-policy` additionally provisions a `debezium_cdc` REPLICATION role
with read-only access to all twelve policy schemas, used by the
Debezium connector that publishes audit/policy events to Kafka.

## Pooler & application connectivity

Three CNPG `Pooler` CRDs (PgBouncer in transaction mode, 50 connections
per service) front the three multi-tenant clusters; see
[`../poolers/`](../poolers/). Application services consume the
resulting Pooler service through the `<bc>-db-dsn` Secret contract
documented at
[`../../helm/open-foundry/DATABASE_URL.md`](../../helm/open-foundry/DATABASE_URL.md),
and split writer/reader traffic via the
[`libs/db-pool`](../../../../libs/db-pool/) helper crate (S6.4).

## Helm template

The original Helm-rendered template at [`../templates/cluster.yaml`](../templates/cluster.yaml)
is preserved for any future stand-alone cluster (e.g. a tenant-specific
sandbox) but is **no longer applied** by the platform chart — the four
manifests above are the single source of truth.
