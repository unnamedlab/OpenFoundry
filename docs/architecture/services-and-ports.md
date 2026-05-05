# Services and Ports

All backend services expose a health endpoint and bind to fixed default ports in local development. The edge gateway listens on `8080` and proxies public traffic to these internal services.

## Service Map

The **Plano objetivo** column maps each service onto one of the five
target planes from [Runtime Topology](./runtime-topology.md): *storage*,
*ingestion*, *compute*, *control* or *state* (relational). A small number
of services are dual-anchored (e.g. write-path services that govern
*state* but emit on the *control* plane).

| Service | Default Port | Plano objetivo | Primary Role |
| --- | --- | --- | --- |
| `gateway` | `8080` | control | Edge routing, HTTP compatibility, CORS, request IDs, rate limiting, tenant-aware access enforcement |
| `identity-federation-service` | `50112` | control | Login, refresh, MFA, SAML/OIDC/OAuth flows, service account tokens, scoped/guest sessions |
| `auth-service` | `50051` | control | User administration and temporary auth compatibility |
| `tenancy-organizations-service` | `50113` | control | Tenant resolution, organizations, enrollments, spaces, projects, and sharing boundaries |
| `data-connector` | `50052` | ingestion | Connector hyperautomation and discovery orchestration |
| `connector-management-service` | `50088` | ingestion | Connector catalog, source/connection definitions, capabilities, credentials metadata, connection testing, and sync definitions |
| `ingestion-replication-service` | `50090` (HTTP REST) / `50091` (gRPC `IngestJobService`) | ingestion | Ingest-job materialization, Debezium/Flink control plane, and CDC checkpoint ownership; no source-definition ownership |
| `dataset-service` | `50053` | state | Dataset metadata/discovery plus versioning runtime after consolidation; `dataset-versioning-service` is the sole runtime owner of versions/branches/transactions and Iceberg-backed snapshot state |
| `streaming-service` | `50054` | ingestion | Streaming pipelines and archive management |
| `sql-bi-gateway-service` | `50133` (Flight SQL gRPC) / `50134` (HTTP `/healthz` + saved queries) | compute | **Edge SQL gateway** for external BI traffic (Tableau, Superset, Arrow Flight SQL JDBC clients). Implemented as a real Apache Arrow Flight SQL server backed by DataFusion that routes per-statement to the appropriate backend (Iceberg via `sql-warehousing-service`, Trino for Iceberg analytics, Vespa, Postgres) — see [ADR-0014](./adr/ADR-0014-retire-trino-flight-sql-only.md) and [ADR-0029](./adr/ADR-0029-reintroduce-trino-for-iceberg-analytics.md), supersedes [ADR-0009](./adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md). Internal service-to-service SQL still uses Flight SQL P2P. |
| `sql-warehousing-service` | `50123` (Flight SQL gRPC) / `50124` (HTTP `/healthz`) | compute | SQL warehousing workflows, intermediate persistence and large-scale SQL transformations exposed as an Apache Arrow Flight SQL server backed by DataFusion |
| `pipeline-service` | `50056` | compute | Pipeline compatibility shell during service decomposition |
| `pipeline-authoring-service` | `50080` | compute | Pipeline definitions, validation, compilation, pruning, and executable plan generation |
| `pipeline-build-service` | `50081` | compute | Pipeline run execution and retry orchestration |
| `pipeline-schedule-service` | `50082` | control | Shared schedule orchestration for pipeline and workflow cron/event triggers, due runs, windows, and backfills |
| `lineage-service` | `50083` | compute | Dataset and column lineage APIs |
| `ontology-definition-service` | `50103` | control | Control plane: object types, properties, interfaces, link types, action definitions, function packages, object-set definitions, funnel definitions, project governance |
| `object-database-service` | `50104` | state | Write authority: object instances, link instances, revision history, transactional outbox |
| `ontology-query-service` | `50105` | compute | Serving plane: search, graph traversal, object views, KNN, object-set queries, read models and projections |
| `ontology-actions-service` | `50106` | control | Controlled mutations and policy/runtime plane: action validation/planning/execution, batch ingestion (funnel sources + runs + storage insights), function-package runtime (TypeScript/Python sandbox), and rule-engine (policy compilation, marking resolution, permission-aware filters). Sole runtime owner of the `actions_log` Cassandra column family — absorbed `ontology-funnel-service` (ex-`50107`), `ontology-functions-service` (ex-`50108`) and `ontology-security-service` (ex-`50109`) per ADR-0030 (S8.1) |
| `fusion-service` | `50058` | compute | Fusion and spreadsheet-oriented interactions |
| `ml-service` | `50059` | compute | Experiments, training, registry, model lifecycle |
| `ai-service` | `50060` | compute | AI providers, chat, tools, workflows |
| `workflow-automation-service` | `50137` | control | Workflow orchestration and execution runtime |
| `notebook-runtime-service` | `50134` | compute | Notebook kernels, cells, sessions, and interactive execution |
| `document-reporting-service` | `50102` | compute | Notepad-style documents and document reporting surfaces |
| `app-builder-service` | `50063` | control | App composition and runtime surfaces |
| `report-service` | `50064` | compute | Report generation and delivery |
| `code-repo-service` | `50065` | state | Code repository APIs |
| `marketplace-service` | `50066` | control | Marketplace and catalog APIs |
| `nexus-service` | `50067` | control | Federation, sharing, and multi-org collaboration |
| `geospatial-service` | `50068` | compute | Geospatial and mapping APIs |
| `notification-alerting-service` | `50114` | control | Notification transport, inbox APIs, delivery channels, alerting, and websocket fanout |
| `audit-service` | `50070` | control | Audit collection and export |

