# ADR-0040 — Virtual tables service

**Status:** Draft (P3 — D1.1.9)
**Date:** 2026-05-04

## Context

Foundry exposes virtual tables as "pointers to tables in supported data
platforms outside Foundry" — the platform queries the source on demand
rather than syncing data into a Foundry dataset. The published
behaviour is documented in
`docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Core concepts/Virtual tables.md`,
which prescribes:

* a per-source **virtual tables tab** in the Data Connection app,
* a published **compatibility matrix** for seven providers and ten
  table types,
* support for five **Iceberg catalogs** (AWS Glue, Horizon, Object
  Storage, Polaris, Unity),
* hard limits — agent-worker sources, agent-proxy / bucket-endpoint /
  self-service private-link egress policies are **not** supported,
* a **schema details panel** with Incremental / Versioning toggles,
* an **update-detection poller** that triggers downstream builds when
  a source row has changed.

D1.1.9 P1 wired the persistence + a server skeleton with first-class
endpoints, and P2 added the Foundry-worker / egress enforcement plus
the Iceberg catalog trait + matrix. P3 lands the UI tab + global
list/detail routes + Cedar policy stub. This ADR captures the
load-bearing decisions taken across P1 → P3 so future contributors
can tell what is "doc-aligned by design" vs "implementation choice".

## Decisions

### D-1: Dedicated `virtual-table-service` (not an extension of `connector-management-service`)

**Decision.** Virtual tables ship in their own service binary
(`services/virtual-table-service`).

**Why.**

* The data model is materially different — `virtual_tables`
  rows reference but do not own the source row, and the unique index
  spans `(source_rid, locator)` (not `(source_id, table_name)`). Co-
  locating the table inside `connector-management-service` would
  conflate two bounded contexts.
* The Foundry doc treats virtual tables as a separate **resource type**
  with its own RID prefix (`ri.foundry.main.virtual-table.<uuid>`)
  and its own audit stream — splitting the service mirrors that.
* The Iceberg catalog dependencies (potentially `aws-sdk-glue`,
  `iceberg-rest`, `databricks-sql`) would inflate the
  `connector-management-service` binary even when the operator is not
  using virtual tables. Feature flags isolate the cost in a dedicated
  crate.

**Trade-off.** A second binary means a second migration set and a
second pod in the helmfile. Mitigated by sharing `libs/auth-middleware`,
`libs/db-pool`, `libs/audit-trail`, `libs/observability` so the
operational surface is identical.

### D-2: Capability matrix is the single source of truth

**Decision.** The provider × table-type compatibility matrix lives in
`src/domain/capability_matrix.rs::MATRIX` as a `const` array. Every
register call reads its capabilities from this table; the UI mirrors
the same matrix in TypeScript so previews are doc-aligned.

**Why.** The Foundry doc explicitly publishes the matrix, and silent
drift between the doc and the registered row would mean a Foundry
user sees the wrong "Write" badge in Pipeline Builder. The
integration test `capability_matrix_matches_doc.rs` asserts every
cell verbatim — a doc revision that flips a cell breaks CI until
the matrix is updated to match.

**Alternative considered.** Reading capabilities from the connector at
register time (e.g. introspect Snowflake `INFORMATION_SCHEMA` to find
the right slot). Rejected because the doc-blessed capability — not
the actual SQL endpoint — drives Foundry's downstream features
(Pipeline Builder, Code Repositories pushdown, Contour). The matrix
is the contract; live introspection only fills in the schema.

### D-3: Iceberg catalogs behind a trait + per-kind module

**Decision.** `domain/iceberg_catalogs/mod.rs` defines the
`IcebergCatalog` trait and a closed `compatibility(provider,
catalog) → CompatibilityStatus` function. Each of the five published
catalog kinds is its own module with config validation + a
deterministic stub body; live SDK calls land behind the
`provider-iceberg` cargo feature in P2.next.

**Why.** The Foundry doc lists each catalog as a separate row of the
"Iceberg catalogs" matrix, with cells that go beyond GA / N/A
(`Legacy: recommended to use Databricks source` for S3 + Unity and
ABFS + Unity). Encoding the matrix as a function with a typed status
enum means Cedar policies + the UI can branch on `Legacy` without
duplicating the table.

