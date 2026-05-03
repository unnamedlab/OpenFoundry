# CloudNativePG (CNPG) — operational runbook

CloudNativePG (`postgresql.cnpg.io`, Apache-2.0) is the **single
Postgres operator** for OpenFoundry. The architectural rationale lives
in [ADR-0010 — CloudNativePG as the single Postgres operator](../../docs/architecture/adr/ADR-0010-cnpg-postgres-operator.md).
This runbook covers day-2 operations.

> **Out of scope:** the operator itself
> (`infra/k8s/platform/manifests/cnpg/operator/values.yaml`). Operator upgrades follow the
> generic playbook in `infra/runbooks/upgrade-playbook.md`.

## Layout

| Path                                       | Purpose                                                                 |
|--------------------------------------------|-------------------------------------------------------------------------|
| `infra/k8s/platform/manifests/cnpg/operator/`                 | Helm values for the cluster-wide CNPG operator (do **not** touch in service-migration PRs). |
| `infra/k8s/platform/manifests/cnpg/templates/cluster.yaml`    | Reference Helm template for a `Cluster` CR with sane platform defaults. |
| `infra/k8s/platform/manifests/cnpg/clusters/`                 | One standalone `Cluster` manifest per bounded context (`<bc>-pg.yaml`). |
| `infra/k8s/platform/manifests/cnpg/clusters/README.md`        | Naming convention and required fields per file.                         |
| `services/<svc>/k8s/README.md`             | Service-side wiring notes (which `Cluster` backs the service, DSN env). |

## Create a new bounded-context cluster

Adding a new service to CNPG is one PR per **wave of 1–2 services** —
never migrate everything in one PR.

1. **Pick the bounded context.** It must already have versioned
   migrations under `services/<svc>/migrations/` and (preferably) be
   present in one of the split Helm release values files under
   `infra/k8s/helm/of-*/values.yaml`.

2. **Create `infra/k8s/platform/manifests/cnpg/clusters/<bc>-pg.yaml`.** Copy one of the
   existing pilots (`identity-federation-pg.yaml`,
   `data-asset-catalog-pg.yaml`) and tune:
   * `metadata.name` → `<bc>-pg`
   * `metadata.labels.openfoundry.io/bounded-context` → `<bc>`
   * `spec.bootstrap.initdb.{database,owner}` → `<bc_with_underscores>`
   * `spec.bootstrap.initdb.secret.name` → `<bc>-pg-app`
   * `spec.storage.size` → fit expected dataset (see
     `infra/k8s/platform/manifests/cnpg/clusters/README.md` for guidance).
   * `spec.backup.barmanObjectStore.destinationPath` →
     `s3://openfoundry-pg-backups/<bc>-pg`.
   * The bootstrap `Secret` (`<bc>-pg-app`) and backup-credentials
     `Secret` (`<bc>-pg-backup`) — keep `change-me` as placeholder
     and replace in-cluster via External Secrets / Vault.

3. **Wire the service in the split chart.** Edit the owning release
   values file under `infra/k8s/helm/of-*/values.yaml`, adding to the
   service's block:
   block:

   ```yaml
   envSecrets:
     DATABASE_URL:
       secretName: <bc>-pg-app
       key: uri
   ```

   CNPG auto-populates the `uri` key with a fully-formatted DSN
   (`postgresql://<user>:<password>@<bc>-pg-rw:5432/<db>`), so no
   credential ever appears in this repo.

4. **Add `services/<svc>/k8s/README.md`** documenting the binding
   (cluster name, RW/RO DNS, DSN env-var convention, link back here).

5. **Validate locally:**

   ```bash
   just helm-check
   kubectl --dry-run=client apply -f infra/k8s/platform/manifests/cnpg/clusters/<bc>-pg.yaml
   grep -rln "cnpg\|cloudnative-pg\|postgresql.cnpg.io" services/   # must list <svc>
   ```

6. **Apply (in the target cluster, after operator is healthy):**

   ```bash
   kubectl apply -f infra/k8s/platform/manifests/cnpg/clusters/<bc>-pg.yaml
   kubectl -n openfoundry wait --for=condition=Ready \
     cluster.postgresql.cnpg.io/<bc>-pg --timeout=10m
   cd infra/k8s/helm && helmfile -e <env> apply
   ```

7. **Run migrations.** Migrations are baked into the service container
   and execute on startup against `$DATABASE_URL` (`sqlx migrate run`).
   Confirm with `kubectl logs deploy/<svc>` that the migration step
   succeeded.

## Manual failover

