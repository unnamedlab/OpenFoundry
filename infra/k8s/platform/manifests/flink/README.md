# Apache Flink on Kubernetes — OpenFoundry manifests

This directory contains the OpenFoundry-specific manifests for the
[Apache Flink Kubernetes Operator](https://github.com/apache/flink-kubernetes-operator)
(Apache-2.0). The operator itself is installed via Helm using the values in
[`values.yaml`](values.yaml); the operational runbook lives at
[`infra/runbooks/flink.md`](../../../../runbooks/flink.md).

| File / directory                                | Purpose                                                                              |
| ----------------------------------------------- | ------------------------------------------------------------------------------------ |
| [`namespace.yaml`](namespace.yaml)              | `Namespace flink` with the standard OpenFoundry labels.                              |
| [`values.yaml`](values.yaml)                    | Helm overrides for `flink-kubernetes-operator`.                                      |
| [`flinkdeployment-cdc-iceberg.yaml`](flinkdeployment-cdc-iceberg.yaml) | Streaming CDC → Iceberg job (HA, checkpoints on Ceph RGW).                           |
| [`maintenance/`](maintenance/)                  | Scheduled Iceberg maintenance jobs (compaction, snapshot expiration, orphan files). |

---

## 1. Streaming jobs

* **`cdc-iceberg`** — see [`flinkdeployment-cdc-iceberg.yaml`](flinkdeployment-cdc-iceberg.yaml).
  Reads CDC events from Redpanda and writes Iceberg tables to
  `s3://openfoundry-iceberg/warehouse` via the
  Lakekeeper REST catalog (Service URL pinned in
  [`infra/k8s/helm/profiles/values-prod.yaml`](../../../helm/profiles/values-prod.yaml)
  line 364: `http://lakekeeper.lakekeeper.svc:8181`).

---

## 2. Iceberg maintenance jobs (`maintenance/`)

The CDC writer commits frequently and produces many small files plus a steady
stream of snapshots. Two batch FlinkDeployments take care of this:

| FlinkDeployment                                                                                       | Action                                                                                | Defaults                                                          |
| ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| [`maintenance/rewrite-data-files.yaml`](maintenance/rewrite-data-files.yaml)                          | `Actions.rewriteDataFiles(table)` — bin-pack small files into 128 MiB ZSTD Parquet.   | `target-file-size-bytes=134217728`, `compression-codec=zstd`.     |
| [`maintenance/expire-snapshots-and-orphans.yaml`](maintenance/expire-snapshots-and-orphans.yaml)      | `Actions.expireSnapshots(table)` + `Actions.deleteOrphanFiles(table)`.                | Tiered retention (see §4.2). Orphan guard `older-than-hours=72`.  |

Both jobs:

* Run as **batch FlinkDeployments** (`execution.runtime-mode: BATCH`,
  `upgradeMode: stateless`) — they exit when all selected tables have been
  processed.
* Read tables from the **Lakekeeper REST catalog** at
  `http://lakekeeper.lakekeeper.svc:8181` (the same Service the CDC job uses).
* Operate on a **comma-separated list of namespaces** read from the
  `ICEBERG_NAMESPACES` environment variable (default: `openfoundry_cdc`).
  Override per environment by patching the CronJob, e.g.:

  ```bash
  kubectl -n flink set env cronjob/iceberg-rewrite-data-files \
      ICEBERG_NAMESPACES=openfoundry_cdc,openfoundry_metrics
  ```

* Are **highly available**: `jobManager.replicas: 2` with
  `high-availability.type: kubernetes` and HA storage on the
  `openfoundry-iceberg` Ceph RGW bucket — identical wiring to
  [`flinkdeployment-cdc-iceberg.yaml`](flinkdeployment-cdc-iceberg.yaml)
  (lines 43–50, 76–95).
* Persist **checkpoints and savepoints on Ceph RGW** under
  `s3://openfoundry-iceberg/flink/{checkpoints,savepoints}/<job-name>` using
  the same `flink-s3-credentials` Secret as the CDC job (materialised from
  the `openfoundry-iceberg` ObjectBucketClaim — see
  [`infra/runbooks/ceph.md`](../../../../runbooks/ceph.md) §3).

---

## 3. Scheduling (Kubernetes `CronJob` → `FlinkDeployment`)

The Flink Kubernetes Operator does **not** ship a `Schedule` CRD. The
canonical way to run a batch FlinkDeployment on a cadence is a vanilla
`batch/v1` `CronJob` that:

1. `kubectl delete` any leftover FlinkDeployment from the previous tick
   (idempotent — `--ignore-not-found`).
2. `kubectl apply -f` the FlinkDeployment manifest mounted from a `ConfigMap`.
3. Polls `.status.jobStatus.state` until it reaches `FINISHED` (success) or
   `FAILED`/`CANCELED` (failure).

The CronJob pods run as the dedicated `iceberg-maintenance-scheduler`
ServiceAccount with a namespace-scoped Role that only grants
`flink.apache.org/flinkdeployments` verbs in the `flink` namespace
(no cluster-wide permissions).

| CronJob                                                                                                          | Schedule (UTC)        | Tier   | FlinkDeployment template                                                       |
| ---------------------------------------------------------------------------------------------------------------- | --------------------- | ------ | ------------------------------------------------------------------------------ |
| [`cronjob-rewrite-data-files.yaml`](maintenance/cronjob-rewrite-data-files.yaml) — `iceberg-rewrite-data-files`  | `30 2 * * *` (daily)  | n/a    | `maintenance/rewrite-data-files.yaml`                                          |
| [`cronjob-expire-snapshots-and-orphans.yaml`](maintenance/cronjob-expire-snapshots-and-orphans.yaml) — `…-hot`   | `30 3 * * *` (daily)  | hot    | `maintenance/expire-snapshots-and-orphans.yaml` (`ICEBERG_TIER=hot`, 7 d)      |
| [`cronjob-expire-snapshots-and-orphans.yaml`](maintenance/cronjob-expire-snapshots-and-orphans.yaml) — `…-gold`  | `0 4 * * 0` (weekly)  | gold   | `maintenance/expire-snapshots-and-orphans.yaml` (`ICEBERG_TIER=gold`, 90 d)    |

The schedules are intentionally staggered so that compaction (02:30) finishes
before the hot-tier expiration job (03:30) runs against the new snapshots.
The gold-tier expiration runs once a week (Sunday 04:00) because the 90 d
retention window does not need finer granularity and the job rewrites a
much larger metadata footprint.

### Apply order

```bash
# 1. Namespace + operator (one-time)
kubectl apply -f infra/k8s/platform/manifests/flink/namespace.yaml
helm upgrade --install flink-kubernetes-operator \
    flink-operator-repo/flink-kubernetes-operator \
    -n flink -f infra/k8s/platform/manifests/flink/values.yaml

# 2. Streaming CDC job (already running in production)
kubectl apply -f infra/k8s/platform/manifests/flink/flinkdeployment-cdc-iceberg.yaml

# 3. Scheduled maintenance (this PR)
kubectl apply -f infra/k8s/platform/manifests/flink/maintenance/
```

---

## 4. Iceberg snapshot retention policy

### 4.1 Why we need this

Every Flink CDC commit creates a new Iceberg snapshot. Without expiration the
metadata tree grows unboundedly, planning latency degrades, and orphan data
files (left behind by compaction or by failed commits) accumulate on RGW. The
maintenance jobs above implement the retention policy below.

### 4.2 Tiered retention (hot vs gold)

This matches §4.2 of the OpenFoundry data-platform design:

| Tier   | Snapshot retention | Min snapshots kept | Orphan-file age threshold | Schedule (UTC)      |
| ------ | ------------------ | ------------------ | ------------------------- | ------------------- |
| `hot`  | **7 days**         | 5                  | 72 h                      | Daily, 03:30        |
| `gold` | **90 days**        | 5                  | 72 h                      | Weekly, Sun 04:00   |

Window values are exposed as env vars on
[`maintenance/expire-snapshots-and-orphans.yaml`](maintenance/expire-snapshots-and-orphans.yaml)
(`ICEBERG_RETENTION_HOT_HOURS=168`,
`ICEBERG_RETENTION_GOLD_HOURS=2160`) so they can be tuned without rebuilding
the maintenance image. The CronJob picks the tier at runtime via the
`ICEBERG_TIER` env var (`hot` or `gold`) — see
[`cronjob-expire-snapshots-and-orphans.yaml`](maintenance/cronjob-expire-snapshots-and-orphans.yaml).

### 4.3 Compaction policy

Run **daily** (02:30 UTC) using `Actions.rewriteDataFiles`:

* Target file size: **128 MiB** (`134217728` bytes).
* Compression: **ZSTD** level 3.
* `min-input-files=5` — never bother rewriting tables with only a handful of
  files.
* `partial-progress.enabled=true` with `max-commits=10` — long compactions
  publish progress incrementally so a transient failure does not lose all the
  work done so far.

### 4.4 Orphan-file cleanup

`Actions.deleteOrphanFiles(table).olderThan(now - 72h)` runs as part of the
expiration job. The 72 h guard is well above the longest legitimate writer
checkpoint window (CDC: 60 s; compaction: ≤ 6 h) so an in-flight writer can
never have its uncommitted files mistaken for orphans.

---

## 5. Validation

```bash
# YAML lint (uses the default ruleset; tabs disallowed, line length 120)
yamllint infra/k8s/platform/manifests/flink

# Server-side validation against a live cluster (recommended)
kubectl apply --server-side --dry-run=server \
    -f infra/k8s/platform/manifests/flink/maintenance/

# Client-side validation when no cluster is reachable. The FlinkDeployment
# manifests are valid YAML, but `kubectl --dry-run=client --validate=true`
# will reject them unless the FlinkDeployment CRD is installed.
kubectl apply --dry-run=client --validate=false \
    -f infra/k8s/platform/manifests/flink/maintenance/
```
