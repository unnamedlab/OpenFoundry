# Capability Map

The fastest way to understand what OpenFoundry is trying to deliver is to read its smoke suites as an executable platform map.

## Capability Phases Encoded In Smoke

| Phase | Scenario | Main Capability Areas |
| --- | --- | --- |
| P2 | `smoke/scenarios/p2-runtime-critical-path.json` | connectors, datasets, sync, pipelines, queries, streaming, reports, geospatial |
| P3 | `smoke/scenarios/p3-semantic-governance-critical-path.json` | ontology, interfaces, properties, governance-oriented workflows |
| P4 | `smoke/scenarios/p4-developer-platform-critical-path.json` | code repositories, branching, commits, search, developer platform flows |
| P5 | `smoke/scenarios/p5-ai-ml-critical-path.json` | AI providers, knowledge bases, embeddings, training jobs, model workflows |
| P6 | `smoke/scenarios/p6-analytics-enterprise-critical-path.json` | analytics datasets, enterprise-tier behaviors, geospatial exploration |

## How The Repo Reflects Those Phases

### Runtime and data operations

The P2 flow shows the core operational backbone:

- connect to a source
- sync into datasets
- operate on the data
- expose results through pipelines, queries, streaming, reports, and maps

This is reflected in service folders such as `data-connector`, `dataset-service`, `pipeline-service`, `sql-bi-gateway-service`, `streaming-service`, `report-service`, and `geospatial-service`.

### Semantic and governance layer

The P3 flow shows that OpenFoundry is not only a data movement stack. It also models meaning, interfaces, and governed domain structures through ontology-centric APIs.

That capability is reflected in a family of dedicated ontology services:

- `ontology-definition-service` — control plane for schema, governance, and definitions
- `object-database-service` — write authority for object and link instances
- `ontology-query-service` — serving plane for search, graph, views, and KNN
- `ontology-actions-service` — action validation and execution
- `ontology-security-service` — policy compilation and permission-aware query filters
- `ontology-funnel-service` — batch ingestion
- `ontology-functions-service` — function runtime

Together with `audit-service`, `auth-service`, and related shared middleware, these services implement the CQRS ontology stack described in the architecture documentation.

#### `ontology-actions-service` — runtime detail

`ontology-actions-service` is the dedicated binary that hosts the Action Types
runtime extracted from the legacy ontology service. Its router is built by
`ontology_actions_service::build_router` (in `services/ontology-actions-service/src/lib.rs`)
and the handlers themselves live in `libs/ontology-kernel`. Full HTTP contract,
environment variables and Foundry mapping are documented in
[`services/ontology-actions-service/README.md`](../../services/ontology-actions-service/README.md).

Runtime dependencies (configurable via environment variables — defaults match
the in-cluster service map in `services/edge-gateway-service/src/config.rs`):

- **Postgres** — owns the `action_types`, `action_executions` (revert ledger),
  `action_what_if_branches` and `action_execution_side_effects` tables.
  Migrations under `services/ontology-actions-service/migrations/` are applied
  on startup; the integration suite under
  `libs/ontology-kernel/tests/actions_integration.rs` reuses the same files.
- **`audit-compliance-service`** — every `execute_action` /
  `execute_action_batch` / inline-edit emits a structured audit event (success,
  denied, failure). Failure to deliver is logged but never aborts the action.
- **`notification-alerting-service`** — fan-out of action-driven notifications
  with the TASK M caps (≤ 500 recipients standard, ≤ 50 from a function).
- **`connector-management-service`** — TASK G writeback / side-effect webhooks.
  Writeback failures abort the action with HTTP 400; side-effect failures are
  logged and the action keeps running.
- **`object-database-service`** — write path for object instances and revisions.
  `update_object` / `delete_object` / `create_object` plans are applied through
  the kernel's transactional helpers and a row is appended to `object_revisions`.
- **`ontology-definition-service`** — schema lookups for object types, property
  declarations and link definitions referenced by an action's input/output schema.

Observability:

- Prometheus counters exported from `ontology_kernel::metrics` (`action_executions_total`,
  `action_failures_total{failure_type}`, latency histograms).
- JSON aggregation surface at `GET /api/v1/ontology/actions/{id}/metrics?window=…`
  computed directly from the `action_executions` ledger.

End-to-end coverage runs via `just test-actions` (Rust integration suite +
Playwright spec at `apps/web/tests/e2e/action-types.spec.ts`).

### Developer platform

The P4 flow demonstrates that the platform also includes repository-like development primitives such as branches, commits, search, and review-oriented flows.

That capability maps cleanly onto `code-repo-service`, and connects naturally with `app-builder-service` and `marketplace-service`.

### AI and ML

The P5 flow shows provider-backed AI and ML capabilities as first-class parts of the platform rather than bolt-on experiments:

- provider registration
- knowledge base creation
- document ingestion
- semantic search
- model training jobs

This is represented by `ai-service`, `ml-service`, and supporting shared crates such as `vector-store`.

### Enterprise analytics

The P6 flow extends the runtime path into richer analytics and geospatial use cases, reinforcing that the platform is meant to support decision workflows, not only CRUD APIs.

## Practical Reading Tip

If you need to understand a product area quickly, start with the matching smoke scenario and then read:

1. the corresponding frontend route in `apps/web/src/routes`
2. the service crate under `services/`
3. the domain contracts under `proto/`

That path usually gives you the shortest route from user behavior to implementation.
