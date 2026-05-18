# Services and Ports

All backend services expose a health endpoint and bind to fixed default ports in local development. Service defaults come from each service's `internal/config/config.go`, or `services/<service>/config.yaml` when there is no Go config. The edge gateway listens on `8080` and proxies public traffic to these internal services.

> Current-source note: this page describes runtime service names and default
> ports. It is not a filesystem map. The HTTP gateway source lives at
> `services/edge-gateway-service`; there is no current `services/gateway`
> directory. For authoritative route ownership, read
> `services/edge-gateway-service/internal/proxy/router_table.go`.

## Service Map

The **Plano objetivo** column maps each service onto one of the five
target planes from [Runtime Topology](./runtime-topology.md): *storage*,
*ingestion*, *compute*, *control* or *state* (relational). A small number
of services are dual-anchored (e.g. write-path services that govern
*state* but emit on the *control* plane).

| Service | Default Port | Plano objetivo | Primary Role |
| --- | --- | --- | --- |
| `edge-gateway-service` | `8080` | control | Public HTTP edge, route selection, request IDs, rate limiting, tenant/auth headers, audit fan-out |
| `identity-federation-service` | `50112` | control | Login, refresh, MFA, SAML/OIDC/OAuth flows, service account tokens, scoped/guest sessions |
| `authorization-policy-service` | `50093` | control | Roles, permissions, groups, policies, restricted views, and merged security-governance/checkpoints alias surfaces |
| `tenancy-organizations-service` | `50113` | control | Tenant resolution, organizations, enrollments, spaces, projects, and sharing boundaries |
| `connector-management-service` | `50088` | ingestion | Connector catalog, source/connection definitions, credentials metadata, connection testing, discovery orchestration, and virtual-table style routes |
| `ingestion-replication-service` | `50090` | ingestion | Ingest-job materialization, replication control plane, and CDC metadata endpoints |
| `dataset-versioning-service` | `50078` | state | Dataset metadata, branches, transactions, versions, files, and Iceberg-backed snapshot state |
| `media-sets-service` | `50156` / `50157` | state | Media set metadata, media item references, and media storage APIs |
| `iceberg-catalog-service` | `50118` service default; `8197` gateway YAML default | storage | Iceberg REST catalog compatibility surface; `DefaultUpstreams()` also uses `50118`, while checked-in gateway `config.yaml` overrides `iceberg_catalog_service_url` to `8197` |
| `sql-bi-gateway-service` | `50133` / `50134` | compute | Flight SQL / BI edge plus HTTP `/healthz` and saved-query style surfaces |
| `pipeline-build-service` | `50081` | compute | Pipeline definitions, validation, preview/build execution, run history, and scheduled/cron trigger ownership after consolidation |
| `lineage-service` | `50083` | compute | Dataset and column lineage APIs |
| `ontology-definition-service` | `50103` | control | Ontology schema/control plane: object types, properties, interfaces, link types, action definitions, and project governance |
| `object-database-service` | `50104` | state | Object instances, link instances, revision history, and transactional outbox |
| `ontology-query-service` | `50105` | compute | Search, graph traversal, object-set queries, KNN, read models, and projections |
| `ontology-actions-service` | `50106` | control | Controlled mutations, action validation/execution, funnel/functions/rules, and policy-aware filters |
| `workflow-automation-service` | `50137` | control | Workflow orchestration and execution runtime |
| `notebook-runtime-service` | `50134` | compute | Notebook kernels, cells, sessions, notepad/reporting-style surfaces after consolidation |
| `application-composition-service` | `50140` | control | Application composition, templates, publishing, and related widget/app surfaces |
| `code-repository-review-service` | `50155` | state | Code repository review and developer-platform repository flows |
| `federation-product-exchange-service` | `50120` | control | Federation, marketplace, product exchange, and Nexus-style collaboration surfaces |
| `notification-alerting-service` | `50114` | control | Notification transport, inbox APIs, delivery channels, alerting, and websocket fanout |
| `audit-compliance-service` | `50115` | control | Audit collection, retention, lineage deletion, SDS, GDPR, and compliance posture surfaces |
| `model-catalog-service` | `50085` | compute | ML experiments, runs, models, and model versions |
| `model-deployment-service` | `50086` | compute | Model deployments, predictions, drift, and batch prediction APIs |
| `ai-evaluation-service` | `50075` | compute | AI guardrail and evaluation APIs |
| `llm-catalog-service` | `50095` | compute | AI provider catalog APIs |
| `retrieval-context-service` | `50098` | compute | RAG context APIs; `router_table.go` does not currently select this upstream for public `/api/*` paths |
| `agent-runtime-service` | `50127` | compute | Agent/AI runtime, tool execution, prompt workflow compatibility, and conversation surfaces |
| `entity-resolution-service` | `50058` | compute | Entity resolution and fusion-style APIs |
| `ontology-exploratory-analysis-service` | `50131` | compute | Exploratory ontology analysis and geospatial-style APIs after consolidation |
| `telemetry-governance-service` | `50153` | control | Monitoring views, monitor rules, and telemetry governance |
| `cipher-service` | `:8080` from `config.yaml`; `50160` in compose/gateway | control | Default target behind the gateway `Cipher` alias for `/api/v1/auth/cipher/*` |
| `network-boundary-service` | `:8080` from `config.yaml` | control | Egress-policy APIs plus network-boundary placeholder routes |
| `global-branch-service` | `:8080` from `config.yaml`; `50161` in compose/gateway | state | `/api/v1/global-branches` branch CRUD owner; legacy `/api/v1/code-repos/.../branches` stays on code-repository-review without gateway rewrites |
| `report-service` | `:8080` from `config.yaml`; `50163` in compose/gateway | compute | Default target behind the gateway `Report` alias for `/api/v1/reports*` |
| `knowledge-index-service` | `:8080` from `config.yaml`; `50162` in compose/gateway | compute | Current router owner for `/api/v1/ai/knowledge-bases*` CRUD, documents, and search |
| `function-runtime-service` | `50190` | compute | User-authored TypeScript/Python function registry and execution runtime |

