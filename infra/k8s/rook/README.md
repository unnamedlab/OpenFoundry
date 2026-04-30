# Rook-Ceph manifests (production object storage)

These manifests describe the Ceph cluster that backs OpenFoundry's S3-compatible
object store in production. They are consumed either directly with `kubectl
apply` or through the Terraform module under `infra/terraform/modules/ceph`.

## Files

| File              | Purpose                                                        |
|-------------------|----------------------------------------------------------------|
| `cluster.yaml`    | `CephCluster` (mon=5, mgr=2, dataDirHostPath, device discovery) + `CephBlockPool rbd-fast` |
| `objectstore.yaml`| `CephObjectStore openfoundry` (legacy, EC 8+3) and `CephObjectStore rgw-data` (EC 4+2) + their `StorageClass`es |
| `bucket.yaml`     | `ObjectBucketClaim`s for datasets / models / iceberg           |

The Rook **operator** itself is not packaged here — it is installed via the
upstream `rook-ceph` Helm chart from <https://charts.rook.io/release> by the
Terraform module. These manifests only describe the *desired Ceph topology*
that the operator should reconcile.

## CRUSH layout and pool assignment

The CephCluster keeps a HA control plane (`mon.count=5`, `mgr.count=2`, see
`cluster.yaml` lines 30–35) and exposes two purpose-built pools on top of it.
Mon and mgr pods are explicitly spread across availability zones via
`placement.mon.topologySpreadConstraints` / `placement.mgr.topologySpreadConstraints`
(`topologyKey: topology.kubernetes.io/zone`, `maxSkew: 1`). With 5 mons across
3+ zones the resulting layout is 2-2-1, so quorum (3 of 5) survives the loss
of any single zone. Mons use `whenUnsatisfiable: DoNotSchedule` (a Pending mon
is preferable to a mis-placed mon); mgrs use `ScheduleAnyway` (mgr loss does
not break I/O). The contract is enforced on every PR by
`tools/ceph-lint/check_topology.py` (CI: `.github/workflows/ceph-lint.yml`).

| Pool       | Type                  | Failure domain | Device class | Workload                        |
|------------|-----------------------|----------------|--------------|---------------------------------|
| `rbd-fast` | Replicated (size=3)   | `zone`         | `nvme`       | **Kafka** log segments, **Postgres** (PGDATA + WAL) — IOPS-sensitive |
| `rgw-data` | Erasure coded (k=4, m=2) | `zone`      | default      | **Iceberg** parquet/manifest files via the `rgw-data` `CephObjectStore` |
| `openfoundry.*` (legacy) | Replicated metadata + EC 8+3 data | `host` | default | Existing `datasets` / `models` / `iceberg` buckets — kept for backwards compatibility (allowlisted in `tools/ceph-lint/check_topology.py::LEGACY_HOST_FAILURE_DOMAIN_ALLOWLIST`) |

Notes:

* `failureDomain` is set explicitly on every new pool. We use `zone` because
  the cluster only labels nodes with `topology.kubernetes.io/zone` (see
  `infra/k8s/strimzi/kafka-cluster.yaml:67`); no `topology.rook.io/rack` (or
  equivalent) label is published. The brief allows `rack` *only when* nodes
  expose such a label — switch the two `failureDomain: zone` occurrences in
  `objectstore.yaml` to `failureDomain: rack` once the topology label is
  rolled out cluster-wide.
* `rbd-fast` pins itself to `deviceClass: nvme` so that random-IO workloads
  never land on slower SAS/SATA OSDs even on mixed nodes.
* `rgw-data` uses EC 4+2 (raw→usable ≈ 1.5×) which tolerates the loss of any
  two zones simultaneously while keeping write amplification low for the
  large, append-mostly Iceberg objects.
* **No `cephfs-shared`** filesystem is declared: the previous topology never
  shipped one, so creating it now would be a non-reversible expansion of the
  storage surface.

### Consuming the pools

* Kafka (`infra/k8s/strimzi/kafka-cluster.yaml`) and Postgres should be
  pointed at a `StorageClass` whose `pool` parameter is `rbd-fast` and whose
  `clusterID` is `rook-ceph` (provisioner `rook-ceph.rbd.csi.ceph.com`). The
  `StorageClass` itself is intentionally **not** declared here; it is owned
  by the per-workload modules so each can tune `imageFeatures`,
  `csi.storage.k8s.io/fstype`, etc. without touching this directory.
* New Iceberg `ObjectBucketClaim`s should set `storageClassName:
  ceph-bucket-rgw-data` so they land in the EC 4+2 store. Existing OBCs in
  `bucket.yaml` are left on the legacy `ceph-bucket` class to avoid a
  destructive bucket re-creation; migration is tracked in
  `infra/runbooks/ceph.md`.

## Apply order

```bash
# 0. Operator + CRDs (handled by Terraform, or manually):
helm repo add rook-release https://charts.rook.io/release
helm install --create-namespace -n rook-ceph rook-ceph rook-release/rook-ceph

# 1. Cluster
kubectl apply -f cluster.yaml
kubectl -n rook-ceph wait --for=condition=Ready cephcluster/openfoundry --timeout=20m

# 2. Object store (RGW + pools + StorageClass)
kubectl apply -f objectstore.yaml
kubectl -n rook-ceph wait --for=condition=Ready cephobjectstore/openfoundry --timeout=10m

# 3. Buckets
kubectl apply -f bucket.yaml
```

See `infra/runbooks/ceph.md` for installation, OSD expansion, and disaster
recovery procedures, including how to harvest OBC credentials and feed them
into the `open-foundry-prod-env` secret consumed by the platform.
