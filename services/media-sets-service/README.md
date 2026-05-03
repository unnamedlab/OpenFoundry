# media-sets-service

Foundry-style **media sets**: unstructured media storage with branches,
transactional write batches, path deduplication, retention windows, and
virtual (external-source) media sets.

The service exposes both REST (Axum) and gRPC (Tonic) on the same
process. Contracts live in `proto/media_set/`. Bytes ride on an
S3-compatible backend through `libs/storage-abstraction`.

## End-to-end flow (source → media set → pipeline → output)

The full life-cycle for a Foundry-style media workflow spans four
services. `media-sets-service` only owns the middle two; the others are
linked here so a reader can chase the contracts without grepping.

```
                                               ┌──────────────────────────────┐
                                               │ pipeline-authoring-service   │
                                               │  (P1.4 nodes)                │
[external S3 / ABFS]                            ├──────────────────────────────┤
        │                                       │ MediaSetInputNode            │
        │ enumerate                             │   ↓                          │
        ▼                                       │ MediaTransform (resize,      │
┌─────────────────────────────┐                 │   ocr, transcribe, …)        │
│ connector-management-svc    │                 │   ↓                          │
│  domain::media_set_sync     │                 │ MediaSetOutputNode  ─────────┼──┐
│   classify + dispatch       │                 │   or                         │  │
│   (MEDIA_SET_SYNC          ─┼─POST /upload-url│ ConvertMediaSetToTableRows ──┼──┼─→ dataset
│    | VIRTUAL_…)             │                 │   or                         │  │
└─────────────────────────────┘                 │ GetMediaReferences (dataset  │  │
        │ POST /virtual-items                   │   → media refs) ─────────────┼──┘
        ▼                                       └──────────────────────────────┘
┌─────────────────────────────┐                              ▲
│  media-sets-service         │                              │
│   media_sets / media_items  │ ─── presigned download ──────┘
│   transactions              │
│   retention reaper          │
│   virtual-set source        │
│     resolution (→ connector │
│     management for endpoint)│
└─────────────────────────────┘
```

End-to-end, each box owns a small slice of the story:

1. **`connector-management-service`** persists a `media_set_syncs` row
   (`POST /sources/{rid}/media-set-syncs`) per Foundry "Set up a media
   set sync" doc. Two flavours:
   - `MEDIA_SET_SYNC` posts each accepted source file to
     `POST /media-sets/{rid}/items/upload-url` here so bytes land in
     Foundry storage.
   - `VIRTUAL_MEDIA_SET_SYNC` posts to
     `POST /media-sets/{rid}/virtual-items` so only the metadata
     pointer lives in Foundry; bytes stay in the source bucket.
   See [`services/connector-management-service/src/domain/media_set_sync.rs`](../connector-management-service/src/domain/media_set_sync.rs)
   for the per-file decision logic and Foundry filter taxonomy.
2. **`media-sets-service`** (this crate) holds the source-of-truth
   tables (`media_sets`, `media_items`, `media_set_transactions`),
   issues presigned upload/download URLs, applies path-dedup, runs the
   retention reaper, and resolves virtual-set download URLs by looking
   up source endpoint metadata in `connector-management-service`.
3. **`pipeline-authoring-service`** (P1.4) registers Pipeline Builder
   node kinds that consume / produce media items:
   - `MediaSetInputNode` reads items or paths from a media set.
   - `MediaTransformNode` (kinds: `extract_text_ocr`, `resize`,
     `rotate`, `crop`, `transcribe_audio`, `generate_embedding`,
     `render_pdf_page`, `extract_layout_aware`) emits either derived
     media items or dataset rows depending on the kind.
   - `MediaSetOutputNode` writes derived items back into an existing
     or freshly-created media set, honouring the Foundry write-mode
     contract (TRANSACTIONLESS sets accept only `modify`).
   - `ConvertMediaSetToTableRows` flattens a media set into dataset
     rows (one per item) so downstream SQL nodes can join media
     metadata against tabular data.
   - `GetMediaReferences` walks a dataset of files and registers each
     into a target media set, returning rows of media references.
