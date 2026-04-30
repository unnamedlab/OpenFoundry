# Per-bounded-context CloudNativePG `Cluster`s

This directory holds **one CNPG `Cluster` manifest per bounded context**
(database-per-service), instantiating the parametrised template at
[`infra/k8s/cnpg/templates/cluster.yaml`](../templates/cluster.yaml) with
the values that make sense for that service.

It implements task **T12** (per-service Postgres clusters) and is the
substrate for **T13** (each service references its own CNPG `Cluster`).
The broader rationale lives in
[ADR-0010 — CloudNativePG as the single Postgres operator](../../../../docs/architecture/adr/ADR-0010-cnpg-postgres-operator.md).

## Naming convention

* One file per bounded context, named **`<bc>-pg.yaml`** (e.g.
  `identity-federation-pg.yaml`, `data-asset-catalog-pg.yaml`).
* The CRD `metadata.name` mirrors the file name without the extension
  (`<bc>-pg`). CNPG derives the read/write Service name from the
  cluster, so each service then dials
  `<bc>-pg-rw.openfoundry.svc.cluster.local:5432` (and `-ro` for
  read-only replicas).
* The bootstrap database and owner default to `<bc>` so credentials
  Secrets and DSNs stay self-describing
  (`<bc>-pg-app` follows the CNPG default of `<cluster>-app`).

## What every file must declare

The template at `infra/k8s/cnpg/templates/cluster.yaml` already provides
sane defaults (3 instances, 1 sync replica, `ceph-rbd` storage class,
S3 backups to `s3://openfoundry-pg-backups/<cluster>/`). Files under
this directory are **standalone manifests** — i.e. they do not invoke
the Helm template, they encode the same shape directly so that
`kubectl --dry-run=client apply -f infra/k8s/cnpg/clusters/<bc>-pg.yaml`
works without rendering. This mirrors the existing `apicurio-pg`
manifest in `infra/k8s/strimzi/apicurio-registry.yaml`.

A new file should declare:

* `apiVersion: postgresql.cnpg.io/v1`, `kind: Cluster`.
* `metadata.name: <bc>-pg`, `metadata.namespace: openfoundry` (the same
  namespace the umbrella Helm chart deploys workloads into; override
  in environment-specific overlays only when justified).
* `spec.instances: 3` and `min/maxSyncReplicas: 1` (matches the
  durability stance of the Strimzi Kafka cluster — at least one
  in-sync standby acks every commit).
* `spec.bootstrap.initdb.{database,owner}: <bc>` and
  `spec.bootstrap.initdb.secret.name: <bc>-pg-app`.
* `spec.storage.storageClass: ceph-rbd` and a `size` chosen for the
  expected dataset size of the bounded context (start at `20Gi` for
  small catalogs / auth stores, `50Gi`+ for catalogs that grow with
  ingest).
* `spec.backup.barmanObjectStore` pointing at
  `s3://openfoundry-pg-backups/<bc>-pg` via the cluster-local Ceph RGW
  endpoint (`http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80`,
  same one used by the Iceberg/Lakekeeper stack — see
  `infra/k8s/rook/objectstore.yaml`). Credentials live in the
  `<bc>-pg-backup` Secret so they can be rotated independently of the
  app credentials.
* A bootstrap `Secret` named `<bc>-pg-app` carrying `username` /
  `password`. The shipped value **must** be replaced at deploy time
  by External Secrets / Vault — never commit a real password.
* A backup credentials `Secret` named `<bc>-pg-backup` carrying
  `ACCESS_KEY_ID`, `ACCESS_SECRET_KEY` (and `REGION` when relevant),
  populated the same way.

## Wiring services to their cluster

Services consume the cluster through the `DATABASE_URL` env var, which
is plumbed from the umbrella Helm chart (`infra/k8s/helm/open-foundry/`)
via the per-service `env:` map in `values.yaml`. The
`config::Environment` loader in each service maps `DATABASE_URL` onto
`AppConfig.database_url` automatically — **do not hardcode DSNs in
`config/*.toml`**. The DSN format is:

```
postgresql://<user>:<password>@<bc>-pg-rw.openfoundry.svc.cluster.local:5432/<bc>?sslmode=require
```

Username / password come from the `<bc>-pg-app` Secret created
alongside the `Cluster`. Rendering them into the env var is the
responsibility of the values overlay (typically with External Secrets
projecting the Secret into the chart-wide `existingSecret` referenced
by `global.existingSecret`).

## Roll-out strategy

Migrating every service to its own CNPG `Cluster` is split into
**waves of 1–2 services per PR** to keep blast radius small. The PR
that introduces this directory wires the first two pilots
(`identity-federation-pg`, `data-asset-catalog-pg`); subsequent waves
follow the same recipe and update the checklist in
[`infra/runbooks/cnpg.md`](../../../runbooks/cnpg.md).

## T13 binding coverage (per-service)

Closure of **T13** (each `services/*/migrations` targets its own CNPG
`Cluster`) is materialised by the binding doc each stateful service
ships at `services/<svc>/k8s/README.md`. Every such doc points at the
matching manifest in this directory and at the projected Secret used to
inject `DATABASE_URL`. The current invariant is:

* **63** services own Postgres state (i.e. ship a non-empty
  `services/*/migrations/`); **63** matching `Cluster` manifests live
  in this directory; **63** binding docs exist under
  `services/*/k8s/README.md`. There must never be drift between these
  three sets — if you add a `migrations/` directory, you must also add
  a `<bc>-pg.yaml` here and a `services/<bc>-service/k8s/README.md`.
* **31** services in `services/` are **stateless** (no `migrations/`)
  and intentionally have **no** CNPG `Cluster`. They include
  `edge-gateway-service`, `model-serving-service`, `tool-registry-service`,
  `widget-registry-service`, `prompt-workflow-service`, etc. They must
  not be flagged as "missing CNPG cluster" in audits.

A lightweight invariant check that future CI can run:

```bash
# Every service with migrations/ must have both a CNPG cluster manifest
# and a k8s/README.md binding.
for d in services/*/; do
  svc=$(basename "$d")
  bc=${svc%-service}
  has_mig=0
  [ -d "$d/migrations" ] && [ -n "$(ls -A "$d/migrations" 2>/dev/null)" ] && has_mig=1
  if [ "$has_mig" = 1 ]; then
    test -f "infra/k8s/cnpg/clusters/${bc}-pg.yaml" \
      || { echo "MISSING manifest: ${bc}-pg.yaml"; exit 1; }
    test -f "$d/k8s/README.md" \
      || { echo "MISSING binding: $d/k8s/README.md"; exit 1; }
  fi
done
```
