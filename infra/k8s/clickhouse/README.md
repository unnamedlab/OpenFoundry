# ClickHouse manifests (analytics, sub-second BI)

These manifests describe the ClickHouse deployment that powers OpenFoundry's
analytics / BI dashboards. They are reconciled by the
**Altinity clickhouse-operator** (Apache-2.0) and use the **official
`clickhouse/clickhouse-server` and `clickhouse/clickhouse-keeper` images**
(Apache-2.0). The Bitnami images are intentionally avoided because of the
Bitnami restricted-distribution policy.

## Files

| File                  | Purpose                                                          |
|-----------------------|------------------------------------------------------------------|
| `namespace.yaml`      | `clickhouse` namespace (operator + CRs)                          |
| `keeper.yaml`         | `ClickHouseKeeperInstallation`, replicas=3 (replaces ZooKeeper)  |
| `clickhouse.yaml`     | `ClickHouseInstallation`, 1 cluster, shards=2, replicas=3        |
| `trino-catalog.yaml`  | `ConfigMap` with the Trino catalog properties for this ClickHouse |

The operator itself is not packaged here -- it is installed via the
upstream `clickhouse-operator` Helm chart (see `infra/runbooks/clickhouse.md`).

## Apply order

```bash
# 0. Operator + CRDs (Altinity, Apache-2.0):
helm repo add altinity-clickhouse-operator \
  https://docs.altinity.com/clickhouse-operator
helm repo update
helm upgrade --install --create-namespace -n clickhouse clickhouse-operator \
  altinity-clickhouse-operator/altinity-clickhouse-operator

# 1. Namespace (idempotent if the operator already created it):
kubectl apply -f namespace.yaml

# 2. Keeper ensemble (must be Ready before ClickHouse server starts):
kubectl apply -f keeper.yaml
kubectl -n clickhouse wait --for=jsonpath='{.status.status}'=Completed \
  chk/openfoundry --timeout=15m

# 3. ClickHouse cluster (shards=2, replicas=3):
kubectl apply -f clickhouse.yaml
kubectl -n clickhouse wait --for=jsonpath='{.status.status}'=Completed \
  chi/openfoundry --timeout=20m

# 4. Trino catalog (mounted by the Trino Helm release):
kubectl apply -f trino-catalog.yaml
```

See `infra/runbooks/clickhouse.md` for installation, schema bootstrap,
backup / restore and disaster-recovery procedures.

## Topology rationale: real per-shard quorum

The `openfoundry` cluster is provisioned with **`shardsCount: 2`** and
**`replicasCount: 3`** (6 ClickHouse server pods total), backed by a
3-node ClickHouse Keeper ensemble (`keeper.yaml`).

Three replicas per shard give us a **real quorum inside each shard**,
which is the design objective for the analytics tier:

* With `insert_quorum=2` (majority of 3) writes are acknowledged only
  after they are durable on a strict majority of replicas, so the loss
  of any single replica neither blocks ingestion nor risks divergence.
* A 2-replica shard cannot satisfy this property: any quorum value
  would require *both* replicas (no fault tolerance) or fall back to
  `quorum=1` (no real majority, eventual consistency only). Three
  replicas are the minimum size that yields a non-trivial majority.
* Reads benefit from the extra replica through `load_balancing=random`
  without changing the Distributed table layout.

The Keeper ensemble stays at **`replicas: 3`** (see `keeper.yaml`)
because its Raft quorum is independent of the per-shard data quorum
and 3 nodes already tolerate one failure.

Pod resources (`requests`/`limits`) and the per-pod PVCs
(`clickhouse-data` 200Gi, `clickhouse-log` 25Gi) are unchanged: the
extra replica adds capacity and fault tolerance without any
per-pod sizing change. The `volumeClaimTemplates` continue to omit
`storageClassName`, so the cluster default (Ceph RBD in the reference
deployment) is used unchanged.
