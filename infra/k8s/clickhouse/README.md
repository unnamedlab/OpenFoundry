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
| `clickhouse.yaml`     | `ClickHouseInstallation`, 1 cluster, shards=2, replicas=2        |
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

# 3. ClickHouse cluster (shards=2, replicas=2):
kubectl apply -f clickhouse.yaml
kubectl -n clickhouse wait --for=jsonpath='{.status.status}'=Completed \
  chi/openfoundry --timeout=20m

# 4. Trino catalog (mounted by the Trino Helm release):
kubectl apply -f trino-catalog.yaml
```

See `infra/runbooks/clickhouse.md` for installation, schema bootstrap,
backup / restore and disaster-recovery procedures.