CNPG handles automatic failover when the primary becomes unhealthy.
For planned failovers (e.g. node drain, primary upgrade test) use the
`cnpg` `kubectl` plugin:

```bash
# Inspect cluster topology and identify the current primary.
kubectl cnpg status <bc>-pg -n openfoundry

# Promote a specific replica (or omit the flag to let CNPG pick).
kubectl cnpg promote <bc>-pg -n openfoundry [<replica-pod-name>]
```

Verify the new primary served traffic correctly by tailing logs of the
client pods (`kubectl logs deploy/<svc> -n openfoundry`) — they
should reconnect to `<bc>-pg-rw` within seconds. The chart's PDBs
guarantee at least one in-sync standby remains during the swap, so no
acknowledged commit is lost (`minSyncReplicas: 1`).

If `kubectl cnpg` is not installed:

```bash
kubectl krew install cnpg                # one-time
# or download from https://github.com/cloudnative-pg/cloudnative-pg/releases
```

## Backups

Every per-bounded-context cluster ships its own
`backup.barmanObjectStore` block pointing at:

* **Bucket:** `openfoundry-pg-backups` on the cluster-local Ceph RGW
  (`http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80`, defined by
  `infra/k8s/platform/manifests/rook/objectstore.yaml`).
* **Sub-path:** `s3://openfoundry-pg-backups/<bc>-pg/` — one prefix
  per cluster, never shared.
* **Credentials:** `Secret/<bc>-pg-backup` keys
  `ACCESS_KEY_ID` / `ACCESS_SECRET_KEY` (`REGION` optional).
* **Retention:** 30 days by default. Override per-cluster only when
  compliance requires longer retention.

### Trigger an on-demand base backup

```bash
cat <<EOF | kubectl apply -f -
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: <bc>-pg-$(date +%Y%m%d-%H%M%S)
  namespace: openfoundry
spec:
  cluster:
    name: <bc>-pg
EOF
```

### Restore from backup

Restores go through a fresh `Cluster` bootstrapped with
`bootstrap.recovery` pointing at the same `barmanObjectStore`. Full
recipe (with PITR) lives in CNPG's official docs:
<https://cloudnative-pg.io/documentation/current/recovery/>. Cross-link
the disaster-recovery flow with `infra/runbooks/disaster-recovery.md`.

## Migration wave tracking

The first wave (the pilot PR that introduced this directory) wired:

* `identity-federation-service` → `identity-federation-pg`
* `data-asset-catalog-service`  → `data-asset-catalog-pg`

A subsequent bulk PR closed **T13** by generating a `Cluster` manifest
for **every** bounded context with `services/<svc>/migrations/`, so the
directory now contains one `<bc>-pg.yaml` per service that requires
Postgres. The `kubectl apply` step itself still rolls out **1–2
services per PR** so the blast radius of any per-service tuning
(storage size, resources, logical-replication slots, …) stays small;
the manifests already in tree are the inventory, not a single-shot
deploy plan.

Track remaining work using `services/*/migrations` as the source of
truth and reconcile against the manifest set:

```bash
# Bounded contexts that need a Cluster manifest.
find services -mindepth 2 -maxdepth 2 -type d -name migrations \
  | sed -E 's|services/||;s|/migrations||;s|-service$||' \
  | sort > /tmp/bc.txt

# Bounded contexts that already have one.
ls infra/k8s/platform/manifests/cnpg/clusters/*.yaml \
  | xargs -n1 basename \
  | sed 's|-pg\.yaml$||' \
  | sort > /tmp/cluster.txt

diff /tmp/bc.txt /tmp/cluster.txt   # must be empty
```

The grep gate stays honest about service-side wiring (the Helm
`envSecrets.DATABASE_URL` projection per service):

```bash
# Should grow by 1–2 lines per wave PR.
grep -rln "cnpg\|cloudnative-pg\|postgresql.cnpg.io" services/ | sort
```

## See also

* [ADR-0010 — CloudNativePG as the single Postgres operator](../../docs/architecture/adr/ADR-0010-cnpg-postgres-operator.md)
* `infra/k8s/platform/manifests/cnpg/operator/values.yaml` — operator install values
* `infra/k8s/platform/manifests/cnpg/templates/cluster.yaml` — reference Helm template
* `infra/k8s/platform/manifests/cnpg/clusters/README.md` — per-bounded-context manifest convention
* `infra/runbooks/disaster-recovery.md` — multi-component DR flow
* `infra/runbooks/ceph.md` — Ceph RGW / S3 endpoint and credentials
