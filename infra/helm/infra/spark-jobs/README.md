# SparkApplication catalogue

`SparkApplication` CRs that target the Iceberg lakehouse. The operator
itself is in [../spark-operator/](../spark-operator/).

## Allowlist for snapshot-mutating jobs

The audit namespace `of_audit.*` is WORM (S5.1.c). The
`iceberg-expire-snapshots.yaml` job carries an explicit allowlist of
namespaces it is allowed to touch:

```
of_lineage, of_ai, of_metrics_long
```

Any operator who edits the allowlist to include `of_audit` violates the
audit policy and must be reverted.

## Catalogue

| File | What | Schedule |
|------|------|----------|
| `iceberg-rewrite-data-files.yaml` | Compact small files into 256MB targets across the allowlist namespaces. | weekly Sun 03:00 UTC |
| `iceberg-expire-snapshots.yaml` | Drop snapshots older than per-namespace retention (90d/1y) — never against `of_audit`. | weekly Sun 04:00 UTC |
| `metrics-aggregation-service-daily.yaml` | Read Mimir TSDB blocks from S3, aggregate per-service per-day, append to `of_metrics_long.service_metrics_daily`. | daily 02:00 UTC |
| `_pipeline-run-template.yaml` | **Template** (not rendered by Helm — leading underscore). Loaded by `pipeline-build-service`, `${...}`-substituted, and POSTed to the K8s API as one `SparkApplication` per pipeline run. See [`README-pipeline-run.md`](README-pipeline-run.md). | per pipeline run |

## WORM protection

Every job in this directory MUST:

1. Embed the allowlist as the FIRST CLI argument (so the alert
   `SparkApplicationTargetsWormNamespace` in the Prometheus rules can
   detect violations from operator metrics).
2. Run with the dedicated service account
   `spark-jobs-non-audit` whose RBAC denies write to objects bearing
   the `openfoundry.io/worm: "true"` label.
3. Be reviewed by both the data-platform and security squads on PR.
