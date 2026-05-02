# Datasets — Markings

Markings are the access-control tags attached to a dataset (e.g. `pii`, `confidential`, `restricted`). The effective set drives both the UI's classification badge and the runtime clearance gate.

## Sources

A row in `dataset_markings` carries one of two `source` values:

* **`direct`** — explicitly attached to the dataset.
* **`inherited`** — projected from an upstream dataset (the resolver re-derives these on the fly; they are not authoritative in the table).

The cached resolver — [`MarkingResolver`](../../../services/data-asset-catalog-service/src/domain/markings.rs) — composes the effective list as:

```
effective(rid) = direct(rid)
              ∪ ⋃ effective(parent)  for parent in lineage.upstream(rid)
```

Cycles in the lineage graph are detected (`MarkingResolveError::Cycle`) and dropped from the union.

## Resolver caching

* TTL: **60 s** by default (`MarkingResolver::new`).
* Capacity: 10 000 RIDs.
* Invalidation: per-RID via `invalidate(rid)`; bulk via `invalidate_all`. The data-asset-catalog wires the per-RID invalidator to the lineage event bus so an upstream change ripples within a single TTL window worst-case.

## Reading effective markings

Internal callers ask the resolver:

```rust
let effective: Arc<Vec<EffectiveMarking>> = state
    .marking_resolver
    .compute(dataset_rid)
    .await?;
```

External read endpoints (`GET /v1/datasets/{id}` and `GET /internal/datasets/{id}`) return the **direct** marking IDs only; the inherited set is computed at the access-decision boundary, not surfaced as a stable list to clients.

## Attaching / detaching

A future `POST/DELETE /v1/datasets/{rid}/markings/{marking_id}` pair will mutate `dataset_markings` directly. The endpoints will require the `dataset.admin` scope (gated by `crate::security::require_dataset_admin`), separate from `dataset.write`, so an operator with bulk-upload rights cannot silently elevate a dataset's classification.

## History

Earlier revisions derived a single string marking from the dataset's tag list (`marking:pii`, `classification:confidential`, …) via a helper called `marking_from_tags`. **T9.1 removed it.** `dataset_markings` is now the single source of truth and there is no fallback path.

## Enforcement

Read paths consult the caller's `Claims::allowed_markings()` / `allows_marking(s)` before returning rows. A denial increments `dataset_marking_enforcement_denials_total`. Today the gate runs in-process; the same shape will accept a remote check against `authorization-policy-service` once its dataset bundle is shipped.

## Observability

| Metric                                          | Type    | When it moves                                             |
| ----------------------------------------------- | ------- | --------------------------------------------------------- |
| `dataset_marking_enforcement_denials_total`     | counter | a marking gate rejected a read                            |
| `dataset_rbac_denials_total{operation="*"}`     | counter | RBAC scope check failed                                   |

## See also

* [transactions.md](./transactions.md)
* [retention.md](./retention.md)
