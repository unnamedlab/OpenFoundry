# ADR-0041 — `iceberg-catalog-service`: Foundry Iceberg REST Catalog

* **Status:** Accepted (2026-05-05) — Beta (D1.1.8 4/5)
* **Supersedes:** ADR-0008 (Lakekeeper as the Iceberg REST catalog) for
  the *internal* catalog surface; we still link to Lakekeeper-flavoured
  *external* warehouses through the third-party adapters covered by the
  P6 work.
* **Related:** ADR-0027 (Cedar policy engine),
  ADR-0029 (Trino + Iceberg analytics),
  ADR-0034 (Datasets ↔ Foundry parity).

## Context

The Foundry doc § "Iceberg tables" enumerates four guarantees the
internal catalog must provide: REST Catalog spec compliance,
all-or-nothing transaction semantics across multiple writes, marking
inheritance with snapshot semantics, and explicit schema evolution.
Lakekeeper (ADR-0008) covers spec compliance but does **not** model
the Foundry-side guarantees: it leans on optimistic concurrency,
treats markings as opaque properties, and exposes implicit schema
evolution. We cannot ship those Foundry semantics without owning the
catalog surface.

## Decision

Build a dedicated Rust service, `iceberg-catalog-service`, that:

1. **Implements the Apache Iceberg REST Catalog OpenAPI spec** for
   external clients (PyIceberg, Spark, Trino, Snowflake) so any
   compliant Iceberg client can read / write Foundry tables without a
   Foundry-specific shim.
2. **Wraps every spec endpoint in Foundry-pattern transaction
   semantics** via `FoundryIcebergTxn` (P2). The wrapper batches every
   pending mutation onto `POST /iceberg/v1/transactions/commit` so a
   commit either lands across every touched table or rolls back as a
   single Postgres transaction with `SELECT … FOR UPDATE` row locks.
3. **Aliases Foundry's `master` ↔ Iceberg's `main`** at every entry
   point that accepts a branch name (per the doc § "Default
   branches"). The alias is logged in the response header
   `X-Foundry-Branch-Alias: master->main` for transparency.
4. **Enforces strict schema evolution.** Implicit `add-schema` updates
   that diverge from the current schema are rejected with
   `422 SCHEMA_INCOMPATIBLE_REQUIRES_ALTER`; the dedicated
   `POST .../alter-schema` endpoint is the only path to change a
   table's shape.
5. **Bundles a Cedar policy set for markings** (P3, ADR-0027). The
   `authz_cedar::iceberg_policies` module declares per-action policies
   for `IcebergNamespace` and `IcebergTable` resources. The catalog
   evaluates them in-process on every spec request — there is no
   network hop to a remote PDP.

The service runs **independently** of `dataset-versioning-service`
because the spec is a contract external clients consume; bundling it
into the dataset service would couple the catalog's release cadence
to internal storage refactors. The service depends on
`storage-abstraction::iceberg` for object-store I/O and on
`oauth-integration-service` for OAuth2 client credential validation.

## Consequences

### Positive

* External Iceberg clients work against Foundry today without a
  Foundry-specific shim.
* All-or-nothing transactions match the Foundry doc; the build
  executor can adopt the bridging adapter (`build_integration::IcebergJobOutputs`)
  to inherit the guarantee.
* Markings + Cedar enforcement live in the same service that owns the
  resource lifecycle, so a marking added on a namespace surfaces to
  policies without crossing service boundaries.
* Coverage: 39 lib tests + 14 integration tests in Rust, 4 Python
  PyIceberg suites, 2 Playwright e2e specs, plus a `cargo llvm-cov ≥ 72%`
  CI gate.

### Negative

* Two catalogs in the platform. Lakekeeper still exists as the
  *external warehouse* adapter and shares no code with this service;
  the duplication is intentional but is a maintenance cost.
* Markings inheritance is a snapshot; operators expecting retroactive
  propagation must explicitly re-apply via `manage_markings`.
* Cedar policy evaluation adds ~1ms per spec call (p99 measured against
  the in-memory store on a dev laptop). Acceptable for the catalog's
  request volume but noted here so future profiling work has a
  baseline.

### Open work for 5/5

* **P4 — Compaction worker.** Implements the `replace` snapshot type
  end-to-end: a maintenance job that rewrites manifests + data files
  without changing logical contents.
* **P5 — Bring-Your-Own-Bucket + at-rest encryption.** Per-warehouse
  KMS integration so customer-managed keys can encrypt Iceberg data.
* **P6 — Credential vending + 3rd-party catalog federation.** The
  `LoadTable.config` shape already declares the
  `s3.access-key-id-template` placeholders (Beta surface); P6 turns
  them into actual STS / SAS vending and adds the Databricks / Unity
  adapters covered by the doc § "Iceberg catalogs".

## Alternatives considered

* **Extend Lakekeeper.** Rejected — the upstream project does not
  model Foundry transactions, markings, or master↔main alias, and
  forking it would freeze us at a specific release.
* **Inline the catalog into `dataset-versioning-service`.** Rejected —
  the spec endpoints are a contract external clients consume directly,
  not a Foundry-internal API; bundling them couples release cadences
  unnecessarily.
* **Push markings into PostgREST + Postgres RLS.** Rejected — Cedar
  per ADR-0027 is the platform-wide answer; bypassing it for one
  service would fragment the policy story.