### Internal / data-plane binaries (no gateway routes)

The following binaries live under `services/` and ship in the same
Helm releases but are not reachable through the edge gateway; they are
data-plane consumers, CLI tools, or runtime workers.

| Service | Plano objetivo | Primary Role |
| --- | --- | --- |
| `action-log-sink` | storage | Kafka consumer that lands ontology action events into Iceberg action logs |
| `ai-sink` | storage | Kafka consumer that lands AI runtime events into Iceberg sinks |
| `audit-sink` | storage | Kafka consumer that lands audit events into the Iceberg audit archive |
| `iceberg-object-indexer` | compute | Iceberg scan worker that writes object rows into `object-database-service` |
| `ontology-indexer` | compute | Cassandra → Vespa indexer for ontology read models |
| `pipeline-runner` | compute | Generic pipeline-step runner used by `pipeline-build-service` |
| `pipeline-runner-spark` | compute | Spark variant of the pipeline runner (Iceberg writes, heavy transforms) |
| `reindex-coordinator-service` | compute | Foundry-pattern reindex coordinator (Kafka-driven full-keyspace scan; ADR-0037) |
| `compute-module-service` | compute | Hosts user-supplied compute modules (Foundry-style container UDFs) |
| `media-transform-runtime-service` | compute | Worker runtime for media transforms scheduled by `media-sets-service` |
| `sdk-generation-service` | control | OpenAPI / TS / Python / Java SDK generation hand-off endpoint |
| `solution-design-service` | control | Solution / template authoring control plane |
| `workflow-automation-service` (CronJob: `approvals-timeout-sweep`) | control | Periodic timeout sweeper shipped alongside `workflow-automation-service`; turns expired approvals into `approval.expired.v1` events |

`workflow-automation-service` also hosts the consolidated **saga** and
**approval** substrates after S8 consolidation (see
[ADR-0030](./adr/ADR-0030-service-consolidation-30-targets.md)). The
legacy `automation-operations-service` and `approvals-service`
binaries no longer exist on disk; their packages now live in
[`services/workflow-automation-service/internal/automationoperations/`](../../services/workflow-automation-service/internal/automationoperations/)
and
[`services/workflow-automation-service/internal/approvals/`](../../services/workflow-automation-service/internal/approvals/).

### Edge SQL surfaces — explicit positioning

Two surfaces sit at the **edge of the compute plane** and are easy to
confuse; their roles are intentionally disjoint:

