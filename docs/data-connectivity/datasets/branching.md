# Datasets — Branching

Branches are isolated views of a dataset's history. They share storage (file paths), but each branch keeps its own pointer to a `head_transaction_id`; commits on one branch never alter another's view.

## Default branch

Every dataset is created with a single branch named **`master`**. Subsequent branches inherit from a parent (defaulting to `master`) and start with the parent's HEAD as their initial pointer.

## Creating a branch

```http
POST /v1/datasets/{rid}/branches
Content-Type: application/json

{
  "name": "feature/new-schema",
  "parent_branch": "master",
  "description": "trial new column types"
}
```

Two parent-selection strategies are mutually exclusive:

* **`parent_branch`** — the new branch tracks the parent's current HEAD.
* **`from_transaction`** — pin the new branch to a specific historical transaction (its branch becomes the parent).

Omit both for a **root branch** (HEAD = NULL until the first commit).

## Reparenting

```http
POST /v1/datasets/{rid}/branches/{branch}:reparent
Content-Type: application/json

{ "new_parent_branch": "master" }
```

Pass `null` (or omit the field) to make the branch a root. The branch may not become its own parent.

## Soft-deletion

```http
DELETE /v1/datasets/{rid}/branches/{branch}
```

Children are re-parented to the branch's grandparent (which may be NULL → they become roots) before the branch is marked `deleted_at`. The unique-name slot on the dataset is freed via the partial unique index `uq_dataset_branches_dataset_id_name_active`, so the same name can be reused immediately.

## Fallback chains

Each branch may declare an ordered list of fallback branches; readers walk the chain when the branch itself is missing data for a path.

```http
PUT /v1/datasets/{rid}/branches/{branch}/fallbacks
Content-Type: application/json

{ "chain": ["staging", "master"] }
```

A fallback resolution emits `dataset_branch_fallback_resolutions_total{outcome="hit|miss"}`.

## Authorization

All four mutating endpoints (`create_branch`, `delete_branch`, `:reparent`, `PUT /fallbacks`) require `dataset.write`. Read endpoints (`GET .../branches`, `GET .../branches/{branch}`) are open to any authenticated caller.

Denials are counted in `dataset_rbac_denials_total{operation=…}` with operation labels:

* `branch.create`
* `branch.delete`
* `branch.reparent`
* `branch.fallbacks.update`

## Audit

Each successful mutation emits a structured audit record under `target = "audit"`:

| action                      | extra fields                                       |
| --------------------------- | -------------------------------------------------- |
| `branch.create`             | `branch`, `parent_branch_id`, `initial_head`       |
| `branch.delete`             | `branch`, `branch_id`                              |
| `branch.reparent`           | `branch`, `new_parent_id`                          |
| `branch.fallbacks.update`   | `branch`, `chain`                                  |

## See also

* [transactions.md](./transactions.md)
* [retention.md](./retention.md)