### D-4: Egress policy enforcement at the registration boundary

**Decision.** Before any `register_virtual_table` write, the service
calls `connector-management-service` to fetch the source's
`worker_kind` + `egress.kind` and applies the doc § "Limitations"
rules. Failures return a structured **412 PRECONDITION FAILED** with
a stable error code so the UI can route the user to the right
remediation step.

**Why.** The doc lists five hard "not supported" rules. Without
enforcement at the registration boundary, the UI would silently
register tables that downstream Pipeline Builder / Code Repositories
runs would then reject for opaque reasons. Surfacing the error at
register time + bumping
`virtual_table_source_validation_failures_total{reason}` makes
mis-configured sources alert-able.

**Strict mode** is on by default
(`AppConfig::strict_source_validation = true`) and disabled in
integration tests that bypass the upstream service.

### D-5: Cedar entity carries the capability slice

**Decision.** The `VirtualTable` Cedar entity carries the
`capabilities` struct (read / write / incremental / versioning)
verbatim, alongside `markings`, `provider`, `project_rid` and
`source_rid`.

**Why.** Cedar evaluates in-process per ADR-0027; carrying the
capability slice on the entity means a policy can gate
`virtual_table::write_data` on `resource.capabilities.write == true`
without a round-trip to the capability-matrix lookup. Markings are
inherited from the source as a clearance floor — the policy
evaluates `resource.markings ⊆ principal.allowed_markings` exactly
as for a Foundry dataset, so a virtual table never grants more
clearance than the source it points to.

**Action vocabulary** (cedar_schema.cedarschema):

* `virtual_table::view` — required for `GET /virtual-tables/{rid}`
* `virtual_table::register` — required for the `register` endpoint
* `virtual_table::delete` — required for `DELETE /virtual-tables/{rid}`
* `virtual_table::write_data` — required for transforms that write
  back into the source (External Delta / Managed Iceberg / BigQuery
  / Snowflake / object stores per the matrix)
* `virtual_table::manage_markings` — required for the `PATCH
  /markings` endpoint
* `virtual_table::import_to_project` — required when copying a
  virtual table from its auto-registration project into another
  Foundry project

## Consequences

* The matrix lives in three places (Rust `MATRIX`, TypeScript
  `defaultCapabilities`, doc-conformance tests). Drift is loud — the
  test fails, the type signatures diverge, and CI breaks. Worth the
  duplication for a contract this load-bearing.
* P2.next (live SDK integrations) is gated behind `provider-databricks`
  and `provider-iceberg` cargo features. CI runs the default profile so
  the service compiles and tests run on a developer laptop without
  AWS / Databricks / Snowflake credentials.
* Update detection (P5) and auto-registration (P4) are wired as
  no-op spawns in `main.rs`. Filling them in is a body-swap, not an
  architecture change.

## Status by phase

| Phase | Scope                                                                       | Status |
| ----- | --------------------------------------------------------------------------- | ------ |
| P1    | Persistence + handlers + capability matrix + audit                          | Done   |
| P2    | Foundry-worker enforcement + Iceberg catalogs + Databricks skeleton         | Done   |
| P3    | UI tab + global routes + Cedar entity                                       | Done   |
| P4    | Auto-registration scanner (Databricks tag filter, periodic scan)            | Done   |
| P5    | Update detection poller (Iceberg / Delta version detection)                 | Done   |
| P6    | Compute pushdown SDK + Code Repositories code-imports / export-controls     | Done   |

## Architecture flow (D1.1.9 5/5)

