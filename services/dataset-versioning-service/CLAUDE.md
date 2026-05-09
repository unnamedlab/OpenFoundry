# CLAUDE.md — services/dataset-versioning-service

Dataset CRUD, branches, transactions, files, views, retention. The
biggest single-service body of code in this repo (~16k LOC).

## Where to look first

| Concern | Open this |
|---|---|
| Dataset/branch/transaction CRUD | `internal/handlers/handlers.go` |
| Catalog browsing (facets, lists) | `internal/handlers/catalog_parity.go` |
| Branch lifecycle (create/merge/delete) | `internal/handlers/branch_lifecycle.go` |
| Files API + presign | `internal/handlers/backing_files.go` + `internal/backingfs/` |
| Transaction commits | `internal/handlers/transactions.go` |
| Views (parameterized + schema preview) | `internal/handlers/views_schema_preview.go`, `internal/domain/views.go`, `internal/domain/parameterized_view.go` |
| Retention worker | `internal/runtime/retention/worker.go` |
| Wire types | `internal/models/models.go` |
| Postgres queries | `internal/repo/repo.go` |

## Files to handle with care

| File | Lines | Notes |
|---|---:|---|
| `internal/repo/parity.go` | 2356 | Catalog facets, metadata, markings, permissions, lineage, file index. Six concerns in one file — navigate by `grep -n 'func '` |
| `internal/models/models.go` | 1368 | All wire types |
| `internal/repo/repo.go` | 819 | Core repo |
| `internal/handlers/handlers.go` | 819 | Mixed handlers |
| `internal/handlers/catalog_parity.go` | 806 | Catalog facets HTTP |
| `internal/handlers/handlers_test.go` | 1948 | Mixed test file — when adding tests, prefer creating a new `*_test.go` named for the feature instead of growing this one |

## Conventions specific to this service

- **Pagination:** uses `models.Page[T]` with `next_cursor` (not page numbers).
- **Generic envelope:** legacy endpoints return `models.ListResponse[T]`
  (`{ "items": [...] }`); new endpoints use `Page[T]`. Keep the existing
  shape for an endpoint — don't switch envelopes mid-flight.
- **Sentinel errors** in `internal/repo/parity.go`:
  `ErrNotFound`, `ErrPreconditionFailed`, `ErrInvalidTransition`,
  `ErrValidation`. Map them in handlers, don't return raw repo errors.
- **Branch cutoff** is the dominant filter pattern across files API and
  catalog. Whatever you write: respect the active branch's transaction
  visibility window.
- **Transaction state machine** is OPEN → COMMITTED | ABORTED. Files
  can only be added to OPEN. Committed transactions are immutable.
  See `internal/repo/transaction_invariants_test.go`.

## Backing filesystem

The default production wiring uses the local backing filesystem
presigner in `libs/storage-abstraction`. Configure with
`DATASET_FILES_BASE_URL` and `DATASET_FILES_BASE_DIR`. Real S3/Ceph
wiring lives in the same lib — switch via config, not by editing
handlers.

Soft-deleted files return **`410 Gone`**, not 404. Don't change that.

## Iceberg catalog status

Full Iceberg writer/catalog integration is not in this service yet —
`iceberg-catalog-service/` owns it. This service deals with logical
paths and presigned URIs only.

## Testing

```sh
go test ./services/dataset-versioning-service/...
go test -tags integration ./services/dataset-versioning-service/...   # needs Docker
```

The `handlers_test.go` (1948 lines) covers the legacy surface; new
tests should be split per feature.

## Migrations

`internal/repo/migrations/` (goose). Once a migration ships, treat it
as immutable. Schema for the dataset/branch/transaction model is the
*hot path* for the platform — coordinate before adding heavy indices.
