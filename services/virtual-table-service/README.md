# virtual-table-service

First-class catalog and orchestration for **Foundry virtual tables** — pointers
to tables in supported data platforms that Foundry queries on demand instead of
copying. Source of truth for the contract:

> `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Core concepts/Virtual tables.md`

This README is organised by Foundry doc section so you can map each feature in
the platform doc to the code that implements it.

---

## Doc § "Supported sources"

| Foundry source | Provider enum | Module                                            | Status                       |
| -------------- | ------------- | ------------------------------------------------- | ---------------------------- |
| Amazon S3      | `AMAZON_S3`   | `connectors/s3.rs` + `domain/iceberg_catalogs/`   | P1 stub, P2 enforcement done |
| Azure ADLS     | `AZURE_ABFS`  | `connectors/azure_blob.rs`                        | P1 stub                      |
| BigQuery       | `BIGQUERY`    | `connectors/bigquery.rs`                          | P1 stub, P2 capability map   |
| Databricks     | `DATABRICKS`  | `connectors/databricks.rs` (P2)                   | Skeleton + capability detect |
| Foundry        | `FOUNDRY_ICEBERG`                                                                  |
| GCS            | `GCS`         | `connectors/gcs.rs`                               | P1 stub                      |
| Snowflake      | `SNOWFLAKE`   | `connectors/snowflake.rs`                         | P1 stub, P2 capability map   |

The full provider × table-type capability matrix is encoded in
`src/domain/capability_matrix.rs::MATRIX` and asserted against the doc by
`tests/capability_matrix_matches_doc.rs`. Drift between the doc and the matrix
breaks that test.

---

## Doc § "Iceberg catalogs"

Five catalog kinds are supported, each behind its own module under
`src/domain/iceberg_catalogs/`:

| Kind             | Module               | Live SDK behind feature   |
| ---------------- | -------------------- | ------------------------- |
| `AWS_GLUE`       | `aws_glue.rs`        | `provider-iceberg`        |
| `HORIZON`        | `horizon.rs`         | `provider-iceberg`        |
| `OBJECT_STORAGE` | `object_storage.rs`  | always on                 |
| `POLARIS`        | `polaris.rs`         | `provider-iceberg`        |
| `UNITY_CATALOG`  | `unity.rs`           | `provider-iceberg`        |

The (provider × catalog) compatibility matrix lives in
`iceberg_catalogs::compatibility` and is asserted against the doc by
`tests/iceberg_catalog_compat_matrix_matches_doc.rs`. Configure the catalog on
a source via `POST /v1/sources/{rid}/iceberg-catalog` (handler:
`handlers::virtual_tables::set_iceberg_catalog`).

---

## Doc § "Set up a connection for a virtual table" (img_002, img_003)

UI tab lives in [`apps/web/src/lib/components/data-connection/VirtualTablesTab.svelte`](../../apps/web/src/lib/components/data-connection/VirtualTablesTab.svelte)
and is rendered conditionally from
[`/data-connection/sources/[id]/+page.svelte`](../../apps/web/src/routes/data-connection/sources/[id]/+page.svelte)
when the source's `connector_type` resolves to one of the seven
`VirtualTableProvider` values.

Backend endpoints (mounted at `/v1` from `main.rs`):

| Method | Path                                                | Purpose                          |
| ------ | --------------------------------------------------- | -------------------------------- |
| POST   | `/sources/{rid}/virtual-tables/enable`              | Idempotent enable                |
| DELETE | `/sources/{rid}/virtual-tables/enable`              | Disable                          |
| GET    | `/sources/{rid}/virtual-tables/discover?path=`      | Browse remote catalog            |
| POST   | `/sources/{rid}/virtual-tables/register`            | Single-entry register            |
| POST   | `/sources/{rid}/virtual-tables/bulk-register`       | Bulk register                    |
| POST   | `/sources/{rid}/iceberg-catalog`                    | Configure Iceberg catalog kind   |
| GET    | `/virtual-tables`                                   | Paginated list (cursor)          |
| GET    | `/virtual-tables/{rid}`                             | Detail                           |
| DELETE | `/virtual-tables/{rid}`                             | Delete (cascades imports)        |
| PATCH  | `/virtual-tables/{rid}/markings`                    | Update markings                  |
| POST   | `/virtual-tables/{rid}/refresh-schema`              | Re-run schema inference          |

---

## Doc § "Limitations of using virtual tables"

The five "not supported" rules from the doc are enforced **before** the
register call hits the database. Reference:
`src/domain/source_validation.rs`. Failures return a structured 412
`VIRTUAL_TABLES_INCOMPATIBLE_SOURCE_CONFIG` with a stable `code` field plus a
remediation hint:

| Rule                                                     | Code                                          |
| -------------------------------------------------------- | --------------------------------------------- |
| Agent worker sources                                     | `AGENT_WORKER_NOT_SUPPORTED`                  |
| Agent proxy egress policies                              | `AGENT_PROXY_EGRESS_NOT_SUPPORTED`            |
| Bucket endpoint egress policies                          | `BUCKET_ENDPOINT_EGRESS_NOT_SUPPORTED`        |
| Self-service private link egress (operator-provisioned OK) | `SELF_SERVICE_PRIVATE_LINK_NOT_SUPPORTED`   |
| Source not found in connector-management-service         | `SOURCE_NOT_FOUND`                            |

