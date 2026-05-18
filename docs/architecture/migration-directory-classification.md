# Migration Directory Classification

> **Historical snapshot — pre-consolidation baseline.** This document was
> written as the cleanup baseline for the **56 migration roots** that
> existed *before* the service consolidation tracked by
> [ADR-0030](./adr/ADR-0030-service-consolidation-30-targets.md) and the
> Rust → Go monorepo port. Many service directories listed below
> (e.g. `ai-application-generation-service`, `data-asset-catalog-service`,
> `marketplace-service`, `mcp-orchestration-service`, `oauth-integration-service`,
> `scenario-simulation-service`, `checkpoints-purpose-service`,
> `lineage-deletion-service`,
> `retention-policy-service`, `sds-service`, `security-governance-service`,
> `compute-modules-*-service`, `custom-endpoints-service`,
> `developer-console-service`, `event-ingestion-replication-service`,
> `execution-observability-service`, `managed-workspace-service`,
> `monitoring-rules-service`, `pipeline-authoring-service`,
> `time-series-data-service`, `automation-operations-service`) **no longer
> exist as standalone binaries** in the current Go monorepo — their
> migrations have been merged into the consolidated services listed in
> ADR-0030 and ADR-0042. Cross-references to `*.rs` files reflect the
> Rust era of the codebase; the Go ports live under
> `services/<svc>/internal/repo/migrations/`. For the current per-service
> migration layout consult the live tree. `cipher-service`, `network-boundary-service`,
> `report-service`, `global-branch-service`, and `knowledge-index-service` do exist
> in the current Go tree, but gateway alias semantics still differ by key: the
> `cipher_service_url`, `report_service_url`, `global_branch_service_url`, and
> `knowledge_index_service_url` defaults route to the same-named binaries, while
> `network_boundary_service_url` is a legacy alias whose Go default resolves to
> `authorization-policy-service` unless configuration overrides it.

This catalog is the cleanup baseline for the **56 migration roots**
called out by the Cassandra/Foundry parity plan. It classifies every
runtime SQL migration directory into one of the final buckets used by
the platform:

- `pg-schemas`
- `pg-policy`
- `pg-runtime-config`
- `pg-lakekeeper`
- `cassandra`
- `legacy-archive`

`pg-lakekeeper` is intentionally empty today: Lakekeeper owns its own
catalog schema bootstrap and there is no service-local SQL migration
chain for it.

## `pg-schemas`

- `services/ai-application-generation-service/migrations`
- `services/ingestion-replication-service/migrations/cdc_metadata`
- `services/code-repository-review-service/migrations`
- `services/data-asset-catalog-service/migrations`
- `services/dataset-versioning-service/migrations`
- `services/retrieval-context-service/migrations`
  Note: also hosts the `document_intelligence_*` migration absorbed
  from the retired `document-intelligence-service` (S8 / ADR-0030).
- `services/federation-product-exchange-service/migrations`
- `services/lineage-service/migrations`
- `services/marketplace-service/migrations`
- `services/mcp-orchestration-service/migrations`
- `services/model-catalog-service/migrations`
  Note: also hosts the `model_adapter_*` and `model_lifecycle_*` migrations
  absorbed from the retired `model-adapter-service` / `model-lifecycle-service`
  (S8 consolidation, ADR-0030).
- `services/oauth-integration-service/migrations`
- `services/ontology-definition-service/migrations-pg`
- `services/scenario-simulation-service/migrations`
- `services/sdk-generation-service/migrations`
- `services/solution-design-service/migrations`

## `pg-policy`

- `libs/outbox/migrations`
- `services/audit-compliance-service/migrations`
- `services/checkpoints-purpose-service/migrations`
- `services/cipher-service/migrations`
<!-- S8 / ADR-0030 — `code-security-scanning-service/migrations` merged into
     `services/code-repository-review-service/migrations`; the
     `code_security_scanning` schema relocated `pg-policy` → `pg-schemas`. -->