4. The expressions `IsValidMediaReference` and
   `ConstructDelegatedMediaGid`
   ([`services/pipeline-authoring-service/src/domain/expressions.rs`](../pipeline-authoring-service/src/domain/expressions.rs))
   let any node validate / construct the canonical Foundry media
   reference JSON, including the `delegatedMediaGid` URL the OSDK uses
   to address an item from outside Foundry.

The runtime stub in
[`pipeline-authoring-service/src/domain/engine/runtime.rs`](../pipeline-authoring-service/src/domain/engine/runtime.rs)
will eventually delegate to a future `media-transform-runtime`
service. P1.4 only wires the **authoring** + **wire-format** + **type
validation** halves; the executor returns a deterministic stub envelope
so downstream stages can be authored against a stable shape.

## Surfaces

| Surface | Default port | Notes                                          |
|---------|--------------|------------------------------------------------|
| REST    | `50156`      | JWT-authenticated except `/healthz`/`/metrics` |
| gRPC    | `50157`      | `MediaSetService` (see `proto/media_set/`)     |

## Endpoints (REST)

| Method | Path                                            | Notes                                                  |
|--------|-------------------------------------------------|--------------------------------------------------------|
| `POST` | `/media-sets`                                   | Create.                                                |
| `GET`  | `/media-sets?project_rid=…`                     | List.                                                  |
| `GET`  | `/media-sets/{rid}`                             | Get one.                                               |
| `DELETE` | `/media-sets/{rid}`                           | Cascades to branches, transactions, items.             |
| `PATCH` | `/media-sets/{rid}/retention`                  | Body `{ "retention_seconds": n }` — runs reaper inline.|
| `POST` | `/media-sets/{rid}/transactions`                | Open. Only on `TRANSACTIONAL` sets.                    |
| `POST` | `/transactions/{rid}/commit`                    | Seal.                                                  |
| `POST` | `/transactions/{rid}/abort`                     | Discard staged items.                                  |
| `POST` | `/media-sets/{rid}/items/upload-url`            | Presigned PUT — applies path dedup.                    |
| `POST` | `/media-sets/{rid}/virtual-items`               | Register an external item; no bytes copied.            |
| `GET`  | `/media-sets/{rid}/items?branch&prefix&limit&cursor` | List live items.                                  |
| `GET`  | `/items/{rid}`                                  | Get one (deleted items remain reachable by RID).       |
| `GET`  | `/items/{rid}/download-url`                     | Virtual sets resolve via `connector-management-service`.|
| `DELETE` | `/items/{rid}`                                | Soft-delete + best-effort byte cleanup.                |

## Retention

Implements the contract documented in
*Advanced media set settings → Retention policies*:

- `retention_seconds = 0` ⇒ retain forever.
- Reducing the window via `PATCH /media-sets/{rid}/retention` makes
  newly-out-of-window items inaccessible *immediately* — the handler
  runs a one-shot reaper on the affected set before returning.
- Expanding the window (or switching to `0`) does **not** restore
  items that already expired: the schema only allows
  `deleted_at` transitions `NULL → NOW()`; it never goes the other way.
- A periodic background task (`RETENTION_REAPER_SECS`, default 300 s)
  re-scans every set and soft-deletes items whose live `created_at +
  media_sets.retention_seconds` is in the past, then drops the bytes
  from the backing store and emits one `audit` tracing event per
  expired item. The full audit-bus wiring is reserved for H3.

### Transactional retention floor

Reductions on `TRANSACTIONAL` media sets are rejected when the new
window is below `TRANSACTIONAL_RETENTION_FLOOR_SECONDS` (60 s today).
This is a guardrail — `TODO`: replace with the published platform SLO
once `services/sds-service` defines `media-set.retention.min_seconds`.

## Virtual media sets