```mermaid
flowchart LR
  src[Data Connection<br/>source row<br/>(connector-management-service)]
  src --> validate[domain::source_validation<br/>doc § Limitations<br/>(412 PRECONDITION_FAILED)]
  validate --> matrix[domain::capability_matrix<br/>provider × table_type<br/>(closed table)]
  matrix --> register[domain::virtual_tables<br/>register / bulk_register]
  register --> row[(virtual_tables row<br/>+ virtual_table_audit)]

  subgraph reg ["Registration paths"]
    direction TB
    manual[manual<br/>(UI tab)] --> register
    bulk[bulk<br/>(BulkRegisterDialog)] --> register
    auto[auto<br/>(domain::auto_registration<br/>+ Databricks tag filter)] --> register
  end

  row --> ud[domain::update_detection<br/>tokio interval<br/>per-provider probe]
  ud -- DATA_UPDATED --> outbox[(virtual_table_audit<br/>topic foundry.dataset.events.v1)]
  outbox --> trigger[D1.1.6 trigger_engine<br/>EventTrigger { target_rid }]
  trigger --> schedule[downstream pipeline<br/>schedule run]

  row --> code[domain::code_imports<br/>code_imports_enabled<br/>+ export_controls<br/>+ use_external_systems block]
  code --> sdk[Python sdks/<br/>openfoundry_transforms.virtual_tables]
  sdk -- pushdown_to('ibis') --> bq((BigQuery<br/>via Ibis))
  sdk -- pushdown_to('pyspark') --> dbx((Databricks<br/>via PySpark))
  sdk -- pushdown_to('snowpark') --> sf((Snowflake<br/>via Snowpark))
  sdk -- pushdown_to('foundry-compute') --> arrow((Arrow stream<br/>local materialise))

  row --> iceberg[domain::iceberg_catalogs<br/>AwsGlue / Horizon / ObjectStorage<br/>/ Polaris / Unity]

  row --> ui[apps/web<br/>/virtual-tables + /[rid]<br/>+ Data Connection tab]
  ui --> details[VirtualTableDetailsPanel.svelte<br/>Incremental / Versioning / Update detection / Pushdown]
```

## Foundry doc parity matrix

Every section of the published Foundry doc is mapped to its
implementation file (or marked deferred). 🟢 Generally available;
🟡 Beta / partial; 🔴 deferred.

| Foundry doc section                                                                   | Status | Implementation                                                                                                           |
| ------------------------------------------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------ |
| Core concepts § "Supported sources"                                                   | 🟢     | `services/virtual-table-service/src/connectors/{bigquery,snowflake,s3,gcs,azure_blob,databricks}.rs`                     |
| Core concepts § "Iceberg catalogs"                                                    | 🟢     | `domain/iceberg_catalogs/{aws_glue,horizon,object_storage,polaris,unity}.rs` + `iceberg_catalog_compat_matrix_matches_doc.rs` |
| Core concepts § "Supported Foundry workflows"                                         | 🟢     | `services/pipeline-authoring-service/src/domain/virtual_table_nodes.rs` (Streaming + Faster pipelines blocked)            |
| Core concepts § "Virtual table compatibility matrix"                                  | 🟢     | `domain/capability_matrix.rs` + `tests/capability_matrix_matches_doc.rs`                                                  |
| Core concepts § "Set up a connection for a virtual table"                             | 🟢     | `apps/web/src/lib/components/data-connection/VirtualTablesTab.svelte`                                                    |
| Core concepts § "Create virtual tables" (img_003)                                     | 🟢     | `CreateVirtualTableModal.svelte`                                                                                         |
| Core concepts § "Bulk registration" (img_004)                                         | 🟢     | `BulkRegisterDialog.svelte` + `domain::virtual_tables::bulk_register`                                                    |
| Core concepts § "Auto-registration" (img_005, img_006)                                | 🟢     | `domain/auto_registration.rs` + `CreateAutoRegistrationModal.svelte`                                                     |
| Core concepts § "Tag filtering for Databricks sources"                                | 🟢     | `domain::auto_registration::filter_databricks_tags` + wizard step 3                                                      |
| Core concepts § "Virtual tables in Code Repositories" (code-imports + export-controls) | 🟢     | `domain/code_imports.rs` + `PATCH /sources/{rid}/code-imports` + `POST /sources/{rid}/export-controls`                   |
| Core concepts § "Viewing virtual table details"                                       | 🟢     | `VirtualTableDetailsPanel.svelte` (4 cards: Incremental / Versioning / Update detection / Pushdown)                      |
| Core concepts § "Update detection for virtual table inputs"                           | 🟢     | `domain/update_detection.rs` + outbox event on `foundry.dataset.events.v1`                                                |
| Core concepts § "Configure objects backed by virtual tables" [Beta]                   | 🟡     | Cedar entity + capability slice in place (P3); Ontology Manager wiring deferred to D1.1.10                                |
| Core concepts § "Limitations of using virtual tables"                                 | 🟢     | `domain::source_validation` (5 rules → 412) + `domain::code_imports::USE_EXTERNAL_SYSTEMS_INCOMPAT`                       |
| Transforms / Python § "Compute pushdown" (Overview + per-engine)                      | 🟢     | `sdks/python/openfoundry_transforms/virtual_tables.py` (Ibis / PySpark / Snowpark / foundry-compute)                     |
| Transforms / Python § "API reference"                                                 | 🟢     | `read_virtual_table` / `write_virtual_table` / `@pushdown_to` / `@use_external_systems` / `validate_transform`           |
| Transforms / Python § "BigQuery compute pushdown"                                     | 🟢     | `_IbisStub` + `resolve_engine` defaults BigQuery to Ibis                                                                 |
| Transforms / Python § "Snowflake compute pushdown"                                    | 🟢     | `_SnowparkStub` + `resolve_engine` defaults Snowflake to Snowpark                                                        |
| Transforms / Python § "Databricks compute pushdown"                                   | 🟢     | `_PySparkStub` + `resolve_engine` defaults Databricks to PySpark                                                         |
| Pipeline Builder § "Add a virtual table output"                                       | 🟢     | `services/pipeline-authoring-service/src/domain/virtual_table_nodes.rs` (output write_mode validated against matrix)      |
| Connectivity § "Add virtual tables to a Marketplace product"                          | 🔴     | Deferred to D1.4.x (Marketplace P-series); manifest shape sketched in ADR §6                                              |
| Contour § "Analyze in Contour"                                                        | 🔴     | Capability slice exposed; Contour board wiring deferred to D1.5.x                                                         |