- `services/identity-federation-service/migrations`
  Note: `20260425193000_scoped_sessions_security.sql` moved to `legacy-archive`.
- `services/lineage-deletion-service/migrations`
- `services/network-boundary-service/migrations`
- `services/retention-policy-service/migrations`
- `services/sds-service/migrations`
- `services/security-governance-service/migrations`
- `services/telemetry-governance-service/migrations`
- `services/tenancy-organizations-service/migrations`

## `pg-runtime-config`

- `services/agent-runtime-service/migrations`
- `services/application-composition-service/migrations`
- `services/application-composition-service/migrations`
- `services/compute-modules-control-plane-service/migrations`
- `services/compute-modules-runtime-service/migrations`
- `services/connector-management-service/migrations`
- `services/custom-endpoints-service/migrations`
- `services/developer-console-service/migrations`
- `services/event-ingestion-replication-service/migrations`
  Note: hot runtime tables moved to `legacy-archive`; control-plane DDL remains active.
- `services/execution-observability-service/migrations`
- `services/ingestion-replication-service/migrations`
- `services/managed-workspace-service/migrations`
- `services/monitoring-rules-service/migrations`
- `services/notebook-runtime-service/migrations`
  Note: also hosts the `notepad_documents` / `notepad_presence` and
  `spreadsheet_sheets` / `spreadsheet_recalcs` migrations absorbed from
  the retired `document-reporting-service` and
  `spreadsheet-computation-service` (S8 consolidation, ADR-0030).
- `services/notification-alerting-service/migrations`
- `services/pipeline-authoring-service/migrations`
- `services/report-service/migrations`
- `services/sql-bi-gateway-service/migrations`
  Note: also hosts the `warehouse_*`, `tabular_analysis_*` and
  `analytical_expression*` migrations absorbed from the retired
  `sql-bi-gateway-service`, `sql-bi-gateway-service` and
  `analytical-logic-service` (S8 consolidation, ADR-0030). The
  `analytical_expressions` schema is also shipped by the internal
  `libs/analytical-logic` package so non-gateway consumers can
  re-apply it locally for tests.
- `services/time-series-data-service/migrations`
- `services/workflow-automation-service/migrations`

## `cassandra`

- `services/identity-federation-service/src/sessions_cassandra.rs`
  Owns `auth_runtime` session state; replaces the archived `scoped_sessions` SQL path.
- `services/oauth-integration-service/src/pending_auth_cassandra.rs`
  Owns short-lived OAuth pending-auth state; no active SQL migration dir is allowed for that hot path.
- `services/event-ingestion-replication-service/src/domain/runtime_store.rs`
  Owns the hot runtime ledger that replaced `streaming_events`, `streaming_checkpoints`,
  `streaming_cold_archives` and `streaming_topology_checkpoints`.

## `legacy-archive`

- `docs/architecture/legacy-migrations/automation-operations-service/`
- historical `services/automation-operations-service/migrations` path; the
  current owner is `services/workflow-automation-service/internal/automationoperations`
  and its migrations live with `workflow-automation-service`. It hosts the
  saga state-machine + audit schema (`saga.state`, `saga_audit_log`)
  that replaced Temporal as the authoritative orchestration store.
- `docs/architecture/legacy-migrations/event-ingestion-replication-service/`
- `docs/architecture/legacy-migrations/identity-federation-service/`
- `docs/architecture/legacy-migrations/object-database-service/`
- `docs/architecture/legacy-migrations/ontology-actions-service/`
- `docs/architecture/legacy-migrations/ontology-definition-service/`
- `docs/architecture/legacy-migrations/ontology-exploratory-analysis-service/`
- `docs/architecture/legacy-migrations/ontology-query-service/`
- `docs/architecture/legacy-migrations/ontology-security-service/`
- `docs/architecture/legacy-migrations/ontology-timeseries-analytics-service/`
- `docs/architecture/legacy-migrations/workflow-automation-service/`

Any future cleanup PR should update this catalog at the same time it
moves a migration root between buckets.