A virtual media set keeps the metadata in Foundry while the bytes stay
in an external source system addressed by `source_rid`. Create one by
passing `virtual: true` + `source_rid: "<connector source rid>"` to
`POST /media-sets`. Items are registered via
`POST /media-sets/{rid}/virtual-items` (`physical_path`, `item_path`),
which writes a `media_items` row whose `storage_uri` points at the
external location — no bytes are copied into Foundry.

Download URLs are produced by `GET /items/{rid}/download-url`. For
virtual items, the handler queries `connector-management-service`
(`GET /sources/{source_rid}`) to discover the source endpoint and
synthesises a presigned URL pointing at the external system. When
`CONNECTOR_SERVICE_URL` is unset or the upstream call fails, the
endpoint returns **HTTP 503** with a clear error message — by design,
the service refuses to fabricate URLs that would not actually reach
the bytes.

### Limitations (per Foundry docs)

These are documented in
`docs_original_palantir_foundry/.../Virtual media sets.md`:

1. **No awareness of external deletes.** If a file is removed from the
   source system, the virtual media set still surfaces a
   `media_items` row for it — but the download URL will fail at the
   external endpoint (the registry has no callback to learn about
   external deletions).
2. **Transformations are persisted in Foundry.** When a transform
   produces a derived artifact from a virtual item, the derived bytes
   land in Foundry's backing store and incur normal storage costs. The
   source remains read-only.
3. **No additional input formats.** Virtual sets ignore the
   `allowed_mime_types` extension knob; the source system's native
   types are the only accepted formats.

These are upstream contract limits; the handlers do not attempt to
mask them.

## Configuration

Loaded via [`config`] from `config/{default,prod}.toml` plus
`__`-separated environment overrides.

| Env var (or TOML key)            | Default                       | Purpose                                                      |
|----------------------------------|-------------------------------|--------------------------------------------------------------|
| `HOST`                           | `0.0.0.0`                     | REST + gRPC bind address.                                    |
| `PORT`                           | `50156`                       | REST port.                                                   |
| `GRPC_PORT`                      | `port + 1`                    | Tonic port.                                                  |
| `DATABASE_URL`                   | required                      | Postgres writer URL.                                         |
| `JWT_SECRET`                     | required                      | HS256 secret for `auth-middleware`.                          |
| `STORAGE_BUCKET`                 | `media`                       | Object-store bucket.                                         |
| `STORAGE_ROOT`                   | `/tmp/openfoundry-media`      | Local filesystem root (dev backend only).                    |
| `STORAGE_ENDPOINT`               | empty                         | Public endpoint baked into presigned URLs.                   |
| `PRESIGN_TTL_SECONDS`            | `3600`                        | Default presign TTL.                                         |
| `RETENTION_REAPER_SECS`          | `300`                         | Background reaper interval.                                  |
| `CONNECTOR_SERVICE_URL`          | unset                         | Required for virtual-set download URL resolution.            |

[`config`]: https://docs.rs/config/

## Layout

```
src/
  config.rs            # AppConfig (env + TOML).
  domain/
    dedup.rs           # Path-dedup primitive.
    error.rs
    path.rs            # {bucket}/media-sets/{rid}/{branch}/{sha256[:2]}/{sha256}
    retention.rs       # Reaper + audit emission.
    storage.rs         # MediaStorage trait + BackendMediaStorage.
  grpc.rs              # Tonic MediaSetService impl (delegates to *_op).
  handlers/
    health.rs          # /healthz, /metrics.
    items.rs           # CRUD + presigned URLs + virtual register/resolve.
    media_sets.rs      # CRUD + PATCH retention.
    transactions.rs    # Open / commit / abort.
  metrics.rs           # Prometheus families.
  models/              # Postgres rows + REST DTOs.
  proto.rs             # `tonic::include_proto!` glue.
migrations/
  0001_media_sets.sql
  0002_media_items.sql
  0003_retention.sql
tests/                 # Integration tests (testcontainers Postgres).
```