## Decisions (cumulative)

* **D-1: Dedicated `virtual-table-service`** (not an extension of
  `connector-management-service`) — separate bounded context + RID
  prefix + audit stream; Iceberg / Databricks SDK deps stay isolated.
* **D-2: Capability matrix is the single source of truth** — Rust
  `MATRIX` + TS `defaultCapabilities` + doc-conformance tests
  triangulate against the published Foundry table.
* **D-3: Iceberg catalogs behind a trait + per-kind module** — five
  doc-listed catalog kinds, `compatibility(provider, catalog)` is
  closed and fails closed for combinations the doc does not bless.
* **D-4: Egress policy enforcement at the registration boundary** —
  Foundry-worker / direct egress / no agent proxy / no bucket
  endpoint / no self-service private link enforced via 412 with a
  stable error code so the UI surfaces the remediation hint.
* **D-5: Cedar entity carries the capability slice** — `VirtualTable`
  exposes `capabilities.{read, write, incremental, versioning}` so
  policies gate `virtual_table::write_data` without a network hop.
* **D-6: Auto-registration project is read-only** —
  `tenancy-organizations-service` provisions the managed project; the
  service-principal owns writes; users cannot manually edit resources
  in the project.
* **D-7: Orphaned tables are never auto-deleted** — doc § "Auto-
  registration" is explicit. The scanner marks
  `virtual_tables.properties.orphaned = true` and reads return
  `410 GONE_AT_SOURCE`.
* **D-8: Update detection falls back to "potential update" when
  versioning is not supported** — doc § "Update detection for virtual
  table inputs" is explicit. Object-store raw formats (Parquet, Avro,
  CSV plain) return `Version::Unknown` which the classifier maps to
  `PollOutcome::PotentialUpdate` so downstream triggers fire on every
  tick.
* **D-9: `@use_external_systems` decorator is build-blocked** — doc §
  "Limitations" prohibits the combination with virtual tables. The
  Python SDK validator emits
  `VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT`; the Rust
  `domain::code_imports` validator returns the same code so the build
  surface is uniform across CR + author.
