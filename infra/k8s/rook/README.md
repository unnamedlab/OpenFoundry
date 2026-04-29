# Rook-Ceph manifests (production object storage)

These manifests describe the Ceph cluster that backs OpenFoundry's S3-compatible
object store in production. They are consumed either directly with `kubectl
apply` or through the Terraform module under `infra/terraform/modules/ceph`.

## Files

| File              | Purpose                                                        |
|-------------------|----------------------------------------------------------------|
| `cluster.yaml`    | `CephCluster` (mon=5, mgr=2, dataDirHostPath, device discovery)|
| `objectstore.yaml`| `CephObjectStore` + `StorageClass` for the bucket provisioner  |
| `bucket.yaml`     | `ObjectBucketClaim`s for datasets / models / iceberg           |

The Rook **operator** itself is not packaged here — it is installed via the
upstream `rook-ceph` Helm chart from <https://charts.rook.io/release> by the
Terraform module. These manifests only describe the *desired Ceph topology*
that the operator should reconcile.

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