### Edge SQL surfaces — explicit positioning

Two surfaces sit at the **edge of the compute plane** and are easy to
confuse; their roles are intentionally disjoint:

| Component                      | Plano objetivo            | Role                                                                                                                                                                                                                            |
| ------------------------------ | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sql-bi-gateway-service`       | compute (edge BI gateway) | **Edge BI gateway**. The single Apache Arrow Flight SQL surface for external BI clients (Tableau, Superset, JDBC/ODBC). Backed by DataFusion, applies auth/quotas/audit/saved-queries, and routes per-statement to `sql-warehousing-service` (Iceberg), Trino (Iceberg analytics), Vespa (hybrid retrieval) or Postgres (OLTP reference) — see [ADR-0014](./adr/ADR-0014-retire-trino-flight-sql-only.md) and [ADR-0029](./adr/ADR-0029-reintroduce-trino-for-iceberg-analytics.md). |

## Gateway Route Ownership

The gateway maps URL prefixes to backend services. Important examples:

- `/api/v1/auth`, `/api/v1/users` -> `auth-service`
- `/api/v1/tenancy/resolve`, `/api/v1/organizations`, `/api/v1/enrollments` -> `tenancy-organizations-service`
- `/api/v1/datasets`, `/api/v2/filesystem` -> `dataset-service`
- `/api/v1/pipelines` -> `pipeline-authoring-service`
- `/api/v1/pipelines/{id}/run`, `/api/v1/pipelines/{id}/runs` -> `pipeline-build-service`
- `/api/v1/pipelines/triggers/cron/*` -> `pipeline-schedule-service`
- `/api/v1/workflows/events/*`, `/api/v1/workflows/triggers/cron/*`, `/api/v1/schedules/*` -> `pipeline-schedule-service`
- `/api/v1/lineage` -> `lineage-service`
- `/api/v1/ontology/projects` -> `tenancy-organizations-service`
- `/api/v1/ontology/actions`, `/api/v1/ontology/funnel`, `/api/v1/ontology/storage/insights`, `/api/v1/ontology/functions`, `/api/v1/ontology/rules`, `/api/v1/ontology/types/{id}/objects/{id}/inline-edit`, `/api/v1/ontology/types/{id}/rules`, `/api/v1/ontology/objects/{id}/rule-runs` -> `ontology-actions-service` (S8.1: sole runtime owner after absorbing funnel/functions/security)
- `/api/v1/ontology/search`, `/api/v1/ontology/graph`, `/api/v1/ontology/quiver`, `/api/v1/ontology/object-sets`, `/api/v1/ontology/types/{id}/objects/query`, `/api/v1/ontology/types/{id}/objects/knn` -> `ontology-query-service`
- `/api/v1/ontology/links/{id}/instances`, `/api/v1/ontology/types/{id}/objects` -> `object-database-service`
- `/api/v1/ontology/interfaces`, `/api/v1/ontology/shared-property-types`, `/api/v1/ontology/links`, `/api/v1/ontology/types` -> `ontology-definition-service`
- `/api/v1/ml` -> `ml-service`
- `/api/v1/ai` -> `ai-service`
- `/api/v1/reports` -> `report-service`
- `/api/v1/code-repos` -> `code-repo-service`
- `/api/v1/marketplace` -> `marketplace-service`
- `/api/v1/nexus/spaces` -> `tenancy-organizations-service`
- `/api/v1/nexus` -> `nexus-service`

## Cross-Service Dependencies

Configuration files show explicit service-to-service defaults for several domains:

- `connector-management-service` knows about dataset, pipeline, and ontology services
- `ingestion-replication-service` knows about dataset, pipeline, and ontology services
- `data-connector` knows about dataset, pipeline, ontology, and ingestion-replication services for hyperautomation flows
- `pipeline-authoring-service` depends on dataset, workflow, and AI services
- `pipeline-build-service` depends on dataset, workflow, and AI services
- `pipeline-schedule-service` depends on dataset, workflow, and AI services to own shared scheduling while delegating workflow execution to the workflow runtime
- `lineage-service` depends on dataset, workflow, and AI services
- `workflow-automation-service` depends on notification, ontology, and pipeline services
- `ontology-definition-service` depends on audit, AI, and notification services
- `object-database-service` depends on audit and notification services; all writes go through `object-database-service`
- `ontology-query-service` depends on `object-database-service` (fallback point lookups), `ontology-actions-service` (policy filters, S8.1), and AI services
- `ontology-actions-service` depends on `object-database-service` (mutations) and `ontology-definition-service` (action / function package definitions); owns the actions, funnel, function-runtime and rule (policy / marking) HTTP surfaces and the `actions_log` Cassandra column family (S8.1)
- `report-service` depends on dataset and geospatial services
- `notebook-runtime-service` depends on query and AI services
- `marketplace-service` depends on app-builder

## Health Convention

Every service exposes a `/health` route. This shared convention is used by:

- local runtime scripts
- GitHub Actions smoke jobs
- Helm health probes and operational checks

The `sql-warehousing-service` is gRPC-only on its primary port and therefore
exposes its HTTP health probe (`/healthz`, also aliased as `/health`) on a
companion port (`healthz_port`, default `50124`).