| Component                      | Plano objetivo            | Role                                                                                                                                                                                                                            |
| ------------------------------ | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sql-bi-gateway-service`       | compute (edge BI gateway) | **Edge BI gateway**. The single Apache Arrow Flight SQL surface for external BI clients (Tableau, Superset, JDBC/ODBC). The Flight SQL gRPC port (`50133`) is substrate-only today: a literal-SELECT evaluator (`libs/query-engine`) answers BI client probes and richer statements are federated by catalog prefix to optional Flight SQL backends (`WAREHOUSING_FLIGHT_SQL_URL`, `TRINO_FLIGHT_SQL_URL`, `VESPA_FLIGHT_SQL_URL`, `POSTGRES_FLIGHT_SQL_URL`) — see [ADR-0014](./adr/ADR-0014-retire-trino-flight-sql-only.md), [ADR-0029](./adr/ADR-0029-reintroduce-trino-for-iceberg-analytics.md) and [ADR-0030](./adr/ADR-0030-service-consolidation-30-targets.md). The companion HTTP port (`50134`) owns service-local warehousing (`/api/v1/warehouse/*`) and tabular-analysis (`/api/v1/tabular/*`) HTTP CRUD absorbed from the retired `sql-warehousing-service` and `tabular-analysis-service` (S8 consolidation); `router_table.go` has no current edge-gateway route for those two prefixes, so do not document them as public gateway-owned routes. The analytical-expressions surface lives in the `libs/analytical-logic` internal package (no duplicated routes). |

## Gateway Route Ownership

The gateway maps URL prefixes to backend services. Important examples
from `services/edge-gateway-service/internal/proxy/router_table.go`:

- `/api/v1/auth`, `/api/v1/users` -> `identity-federation-service`
- `/api/v1/roles`, `/api/v1/permissions`, `/api/v1/groups`, `/api/v1/policies` -> `authorization-policy-service`
- `/api/v1/tenancy/resolve`, `/api/v1/organizations`, `/api/v1/enrollments` -> `tenancy-organizations-service`
- `/api/v1/connectors/catalog`, `/api/v1/connections` -> `connector-management-service`
- `/api/v1/connector-agents`, connection sync jobs -> `ingestion-replication-service`
- `/api/v1/datasets`, `/api/v2/filesystem` -> `dataset-versioning-service`
- `/api/v1/pipelines`, pipeline runs, and pipeline cron triggers -> `pipeline-build-service`
- `/api/v1/workflows`, approvals, and workflow execution routes -> `workflow-automation-service`
- `/api/v1/lineage` -> `lineage-service`
- `/api/v1/ontology/projects` -> `tenancy-organizations-service`
- `/api/v1/ontology/actions`, `/api/v1/ontology/funnel`, `/api/v1/ontology/storage/insights`, `/api/v1/ontology/functions`, `/api/v1/ontology/rules`, `/api/v1/ontology/types/{id}/objects/{id}/inline-edit`, `/api/v1/ontology/types/{id}/rules`, `/api/v1/ontology/objects/{id}/rule-runs` -> `ontology-actions-service` (S8.1: sole runtime owner after absorbing funnel/functions/security)
- `/api/v1/ontology/search`, `/api/v1/ontology/graph`, `/api/v1/ontology/quiver`, `/api/v1/ontology/object-sets`, `/api/v1/ontology/types/{id}/objects/knn` -> `ontology-query-service`
- `/api/v1/ontology/links/{id}/instances`, `/api/v1/ontology/types/{id}/objects`, `/api/v1/ontology/types/{id}/objects/query` -> `object-database-service`
- `/api/v1/ontology/interfaces`, `/api/v1/ontology/shared-property-types`, `/api/v1/ontology/links`, `/api/v1/ontology/types` -> `ontology-definition-service`
- `/api/v1/ml/experiments`, `/api/v1/ml/models` -> `model-catalog-service`
- `/api/v1/ml/deployments`, `/api/v1/ml/batch-predictions` -> `model-deployment-service`
- `/api/v1/ai/evaluations` -> `ai-evaluation-service`
- `/api/v1/ai/providers` -> `llm-catalog-service`
- `/api/v1/ai/knowledge-bases/*/search` -> `knowledge-index-service` (the gateway does not rewrite paths; this service mounts the matching search route)
- `/api/v1/ai/knowledge-bases` -> `knowledge-index-service`
- `/api/v1/entity-resolution`, `/api/v1/fusion` -> `entity-resolution-service`
- `/api/v1/global-branches` -> `global-branch-service`
- `/api/v1/code-repos` -> `code-repository-review-service`; legacy `/api/v1/code-repos/repositories/{id}/branches` remains here because the gateway does not rewrite it to `/api/v1/global-branches`
- `/api/v1/marketplace`, `/api/v1/federation-product-exchange`, `/api/v1/nexus` -> `federation-product-exchange-service`
- `/api/v1/nexus/spaces` -> `tenancy-organizations-service`
- `/api/v1/notifications` -> `notification-alerting-service`
- `/api/v1/audit` -> `audit-compliance-service`
- `/api/v1/auth/cipher` -> the `Cipher` upstream alias
- `/api/v1/network-boundaries`, `/api/v1/network-boundary`, `/api/v1/data-connection/egress-policies` -> the `NetworkBoundary` upstream alias
- `/api/v1/reports` -> the `Report` upstream alias

### Gateway upstream aliases (Helm parity)

The gateway's `UpstreamURLs` struct in
[`services/edge-gateway-service/internal/config/config.go`](../../services/edge-gateway-service/internal/config/config.go)
keeps **legacy alias fields** for service names and default ports that
were exposed during the strangler-fig cutover, even when the bounded
context has since been absorbed by another Go binary. The package
doc-comment is explicit that the field set and defaults remain stable so
one Helm `values.yaml` can continue to drive the gateway.

The practical consequence is that several upstream keys point to a
service that is **not** a separate binary on disk. The mapping is:

| Gateway upstream key | Resolves to |
| --- | --- |
| `data_connector_service_url` (`:50088`) | `connector-management-service` |
| `connector_management_service_url` (`:50088`) | `connector-management-service` |
| `virtual_table_service_url` (`:50088`) | `connector-management-service` |
| `ontology_service_url` (`:50103`) | `ontology-definition-service` |
| `audit_service_url` (`:50115`) | `audit-compliance-service` |
| `ml_service_url` (`:50085`) | `model-catalog-service` |
| `ai_service_url` (`:50127`) | `agent-runtime-service` |
| `security_governance_service_url` (`:50093`) | Legacy gateway alias; by default resolves to `authorization-policy-service` (absorbed Security/Governance surface). No `services/security-governance-service` binary exists. |
| `cipher_service_url` (`:50160`) | Gateway alias; by default resolves to the real `cipher-service` binary. The same-named directory exists and is the default target. |
| `oauth_integration_service_url` (`:50112`) | `identity-federation-service` (absorbed surface) |
| `session_governance_service_url` (`:50112`) | `identity-federation-service` (absorbed surface) |
| `network_boundary_service_url` (`:50093`) | Legacy gateway alias; `DefaultUpstreams()` resolves to `authorization-policy-service`. The `network-boundary-service` directory exists, but the code default does not route there unless config overrides it; the checked-in `config.yaml` currently overrides this key to `http://localhost:50119`. |
| `checkpoints_purpose_service_url` (`:50093`) | Legacy gateway alias; by default resolves to `authorization-policy-service`. No same-named Go service exists. |
| `retention_policy_service_url` (`:50115`) | `audit-compliance-service` |
| `lineage_deletion_service_url` (`:50115`) | `audit-compliance-service` |
| `sds_service_url` (`:50115`) | `audit-compliance-service` |
| `pipeline_authoring_service_url` (`:50081`) | `pipeline-build-service` (absorbed surface) |
| `pipeline_schedule_service_url` (`:50081`) | `pipeline-build-service` (absorbed surface) |
| `data_asset_catalog_service_url` (`:50078`) | Legacy gateway alias; by default resolves to `dataset-versioning-service` (absorbed Data Asset Catalog surface). No `services/data-asset-catalog-service` binary exists. |
| `dataset_quality_service_url` (`:50078`) | Legacy gateway alias; by default resolves to `dataset-versioning-service` (absorbed dataset quality/health surface). No `services/dataset-quality-service` binary exists. |
| `approvals_service_url` (no `UpstreamURLs` field) | `workflow-automation-service` routes own approvals directly |
| `app_builder_service_url` (no `UpstreamURLs` field) | `application-composition-service` routes app-builder paths directly |
| `application_curation_service_url` (`:50140`) | Legacy gateway alias; by default resolves to `application-composition-service` (absorbed curation/app-builder surface). |
| `model_evaluation_service_url` (`:50086`) | `model-deployment-service` alias in code defaults |
| `model_serving_service_url` (`:50086`) | `model-deployment-service` (absorbed surface) |
| `model_inference_history_service_url` (`:50086`) | `model-deployment-service` (absorbed surface) |
| `prompt_workflow_service_url` (no `UpstreamURLs` field) | `agent-runtime-service` routes own prompt paths directly |
| `knowledge_index_service_url` (`:50162`) | Gateway key for a real binary; by default resolves to `knowledge-index-service`. `router_table.go` sends CRUD, documents, and search on `/api/v1/ai/knowledge-bases*` here. |
| `retrieval_context_service_url` (`:50098`) | `retrieval-context-service`; configured upstream, but no current `SelectUpstream` branch returns it |
| `conversation_state_service_url` (no `UpstreamURLs` field) | `agent-runtime-service` routes conversation paths directly |
| `document_reporting_service_url` (`:50134`) | Legacy gateway alias; by default resolves to `notebook-runtime-service` (absorbed document reporting surface). No `services/document-reporting-service` binary exists. |
| `streaming_service_url` (no `UpstreamURLs` field) | retired; no surviving gateway branch |
| `report_service_url` (`:50163`) | Gateway alias; by default resolves to the real `report-service` binary. The same-named directory exists and is the current `DefaultUpstreams()` target for `/api/v1/reports*`. |
| `geospatial_intelligence_service_url` (`:50131`) | `ontology-exploratory-analysis-service` |
| `code_repo_service_url` (`:50155`) | `code-repository-review-service` |
| `global_branch_service_url` (`:50161`) | Gateway alias; by default resolves to the real `global-branch-service` binary for `/api/v1/global-branches`. The same-named directory exists and is the default target; legacy code-repos branch paths stay on `code_repo_service_url` until a compatible upstream path or rewrite exists. |
| `marketplace_catalog_service_url` (`:50120`) | Legacy gateway alias; by default resolves to `federation-product-exchange-service` (absorbed marketplace catalog surface). |
| `product_distribution_service_url` (`:50120`) | Legacy gateway alias; by default resolves to `federation-product-exchange-service` (absorbed product distribution surface). |
| `nexus_service_url` (`:50113`) | Legacy gateway alias; by default resolves to `tenancy-organizations-service` for `/api/v1/nexus/spaces`; broader `/api/v1/nexus*` routes use `federation-product-exchange-service`. No `services/nexus-service` binary exists. |

