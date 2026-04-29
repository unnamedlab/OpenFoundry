# Trino — edge BI gateway

These manifests deploy [Trino](https://trino.io) (Apache-2.0) as the
**edge BI gateway** for OpenFoundry: a stable federated SQL endpoint
exposed to **external JDBC/ODBC clients** (Tableau, Superset, ad-hoc SQL
notebooks, third-party BI tools) that need to span Iceberg/Polaris,
PostgreSQL/CNPG, Kafka and ClickHouse through a single ANSI-SQL surface.

> **Role:** edge BI gateway only. Trino sits **outside** the platform's
> internal fan-out and is **not** the internal query hub. All
> service-to-service SQL inside the platform runs over **Flight SQL P2P**
> via `libs/query-engine/` and `services/sql-warehousing-service`
> (port 50123), routed at the edge by `services/sql-bi-gateway-service`
> (port 50133). New services must not declare a runtime dependency on
> Trino for internal queries — Trino is reserved for external BI clients
> that cannot speak Flight SQL.
>
> See
> [`docs/architecture/adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md`](../../../docs/architecture/adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md).

## Files

| File                                  | Purpose                                                          |
|---------------------------------------|------------------------------------------------------------------|
| `values.yaml`                         | Values for the upstream `trino/trino` Helm chart                 |
| `iceberg-catalog-configmap.yaml`      | `iceberg.properties` — REST catalog (Polaris) over Ceph S3       |
| `postgresql-catalog-configmap.yaml`   | `postgresql.properties` — CloudNativePG primary                  |
| `kafka-catalog-configmap.yaml`        | `kafka.properties` — read-only, troubleshooting only             |
| `coordinator-pdb.yaml`                | `PodDisruptionBudget` (`minAvailable: 1`) for the coordinators   |

The Trino chart itself is **not** vendored here. It is consumed straight from
the upstream repository:

```bash
helm repo add trino https://trinodb.github.io/charts/
helm repo update
```

## Apply order

```bash
# 0. Namespace and shared secrets (see runbook for secret payload).
kubectl create namespace trino
kubectl -n trino apply -f - <<'YAML'
# trino-internal-shared-secret, trino-s3-iceberg, trino-postgres-credentials,
# trino-polaris-oauth — see infra/runbooks/trino.md §3.
YAML

# 1. Catalog ConfigMaps (must exist before pods start; mounted as volumes).
kubectl -n trino apply -f iceberg-catalog-configmap.yaml
kubectl -n trino apply -f postgresql-catalog-configmap.yaml
kubectl -n trino apply -f kafka-catalog-configmap.yaml

# 2. Coordinator PodDisruptionBudget.
kubectl -n trino apply -f coordinator-pdb.yaml

# 3. Trino itself.
helm upgrade --install trino trino/trino \
  -n trino \
  -f values.yaml
```

## What's in `values.yaml`

* `coordinator.replicas: 2` with experimental coordinator HA + leader
  election (Trino ≥ 425). Anti-affinity spreads them across nodes.
* `server.workers: 6` (also `worker.replicas: 6`) — adjust per workload.
  Workers are stateless and safe to scale via `helm upgrade --set
  server.workers=N`.
* Both pod kinds are annotated with `linkerd.io/inject: enabled` so all
  pod-to-pod traffic (coordinator↔worker, coordinator↔coordinator) is
  authenticated and encrypted by Linkerd mTLS without configuring TLS
  keystores inside Trino.
* External clients authenticate with **JWT** issued by the platform IdP
  (toggle by setting `server.config.authenticationType=JWT` and the JWT
  block — see runbook §4).
* The chart's built-in `catalogs:` field is left empty; the three catalog
  ConfigMaps are mounted directly into `/etc/trino/catalog/` so each
  catalog can be edited and reloaded independently.

## Operations

See [`infra/runbooks/trino.md`](../../runbooks/trino.md) for installation,
secret rotation, scaling, coordinator failover, and troubleshooting
procedures.
