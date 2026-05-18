# dataset-versioning-service (Go)

## LLM quick context (current code)

Owns datasets, versions, branches, transactions, files, schemas, views, quality, and compatibility aliases for dataset APIs.

Agent note: large dataset backend; do not assume it is only a CRUD service.

Current surface:
- `/api/v1/datasets*`
- `/api/v2/datasets* compatibility surface`
- `branches/transactions/files/schema/views endpoints`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `27` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `backingfs`, `config`, `domain`, `handlers`, `models`, `repo`, `runtime`, `server`.
- Local service files present: `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `DATABASE_URL`, `DATASET_FILES_BASE_DIR`, `DATASET_FILES_BASE_URL`, `HOST`, `JWT_SECRET`, `METRICS_ADDR`, `OPENFOUNDRY_JWT_SECRET`, `PORT`
- `RETENTION_WORKER_ENABLED`, `RETENTION_WORKER_INTERVAL`, `SERVICE_VERSION`

Keep this section in sync when changing routes, config, or persistence behavior.

This Go service is the incremental port of the Rust `dataset-versioning-service`.

## Port status

Implemented end-to-end verticals:

- Dataset CRUD (`/api/v1/datasets`).
- Dataset versions (`/api/v1/datasets/{id}/versions`).
- Dataset branches (`/api/v1/datasets/{id}/branches`).
- Files API + backing filesystem presigning:
  - `GET /api/v1/datasets/{id}/files` lists the persisted logical path to physical URI mappings from `dataset_files`, with branch cutoff and prefix filtering.
  - `GET /api/v1/datasets/{id}/files/{file_id}/download` returns a temporary backing-filesystem URL for active files and rejects soft-deleted files with `410 Gone`.
  - `POST /api/v1/datasets/{id}/transactions/{txn}/files` validates an OPEN transaction and returns a transaction-scoped presigned upload URL plus the canonical `physical_uri`.
  - The default production wiring uses the local backing filesystem presigner in `libs/storage-abstraction`; set `DATASET_FILES_BASE_URL` and `DATASET_FILES_BASE_DIR` to control generated URLs.

Still pending after this slice:

- Dataset quality persistence/API.
- Views API and view-specific preview/schema endpoints.
- Retention worker physical purge loop.
- Full Iceberg writer/catalog integration beyond local backing-file presigning.
