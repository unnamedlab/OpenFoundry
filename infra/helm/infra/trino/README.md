# Trino on K8s (S5.6)

[Trino](https://trino.io) (Apache-2.0) running in `trino` namespace.
Used as the **analytical** engine over the Iceberg lakehouse hosted by
Lakekeeper. Reachable as `trino.trino.svc:8080`.

## Topology

| Role | Replicas | CPU req/lim | Mem req/lim |
|------|----------|-------------|-------------|
| coordinator (HA pair) | 2 | 2 / 8 | 8Gi / 32Gi |
| worker (HPA) | 4 → 16 | 4 / 16 | 16Gi / 64Gi |

Two coordinators with a service-fronted leader election keep
read-only DDL (CREATE OR REPLACE VIEW from `views/`) survivable across
zone failure. Workers scale on `trino_worker_active_queries > 4` per
worker for 10 minutes.

## Iceberg connector

Connector config in [`connectors/iceberg.properties`](connectors/iceberg.properties).
Points at Lakekeeper's REST catalog. Object storage is Ceph RGW —
no Hive metastore involved.

## Views

The `views/` directory carries idempotent `CREATE OR REPLACE VIEW`
DDLs. A bootstrap Job runs `for f in *.sql; do trino --execute @$f;
done` after the Trino chart's first successful rollout. Naming and
conventions are documented in [`views/README.md`](views/README.md).

## sql-bi-gateway routing

Once Trino is up the gateway picks it up automatically: any Flight
SQL statement whose first table reference is prefixed `trino.` is
routed there (see `services/sql-bi-gateway-service/src/routing.rs`).
OLTP statements (`postgres.*`, `cassandra.*`) keep going to their
existing backends per ADR-0014; the **analytical** OLAP slice (joins
across `iceberg.*` of larger than ~1M rows, time-windowed aggregates)
is now Trino's job. ADR-0029 owns the policy change.

## Apache-2.0

Trino is Apache-2.0; chart `trinodb/trino` is Apache-2.0. We pin
`server.image` to a specific Trino release tag in `values.yaml`.