> **Do not point a real Helm deployment at stale substitute URLs.** Point
> each alias at its current owner. A key named `*_service_url` is not proof
> that `services/<name>/` exists. `cipher-service`, `knowledge-index-service`,
> `report-service`, and `global-branch-service` are real same-named default
> targets; `network_boundary_service_url` is a legacy alias whose Go default is
> still `authorization-policy-service` unless configuration overrides it.

## Cross-Service Dependencies

Configuration files show explicit service-to-service defaults for several domains:

- `connector-management-service` knows about dataset, pipeline, and ontology services
- `ingestion-replication-service` knows about dataset, pipeline, and ontology services
- connector discovery and virtual-table style routes are consolidated into `connector-management-service`
- `pipeline-build-service` depends on dataset, workflow, AI, and storage services
- `lineage-service` depends on dataset, workflow, and AI services
- `workflow-automation-service` depends on notification, ontology, and pipeline services
- `ontology-definition-service` depends on audit, AI, and notification services
- `object-database-service` depends on audit and notification services; all writes go through `object-database-service`
- `ontology-query-service` depends on `object-database-service` (fallback point lookups), `ontology-actions-service` (policy filters, S8.1), and AI services
- `ontology-actions-service` depends on `object-database-service` (mutations) and `ontology-definition-service` (action / function package definitions); owns the actions, funnel, function-runtime and rule (policy / marking) HTTP surfaces and the `actions_log` Cassandra column family (S8.1)
- reporting routes use the gateway `Report` alias and compose/defaults point it at `report-service`
- `notebook-runtime-service` depends on query and AI services
- marketplace/product-exchange routes are consolidated into `federation-product-exchange-service`
- app-builder/application-curation/developer-console style routes are consolidated into `application-composition-service`

## Health Convention

Every current Go service exposes a `/healthz` route. Some services also keep
`/health` as a compatibility alias. This shared convention is used by:

- local runtime scripts
- GitHub Actions smoke jobs
- Helm health probes and operational checks

The `sql-bi-gateway-service` is gRPC-only on its primary Flight SQL port
(`50133`) and therefore exposes its HTTP `/healthz` probe (also aliased as
`/health`) plus the saved-queries / warehousing / tabular-analysis HTTP
CRUD on a companion port (`healthz_port`, default `50134`). The retired
`sql-bi-gateway-service` previously played the same gRPC-only role on
ports `50123`/`50124`; that surface is now folded into the gateway.
