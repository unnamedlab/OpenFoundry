# Runbook — Decommissioning the per-bounded-context CNPG clusters

> S6.5 of the Cassandra/Foundry parity migration plan.
>
> **State at time of writing**: the legacy 65 per-service `Cluster`
> manifests under `infra/k8s/cnpg/clusters/<bc>-pg.yaml` were already
> deleted from Git in S6.1.a. The cluster CRs and their PVCs may still
> exist in any pre-prod environment that was synced before that commit;
> this runbook removes them.

## Pre-flight

* **Pre-prod only.** No data migration. The four consolidated clusters
  (`pg-schemas`, `pg-policy`, `pg-lakekeeper`, `pg-runtime-config`) are
  bootstrapped empty by the new manifests + bootstrap-SQL ConfigMaps.
* Confirm with the team that no service is currently writing to a
  legacy cluster:
  `kubectl -n openfoundry get pods -l app.kubernetes.io/component=postgres`.

## Step 1 — Catalogue what is still live

```sh
kubectl -n openfoundry get clusters.postgresql.cnpg.io \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' \
    | grep -E -- '-pg$' \
    | sort > /tmp/legacy-clusters.txt
wc -l /tmp/legacy-clusters.txt
```

Expected: ~65 names (matches the manifest count deleted in S6.1.a).

## Step 2 — Delete the Cluster CRs

CNPG's finalizer removes the StatefulSet, Services and PodMonitors
automatically. PVCs survive until step 4.

```sh
xargs -a /tmp/legacy-clusters.txt -I{} \
    kubectl -n openfoundry delete cluster.postgresql.cnpg.io {} --wait=false
```

Watch progress:

```sh
kubectl -n openfoundry get clusters.postgresql.cnpg.io -w
```

## Step 3 — Delete the per-service Secrets

Two Secrets per legacy cluster:

* `<bc>-pg-app` — CNPG-managed superuser DSN (no longer projected
  anywhere; Helm now reads `<bc>-db-dsn`).
* `<bc>-pg-backup` — Ceph S3 credentials for Barman.

```sh
xargs -a /tmp/legacy-clusters.txt -I{} \
    kubectl -n openfoundry delete secret {}-app {}-backup --ignore-not-found
```

## Step 4 — Reap orphan PVCs

CNPG does **not** delete PVCs on cluster deletion (data-safety default).
After confirming no further rollback is desired:

```sh
# List orphans (no Cluster owner reference left).
kubectl -n openfoundry get pvc \
    -l cnpg.io/cluster \
    --no-headers \
    | awk '{print $1}' > /tmp/legacy-pvcs.txt

# Sanity-check before deleting.
wc -l /tmp/legacy-pvcs.txt
xargs -a /tmp/legacy-pvcs.txt -I{} \
    kubectl -n openfoundry delete pvc {} --wait=false
```

Underlying Ceph RBD volumes are released by the CSI driver according to
the StorageClass `reclaimPolicy` (`Delete` by default in our
`ceph-rbd` SC).

## Step 5 — Reap Barman buckets in Ceph S3 (optional)

Each legacy cluster wrote backups under
`s3://openfoundry-pg-backups/<bc>-pg/`. Delete the prefixes once you
are sure no point-in-time recovery is needed:

```sh
mc rm --recursive --force ceph/openfoundry-pg-backups/<bc>-pg/
```

(Replace `mc` with your Ceph S3 client of choice.)

## Step 6 — Verify the new world is healthy

```sh
kubectl -n openfoundry get cluster.postgresql.cnpg.io
# Expect exactly:
#   pg-schemas         3/3   Cluster in healthy state
#   pg-policy          3/3   Cluster in healthy state
#   pg-lakekeeper      3/3   Cluster in healthy state
#   pg-runtime-config  3/3   Cluster in healthy state

kubectl -n openfoundry get pooler.postgresql.cnpg.io
# Expect three poolers (no pooler for pg-lakekeeper).
```

Smoke-test a service:

```sh
kubectl -n openfoundry exec deploy/identity-federation-service -- \
    psql "$DATABASE_URL" -c "SELECT current_user, current_schema;"
# Expect: svc_identity_federation | identity_federation
```

## Rollback

There is no rollback. The legacy manifests are gone from Git; restoring
them would require reverting the S6.1.a commit and re-applying the old
65 clusters. Pre-prod has no production data, so re-bootstrap is
cheaper than rollback.
