# Datasets — Retention

Retention policies decide which dataset files (and which database rows) the platform may delete, and when. Owned end-to-end by `lineage-deletion-service`; `data-asset-catalog-service` and `dataset-versioning-service` only mark candidates and surface metrics.

## Built-in policies

### `DELETE_ABORTED_TRANSACTIONS`

The only system policy currently shipped. Sweeps file paths staged by transactions that ended in `ABORTED`:

1. List `dataset_transactions` with `status = 'ABORTED'` whose age exceeds the policy's grace period (default: 24 h).
2. For each, enumerate the staged paths (`ADD` ops never visible in any committed view).
3. Issue object-store deletes via `storage-abstraction`.
4. Emit a `retention.delete` audit record per file (see `services/lineage-deletion-service/src/domain/audit_emitter.rs`).
5. Increment `dataset_retention_files_deleted_total` and `dataset_retention_bytes_freed_total`.

The job runs as part of the lineage-deletion-service scheduler. Manual re-runs are exposed via `POST /v1/retention/jobs/aborted-transactions:run` (admin-only).

## Custom policies

Custom retention policies (e.g. "delete dataset files >365 d for projects in finance/test") are declared as `retention_policies` rows. The lineage-deletion-service evaluator reads them, materialises a candidate set, and applies the same delete + audit + metric pipeline.

> Today only `DELETE_ABORTED_TRANSACTIONS` ships out of the box; custom-policy authoring is documented separately in [security-governance/index.md](../../security-governance/index.md).

## Authorization

* **Configuring** retention policies (POST/PUT/DELETE) requires the `dataset.admin` scope.
* **Triggering** a manual run requires admin.
* The scheduled execution path runs as the service identity and is exempt from per-call RBAC.

## Audit

The retention pipeline emits structured `tracing::info!(target = "audit", …)` events that the `audit-compliance` collector ingests:

* `retention.delete` — one per file removed, with `dataset_rid`, `path`, `bytes`, `reason` (e.g. `aborted_transaction`).
* `retention.policy.update` — when a custom policy is mutated.
* `retention.run.start` / `retention.run.end` — wraps each scheduler tick.

## Observability

| Metric                                       | Type    | What it counts                              |
| -------------------------------------------- | ------- | ------------------------------------------- |
| `dataset_retention_files_deleted_total`      | counter | files removed by any retention policy        |
| `dataset_retention_bytes_freed_total`        | counter | bytes reclaimed                             |

## Operational notes

* The catalog and versioning services do **not** delete files themselves; they only update DB rows and emit events. Every physical delete is owned by `lineage-deletion-service` so there is exactly one place to reason about object-store consistency.
* Aborted transactions whose files are still being deleted appear as `status = 'ABORTED'` rows; metric drift between commit/abort counts and the retention deletion rate is the canonical signal that the deletion job has fallen behind.

## See also

* [transactions.md](./transactions.md)
* [branching.md](./branching.md)
* [markings.md](./markings.md)
