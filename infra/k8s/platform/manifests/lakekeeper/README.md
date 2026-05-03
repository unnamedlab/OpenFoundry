# Lakekeeper (Iceberg REST Catalog) — Kubernetes manifests

These manifests deploy [Lakekeeper](https://github.com/lakekeeper/lakekeeper)
(Apache-2.0) as the Iceberg REST Catalog used by every OpenFoundry service
that opens an Iceberg table — see
[`libs/storage-abstraction/README.md`](../../../../../libs/storage-abstraction/README.md)
for the client-side wrapper and ADR-0008 for the selection rationale.

The catalog process is **stateless**; metadata is persisted in a
[CloudNativePG](https://cloudnative-pg.io/) (Apache-2.0) `Cluster` named
`lakekeeper-pg` and table data is written to the Ceph RGW bucket
`openfoundry-iceberg` (declared in
[`infra/k8s/platform/manifests/rook/bucket.yaml`](../rook/bucket.yaml)).

## Files

| File             | Purpose                                                        |
|------------------|----------------------------------------------------------------|
| `namespace.yaml` | `Namespace lakekeeper` with the standard OpenFoundry labels    |
| `values.yaml`    | Overrides for the upstream `lakekeeper/lakekeeper` Helm chart  |
| `README.md`      | This file — apply order, dependencies, placeholders            |

The chart itself is not vendored here. It is consumed straight from the
upstream OCI/Helm repository at install time:

```text
repository: https://lakekeeper.github.io/lakekeeper-charts/
chart:      lakekeeper
license:    Apache-2.0
```

Verify the license and pin the chart version in the install command
(`--version X.Y.Z`) on every upgrade.

## Dependencies

These resources MUST exist in the cluster before `helm install` succeeds:

1. **CloudNativePG operator** — installed cluster-wide (see
   [`infra/k8s/platform/manifests/cnpg/templates/cluster.yaml`](../cnpg/templates/cluster.yaml)
   for the operator's CRDs in use elsewhere).
2. **CNPG `Cluster lakekeeper-pg`** in the `lakekeeper` namespace —
   **owned by Task 12, not created here**. It MUST publish:
   * a `lakekeeper-pg-rw` Service (read/write endpoint), and
   * a `lakekeeper-pg-app` Secret with `username` and `password` keys
     (the CNPG default for application credentials).
   The database name is expected to be `lakekeeper` — adjust
   `externalDatabase.database` in `values.yaml` if Task 12 picks a
   different name.
3. **Rook-Ceph RGW** with the `openfoundry-iceberg` bucket — see
   [`infra/k8s/platform/manifests/rook/README.md`](../rook/README.md). The Service
   `rook-ceph-rgw-rgw-data.rook-ceph.svc:80` is the in-cluster S3 endpoint
   referenced by `LAKEKEEPER__S3_ENDPOINT` in `values.yaml`.
4. **`identity-federation-service`** reachable at the OIDC issuer URL
   configured under `auth.oauth2.providerUri` in `values.yaml`. The
   service is deployed by the split `of-platform` chart
   (`services.identity-federation-service`).

## Placeholders to provision out-of-band

`values.yaml` references three Secrets that are intentionally **not**
created by this directory. Provision them before (or, for `lakekeeper-s3`,
right after) `helm install`:

| Secret                       | Keys                                              | Source / how to populate                                                                                                                                            |
|------------------------------|---------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `lakekeeper-pg-app`          | `username`, `password`                            | Created by CNPG when the `Cluster lakekeeper-pg` is reconciled (Task 12). Standard CNPG behaviour — do not create manually.                                          |
| `lakekeeper-s3`              | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`     | Harvest from the Rook OBC's generated Secret (see [`infra/runbooks/ceph.md`](../../../../runbooks/ceph.md)) and copy into the `lakekeeper` namespace, e.g. with `kubectl create secret generic lakekeeper-s3 --from-literal=AWS_ACCESS_KEY_ID=... --from-literal=AWS_SECRET_ACCESS_KEY=...`. |
| `lakekeeper-encryption-key`  | `encryptionKey`                                   | A 32-byte random string used by Lakekeeper to encrypt per-warehouse storage credentials at rest in CNPG. Generate once with `openssl rand -base64 32` and store in the cluster secret manager. **If lost, all per-warehouse credentials become unrecoverable.** |

The OIDC client (`auth.oauth2.audience` / `auth.oauth2.ui.clientID`) and
the realm path inside `auth.oauth2.providerUri` are placeholders too —
update them to whatever `identity-federation-service` exposes for
Lakekeeper.

## Apply order

```bash
# 0. Install the CNPG operator cluster-wide (one-off, see Task 12 / runbook).
#    helm repo add cnpg https://cloudnative-pg.github.io/charts
#    helm install cnpg cnpg/cloudnative-pg -n cnpg-system --create-namespace

# 1. Create the namespace.
kubectl apply -f infra/k8s/platform/manifests/lakekeeper/namespace.yaml

# 2. Create the CNPG `Cluster lakekeeper-pg` (Task 12 — NOT in this dir).
#    kubectl -n lakekeeper apply -f <task-12 manifest>
kubectl -n lakekeeper wait --for=condition=Ready cluster/lakekeeper-pg --timeout=10m

# 3. Create the placeholder Secrets (see table above).
kubectl -n lakekeeper create secret generic lakekeeper-s3 \
    --from-literal=AWS_ACCESS_KEY_ID="$RGW_ACCESS_KEY" \
    --from-literal=AWS_SECRET_ACCESS_KEY="$RGW_SECRET_KEY"
kubectl -n lakekeeper create secret generic lakekeeper-encryption-key \
    --from-literal=encryptionKey="$(openssl rand -base64 32)"

# 4. Install / upgrade Lakekeeper from the upstream chart.
helm repo add lakekeeper https://lakekeeper.github.io/lakekeeper-charts/
helm repo update lakekeeper
helm upgrade --install lakekeeper lakekeeper/lakekeeper \
    --namespace lakekeeper \
    --version <pinned-version> \
    --values infra/k8s/platform/manifests/lakekeeper/values.yaml \
    --wait

# 5. Verify the catalog Service is reachable in-cluster.
kubectl -n lakekeeper get svc lakekeeper
#   The Iceberg REST endpoint is then:
#     http://lakekeeper.lakekeeper.svc:8181
```

The same URL is wired into the rest of the platform via:

* [`infra/k8s/helm/profiles/values-prod.yaml`](../../../helm/profiles/values-prod.yaml)
  — `icebergRestCatalog.url`,
* [`libs/storage-abstraction/README.md`](../../../../../libs/storage-abstraction/README.md)
  — Iceberg client example.

## Validation

```bash
yamllint infra/k8s/platform/manifests/lakekeeper/

helm repo add lakekeeper https://lakekeeper.github.io/lakekeeper-charts/
helm template lakekeeper lakekeeper/lakekeeper \
    --namespace lakekeeper \
    --values infra/k8s/platform/manifests/lakekeeper/values.yaml \
    --debug
```
