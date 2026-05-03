# ceph (Rook + CephObjectStore) Terraform module

Provisions the production object-storage backend for OpenFoundry:

1. Creates the `rook-ceph` namespace.
2. Installs the upstream **rook-ceph** operator via Helm
   (chart repo `https://charts.rook.io/release`).
3. Applies the desired Ceph topology from `infra/k8s/platform/manifests/rook/`:
   - `cluster.yaml` — `CephCluster` (mon=5, mgr=2, host-path state, device discovery)
   - `objectstore.yaml` — `CephObjectStore` with EC 8+3 data pool + replicated metadata pool, 3 RGW pods, and a `ceph-bucket` `StorageClass`
   - `bucket.yaml` — `ObjectBucketClaim`s for `openfoundry-datasets`, `openfoundry-models`, `openfoundry-iceberg`

The `libs/storage-abstraction` crate is unaffected — it keeps speaking S3
against `OBJECT_STORE_ENDPOINT`. In production that endpoint becomes:

```
http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80
```

## Usage

```hcl
module "ceph" {
  source = "../../modules/ceph"

  chart_version        = "v1.15.5"
  namespace            = "rook-ceph"
  app_namespace        = "openfoundry"
  create_app_namespace = true
  enable_monitoring    = true
}
```

## Inputs

See `variables.tf`. Toggles `apply_cluster`, `apply_object_store`, and
`apply_buckets` let you stage the rollout (e.g. apply the cluster, wait for
HEALTH_OK, then apply the object store and buckets in a follow-up plan).

## Outputs

| Name              | Description                                                |
|-------------------|------------------------------------------------------------|
| `namespace`       | Namespace where Rook + CephCluster live.                   |
| `s3_endpoint`     | In-cluster URL to use for `OBJECT_STORE_ENDPOINT`.         |
| `bucket_claims`   | Names of the OBCs that will be created.                    |
| `object_store_name` | Name of the `CephObjectStore` CR.                        |

## Operational notes

- The operator Helm release waits up to 10 minutes for the operator
  Deployment to roll out. The `CephCluster` `kubernetes_manifest` then waits
  for `status.phase == Ready`, which can take ~10–20 min on first install
  while OSDs are formatted.
- Credentials for each OBC are materialised by Rook into a `Secret` and a
  `ConfigMap` named after the bucket in `var.app_namespace`. See
  `infra/runbooks/ceph.md` for the projection into `open-foundry-prod-env`.
- This module does not configure Ceph encryption keys, multisite
  replication, or external monitoring stacks — extend via
  `operator_values_override` if needed.