Strict mode is on by default in production
(`AppConfig::strict_source_validation = true`); set it to `false` in
integration tests that bypass the upstream service.

Each rejection bumps the Prometheus counter
`virtual_table_source_validation_failures_total{reason}` so SREs can alert on
misconfigured upstreams without parsing log text.

---

## Doc § "Virtual table compatibility matrix by source & table type"

Encoded as a closed table in `src/domain/capability_matrix.rs::MATRIX`. Each
row carries:

* `read` / `write` / `incremental` / `versioning`
* `compute_pushdown_engine`: `Ibis` (BigQuery), `PySpark` (Databricks),
  `Snowpark` (Snowflake) or `None` (object stores)
* `foundry_compute`: `python_single_node`, `python_spark`,
  `pipeline_builder_single_node`, `pipeline_builder_spark`

UI mirrors the matrix in
[`apps/web/src/lib/api/virtual-tables.ts::defaultCapabilities`](../../apps/web/src/lib/api/virtual-tables.ts)
so the inspector / create modal can preview capabilities before the row hits
the backend.

---

## Doc § "Update detection for virtual table inputs"

Schema columns:

* `update_detection_enabled BOOLEAN`
* `update_detection_interval_seconds INT`
* `last_observed_version TEXT`  (snapshot id for Iceberg / Delta; ETag for object stores)
* `last_polled_at TIMESTAMPTZ`

The poller is a no-op stub in P1 (`spawn_update_detection_poller` in
`main.rs`); P5 swaps the body for the real polling loop.

---

## Doc § "Configure objects backed by virtual tables" (Beta)

Wired in `services/ontology-definition-service` (D1.1.10). The handler in this
service exposes `GET /virtual-tables/{rid}` so the Ontology Manager can attach
an object type to a virtual table.

---

## Running the service

```bash
# Default profile — capability matrix + stubs only
cargo run -p virtual-table-service

# With provider feature flags (live SDKs land in P2.next)
cargo run -p virtual-table-service \
  --features provider-databricks,provider-iceberg
```

Key environment variables (see `src/config.rs`):

| Variable                                 | Default                       | Notes                                       |
| ---------------------------------------- | ----------------------------- | ------------------------------------------- |
| `DATABASE_URL`                           | required                      | Postgres connection string                  |
| `JWT_SECRET`                             | required                      | HS256 secret                                |
| `HOST` / `PORT`                          | `0.0.0.0:50089`               | HTTP listener                               |
| `GRPC_PORT`                              | `50189`                       | gRPC listener (`VirtualTableCatalog`)       |
| `CONNECTOR_MANAGEMENT_SERVICE_URL`       | `http://localhost:50090`      | Source-validation upstream                  |
| `STRICT_SOURCE_VALIDATION`               | `true`                        | Set to `false` in integration tests         |
| `MAX_BULK_REGISTER_BATCH`                | `500`                         | Soft cap for `/bulk-register`               |
| `AUTO_REGISTER_POLL_INTERVAL_SECONDS`    | `0` (disabled)                | P4 — auto-registration scanner              |
| `UPDATE_DETECTION_DEFAULT_INTERVAL_SECONDS` | `0` (disabled)            | P5 — update-detection poller                |

---

## Tests

```bash
# All integration tests (no live SDKs)
cargo test -p virtual-table-service \
  --features provider-databricks,provider-iceberg \
  -- --include-ignored
```

The test taxonomy:

* `capability_matrix_matches_doc.rs` — provider × table-type matrix
* `iceberg_catalog_compat_matrix_matches_doc.rs` — provider × catalog matrix
* `databricks_capability_detection.rs` — `DESCRIBE TABLE EXTENDED` heuristic
* `foundry_worker_only_enforcement.rs` — every "not supported" rule
* `locator_uniqueness_per_source.rs` — canonical locator for the unique index
* `type_mapping_round_trip.rs` — Arrow ↔ provider type mapping
* `discover_remote_catalog_returns_hierarchy.rs` — surface contract check
* `auto_registration_diff_and_mirror.rs` — P4 diff classification + Databricks tag filter
* `update_detection_classification.rs` — P5 version probe + classifier

---

## Foundry doc parity matrix (D1.1.9 5/5)

The full section-by-section parity matrix lives in
[`docs/architecture/adr/ADR-0040-virtual-tables-service.md`](../../docs/architecture/adr/ADR-0040-virtual-tables-service.md).
Headline status:

| Phase | Scope                                                              | Status |
| ----- | ------------------------------------------------------------------ | ------ |
| P1    | Persistence + endpoints + capability matrix + audit                | 🟢     |
| P2    | Foundry-worker / egress enforcement + Iceberg catalogs + Databricks skeleton | 🟢 |
| P3    | UI tab + global routes + Cedar entity                              | 🟢     |
| P4    | Auto-registration scanner + Databricks tag filter                  | 🟢     |
| P5    | Update detection poller + DATA_UPDATED outbox event                | 🟢     |
| P6    | Compute pushdown SDK + Code Repositories code-imports / export-controls | 🟢 |

**Deferred** (tracked separately): Ontology objects backed by virtual
tables (D1.1.10), Marketplace product virtual tables (D1.4.x),
Contour input wiring (D1.5.x).
