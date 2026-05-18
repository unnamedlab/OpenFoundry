# Services and Ports

All backend service directories under `services/*` are implementation
inventory. This page is the documentation snapshot for that inventory;
when it disagrees with source, the source wins. Re-check the counts with:

```sh
find services -mindepth 1 -maxdepth 1 -type d | wc -l
find libs -mindepth 1 -maxdepth 1 -type d | wc -l
```

Current snapshot: **50 service directories** and **36 shared library
directories**. Default bind ports below come from each service's
`internal/config` package, service `config.yaml`, or worker CLI defaults.
Kubernetes charts commonly expose service containers on port `8080` even
when the local/dev `PORT` default is different.

The edge gateway listens on `8080` and routes public HTTP traffic by
prefix. The authoritative router is
[`services/edge-gateway-service/internal/proxy/router_table.go`](../../services/edge-gateway-service/internal/proxy/router_table.go);
this page summarizes the same ownership in operator-friendly tables.

## Gateway-routed public surfaces

| Public route family | Gateway upstream / current owner | Local default bind | Status |
| --- | --- | ---: | --- |
| `/api/v1/auth`, `/api/v1/users`, `/api/v2/admin/users/me`, OAuth/app registration surfaces | `identity-federation-service` | `50112` | active+routed |
| `/api/v1/auth/cipher` | `cipher-service` in Helm dev/stub wiring; localhost default alias still points at `authorization-policy-service` until gateway overrides are set | `8080` | active+routed stub / override-sensitive |
| `/api/v1/roles`, `/api/v1/permissions`, `/api/v1/groups`, `/api/v1/policies`, `/api/v1/restricted-views`, security-governance aliases | `authorization-policy-service` | `50093` | active+routed |
| `/api/v1/network-boundaries`, `/api/v1/network-boundary`, `/api/v1/data-connection/egress-policies` | `network-boundary-service` in Helm dev/stub wiring; localhost default alias still points at `authorization-policy-service` until gateway overrides are set | `8080` | active+routed stub / override-sensitive |
| `/api/v1/tenancy`, `/api/v1/organizations`, `/api/v1/enrollments`, `/api/v1/spaces`, `/api/v1/projects`, `/api/v1/workspace`, `/api/v1/nexus/spaces`, `/api/v1/ontology/projects` | `tenancy-organizations-service` | `50113` | active+routed |
| `/api/v1/data-connection`, `/api/v1/webhooks`, `/api/v1/listeners`, `/api/v1/connectors/catalog`, connection discovery/HyperAuto routes | `connector-management-service` | `50088` | active+routed |
| `/api/v1/connector-agents`, connection sync job routes | `ingestion-replication-service` | `50090` | active+routed |
| `/api/v1/datasets`, `/api/v2/filesystem`, dataset quality aliases | `dataset-versioning-service` | `50078` | active+routed |
| `/api/v1/iceberg-tables`, `/iceberg/v1`, `/v1/iceberg-clients` | `iceberg-catalog-service` | `50118` | active+routed |
| `/api/v1/queries` and Flight SQL / BI surfaces | `sql-bi-gateway-service` | `50133` gRPC / `50134` HTTP | active+routed |
| `/api/v1/pipelines`, pipeline run and cron trigger routes | `pipeline-build-service` | `50081` | active+routed |
| `/api/v1/lineage` | `lineage-service` | `50083` | active+routed |
| `/api/v1/ontology/functions`, `/api/v1/ontology/funnel`, `/api/v1/ontology/storage/insights`, `/api/v1/ontology/actions`, `/api/v1/ontology/rules`, inline-edit routes | `ontology-actions-service` | `50106` | active+routed |
| `/api/v1/ontology/search`, `/api/v1/ontology/graph`, `/api/v1/ontology/quiver`, `/api/v1/ontology/object-sets`, ontology KNN routes | `ontology-query-service` | `50105` | active+routed |
| Ontology object/link instance routes | `object-database-service` | `50104` | active+routed |
| Ontology type/interface/link definition routes | `ontology-definition-service` | `50103` | active+routed |
| `/api/v1/workflows`, `/api/v1/approvals`, workflow approval routes | `workflow-automation-service` | `50137` | active+routed |
| `/api/v1/notebooks`, `/api/v1/notepad` | `notebook-runtime-service` | `50134` | active+routed |
| `/api/v1/notifications` | `notification-alerting-service` | `50114` | active+routed |
| `/api/v1/monitoring`, `/api/v1/monitoring-views`, `/api/v1/monitor-rules` | `telemetry-governance-service` | `50153` | active+routed |
| `/api/v1/ml/experiments`, `/api/v1/ml/runs`, `/api/v1/ml/models`, `/api/v1/ml/model-versions` | `model-catalog-service` | `50085` | active+routed |
| `/api/v1/ml/deployments`, `/api/v1/ml/batch-predictions` | `model-deployment-service` | `50086` | active+routed |
| `/api/v1/ai/guardrails/evaluate`, `/api/v1/ai/evaluations` | `ai-evaluation-service` | `50075` | active+routed |
| `/api/v1/ai/providers` | `llm-catalog-service` | `50095` | active+routed |
| `/api/v1/ai/prompts`, `/api/v1/agent-runtime`, `/api/v1/ai/conversations`, generic `/api/v1/ai` fallback | `agent-runtime-service` | `50127` | active+routed |
| `/api/v1/ai/knowledge-bases/*/search` | `retrieval-context-service` | `50098` | active+routed |
| `/api/v1/ai/knowledge-bases` management routes | `knowledge-index-service` in Helm stub wiring; localhost default alias remains `50097` until override | `8080` | active+routed stub / override-sensitive |
| `/api/v1/entity-resolution`, `/api/v1/fusion` | `entity-resolution-service` | `50058` | active+routed |
| `/api/v1/reports` | `report-service` in Helm stub wiring; localhost default alias still points at `notebook-runtime-service` until override | `8080` | active+routed stub / override-sensitive |
| `/api/v1/geospatial` | `ontology-exploratory-analysis-service` | `50131` | active+routed |
| `/api/v1/code-repos` | `code-repository-review-service` | `50155` | active+routed |
| `/api/v1/code-repos/repositories/*/branches` | `global-branch-service` exists, but the router currently sends this legacy route family to the `GlobalBranch` upstream alias; localhost default aliases it to `code-repository-review-service` until the cutover override is set | `8080` | active but gateway cutover pending |
| `/api/v1/federation-product-exchange`, `/api/v1/nexus`, `/api/v1/marketplace` | `federation-product-exchange-service` | `50120` | active+routed |
| `/api/v1/apps`, application composition, curation, app-builder/developer-console style route families | `application-composition-service` | `50140` | active+routed |
| `/api/v1/audit`, `/api/v1/audit/sds`, retention and lineage-deletion aliases | `audit-compliance-service` | `50115` | active+routed |

`function-runtime-service` is implemented but intentionally **not** in
this table yet: its README states that `/api/v1/functions/*` gateway
registration and Helm wiring are follow-up work. The older ontology
function metadata route family remains owned by `ontology-actions-service`.

## All service binaries and workers

Status vocabulary:

- `active+routed`: product traffic is routed through the edge gateway.
- `active+not routed`: runnable service exists, but the edge gateway does
  not currently expose its product routes.
- `worker only`: background consumer, job, or runtime worker; no product
  gateway route is expected.
- `active stub`: runnable service exists primarily to return typed 501/health
  responses while full product behavior is completed.
- `integration pending`: meaningful implementation exists, but gateway,
  Helm, frontend, or cross-service cutover is not complete.

| Service directory | Plane / release | Default bind | Status | Primary role |
| --- | --- | ---: | --- | --- |
| `action-log-sink` | storage / data-engine | `9090` metrics | worker only | Kafka `ontology.actions.applied.v1` to Iceberg `action_log` |
| `agent-runtime-service` | compute / ml-aip | `50127` | active+routed | Agent runtime, prompt workflow compatibility, conversation surfaces |
| `ai-evaluation-service` | compute / ml-aip | `50075` | active+routed | AI guardrail and evaluation APIs |
| `ai-sink` | storage / ml-aip | `9090` metrics | worker only | Kafka `ai.events.v1` to Iceberg AI event tables |
| `application-composition-service` | control / apps-ops | `50140` | active+routed | Workshop/app composition, templates, publishing, application curation aliases |
| `audit-compliance-service` | control / apps-ops | `50115` | active+routed | Audit collection, retention, lineage deletion, SDS, GDPR, compliance posture |
| `audit-sink` | storage / apps-ops | `9090` metrics | worker only | Kafka `audit.events.v1` to Iceberg audit archive |
| `authorization-policy-service` | control / platform | `50093` | active+routed | Roles, permissions, groups, Cedar policies, restricted views, security-governance aliases |
| `cipher-service` | control / platform | `8080` | active stub / override-sensitive | Cipher route backing for `/api/v1/auth/cipher`; Helm dev can route gateway here |
| `code-repository-review-service` | state / apps-ops | `50155` | active+routed | Code repositories, review flows, legacy global branch route shape |
| `compute-module-service` | compute / apps-ops | `8080` | active+not routed | Compute module resources and future UDF/container module control plane |
| `connector-management-service` | ingestion / data-engine | `50088` | active+routed | Connector catalog, connections, credentials metadata, discovery, HyperAuto-style routes |
| `dataset-versioning-service` | state / data-engine | `50078` | active+routed | Dataset metadata, branches, transactions, files, quality aliases |
| `edge-gateway-service` | control / platform | `8080` | active ingress | Public HTTP edge, route selection, request IDs, rate limits, auth headers |
| `entity-resolution-service` | compute / apps-ops | `50058` | active+routed | Entity resolution and Fusion-style MDM APIs |
| `federation-product-exchange-service` | control / apps-ops | `50120` | active+routed | Federation, marketplace, product exchange, Nexus collaboration surfaces |
| `function-runtime-service` | compute / not Helm-wired | `50190` | integration pending | User-authored TypeScript/Python function registry, versioning, and invocation |
| `global-branch-service` | state / apps-ops | `8080` | integration pending | Milestone A global branch lifecycle CRUD and participation coordinator |
| `iceberg-catalog-service` | storage / data-engine | `50118` | active+routed | Iceberg REST catalog compatibility and OpenFoundry table-writer adapter |
| `iceberg-object-indexer` | compute / data-engine job | `9090` health/metrics | worker only | Iceberg table to object-database indexing job |
| `identity-federation-service` | control / platform | `50112` | active+routed | Login, refresh, MFA, SAML/OIDC/OAuth, service accounts, sessions |
| `ingestion-replication-service` | ingestion / data-engine | `50090` | active+routed | Ingest jobs, replication control plane, CDC metadata endpoints |
| `knowledge-index-service` | compute / ml-aip | `8080` | active stub / override-sensitive | Knowledge-base management placeholder; search routes go to retrieval-context |
| `lineage-service` | compute / data-engine | `50083` | active+routed | Dataset and column lineage APIs |
| `llm-catalog-service` | compute / ml-aip | `50095` | active+routed | AI provider/model catalog APIs |
| `media-sets-service` | state / data-engine | `50156` HTTP / `50157` gRPC | active+not routed | Media set metadata, items, branches, storage references |
| `media-transform-runtime-service` | compute / data-engine | `50173` | worker only | Worker runtime for media transforms scheduled by media sets |
| `model-catalog-service` | compute / ml-aip | `50085` | active+routed | ML experiments, runs, models, model versions |
| `model-deployment-service` | compute / ml-aip | `50086` | active+routed | Model deployments, prediction, drift, batch prediction APIs |
| `network-boundary-service` | control / platform | `8080` | active stub / override-sensitive | Network boundary and egress policy routes; Helm dev can route gateway here |
| `notebook-runtime-service` | compute / apps-ops | `50134` | active+routed | Notebooks, cells, sessions, kernels, notepad/reporting-style absorbed surfaces |
| `notification-alerting-service` | control / apps-ops | `50114` | active+routed | Notifications, inbox, delivery channels, alerting, websocket fanout |
| `object-database-service` | state / ontology | `50104` | active+routed | Object instances, link instances, revision history, transactional outbox |
| `ontology-actions-service` | control / ontology | `50106` | active+routed | Actions, inline edits, functions metadata, funnels, rules, policy-aware filters |
| `ontology-definition-service` | control / ontology | `50103` | active+routed | Object types, properties, interfaces, link types, action definitions, governance |
| `ontology-exploratory-analysis-service` | compute / ontology | `50131` | active+routed | Exploratory ontology analysis and geospatial-style APIs |
| `ontology-indexer` | compute / ontology | `50124` | worker only | Kafka/Cassandra to search backend projection worker |
| `ontology-query-service` | compute / ontology | `50105` | active+routed | Search, graph traversal, object sets, KNN, read models, projections |
| `pipeline-build-service` | compute / data-engine | `50081` | active+routed | Pipeline definitions, validation, preview/build execution, runs, schedules |
| `pipeline-runner` | compute / data-engine job | `9090` health/metrics | worker only | Generic pipeline-step runner used by pipeline-build-service |
| `pipeline-runner-spark` | compute / data-engine job | n/a | worker only | Spark/JVM transform runtime retained for Spark-specific jobs |
| `reindex-coordinator-service` | compute / ontology | `9090` | worker only | Foundry-pattern reindex coordinator |
| `report-service` | compute / apps-ops | `8080` | active stub / override-sensitive | Typed 501 backing for `/api/v1/reports*` until notebook/reporting cutover completes |
| `retrieval-context-service` | compute / ml-aip | `50098` | active+routed | Knowledge-base retrieval and RAG context search APIs |
| `sdk-generation-service` | control / apps-ops | `50144` | active+not routed | OpenAPI / TypeScript / Python / Java SDK generation hand-off endpoint |
| `solution-design-service` | control / apps-ops | `50142` | active+not routed | Solution/template authoring control plane |
| `sql-bi-gateway-service` | compute / data-engine | `50133` gRPC / `50134` HTTP | active+routed | Flight SQL / BI edge plus saved-query, warehouse, tabular-analysis HTTP CRUD |
| `telemetry-governance-service` | control / apps-ops | `50153` | active+routed | Monitoring views, monitor rules, telemetry governance |
| `tenancy-organizations-service` | control / platform | `50113` | active+routed | Tenants, organizations, enrollments, spaces, projects, workspace sharing |
| `workflow-automation-service` | control / apps-ops | `50137` | active+routed | Workflow orchestration, automation, saga substrate, approvals |

## Gateway upstream aliases (Helm parity)

The gateway's `UpstreamURLs` struct in
[`services/edge-gateway-service/internal/config/config.go`](../../services/edge-gateway-service/internal/config/config.go)
keeps legacy alias fields so one Helm profile can survive strangler-fig
cutovers. Some aliases resolve to a surviving owner; others can be
pointed at a re-stood stub service by environment-specific Helm values.

| Gateway upstream key | Default localhost target | Current interpretation |
| --- | --- | --- |
| `data_connector_service_url` | `connector-management-service` (`:50088`) | legacy alias for connector management |
| `ontology_service_url` | `ontology-definition-service` (`:50103`) | legacy alias for ontology definitions |
| `audit_service_url` | `audit-compliance-service` (`:50115`) | legacy alias for audit compliance |
| `ml_service_url` | `model-catalog-service` (`:50085`) | legacy ML fallback |
| `ai_service_url` | `agent-runtime-service` (`:50127`) | generic AI fallback |
| `security_governance_service_url` | `authorization-policy-service` (`:50093`) | absorbed surface |
| `cipher_service_url` | `authorization-policy-service` (`:50093`) unless Helm override points to `cipher-service:8080` | active stub exists; override-sensitive |
| `oauth_integration_service_url` | `identity-federation-service` (`:50112`) | absorbed surface |
| `session_governance_service_url` | `identity-federation-service` (`:50112`) | absorbed surface |
| `network_boundary_service_url` | `authorization-policy-service` (`:50093`) unless Helm override points to `network-boundary-service:8080` | active stub exists; override-sensitive |
| `checkpoints_purpose_service_url` | `authorization-policy-service` (`:50093`) | absorbed surface |
| `retention_policy_service_url` | `audit-compliance-service` (`:50115`) | absorbed surface |
| `lineage_deletion_service_url` | `audit-compliance-service` (`:50115`) | absorbed surface |
| `sds_service_url` | `audit-compliance-service` (`:50115`) | absorbed surface |
| `virtual_table_service_url` | `connector-management-service` (`:50088`) | reserved/connector-owned surface |
| `pipeline_authoring_service_url` | `pipeline-build-service` (`:50081`) | absorbed surface |
| `pipeline_schedule_service_url` | `pipeline-build-service` (`:50081`) | absorbed surface |
| `data_asset_catalog_service_url` | `dataset-versioning-service` (`:50078`) | absorbed surface |
| `dataset_quality_service_url` | `dataset-versioning-service` (`:50078`) | absorbed surface |
| `approvals_service_url` | `workflow-automation-service` (`:50137`) | absorbed surface |
| `app_builder_service_url` | `application-composition-service` (`:50140`) | absorbed surface |
| `application_curation_service_url` | `application-composition-service` (`:50140`) | absorbed surface |
| `model_evaluation_service_url` | `model-deployment-service` (`:50086`) | absorbed surface |
| `model_serving_service_url` | `model-deployment-service` (`:50086`) | absorbed surface |
| `model_inference_history_service_url` | `model-deployment-service` (`:50086`) | absorbed surface |
| `prompt_workflow_service_url` | `agent-runtime-service` (`:50127`) | absorbed surface |
| `knowledge_index_service_url` | placeholder `:50097` unless Helm override points to `knowledge-index-service:8080` | active stub exists; override-sensitive |
| `conversation_state_service_url` | `agent-runtime-service` (`:50127`) | absorbed surface |
| `document_reporting_service_url` | `notebook-runtime-service` (`:50134`) | absorbed surface |
| `report_service_url` | `notebook-runtime-service` (`:50134`) unless Helm override points to `report-service:8080` | active stub exists; override-sensitive |
| `geospatial_intelligence_service_url` | `ontology-exploratory-analysis-service` (`:50131`) | absorbed surface |
| `code_repo_service_url` | `code-repository-review-service` (`:50155`) | active owner |
| `global_branch_service_url` | `code-repository-review-service` (`:50155`) unless cutover override points to `global-branch-service:8080` | Milestone A service exists; frontend/gateway cutover pending |
| `marketplace_catalog_service_url` | `federation-product-exchange-service` (`:50120`) | absorbed surface |
| `product_distribution_service_url` | `federation-product-exchange-service` (`:50120`) | absorbed surface |
| `nexus_service_url` | `tenancy-organizations-service` (`:50113`) | Nexus spaces route; product exchange routes use federation service |

## Cross-service dependencies

Configuration files show explicit service-to-service defaults for several
domains:

- `connector-management-service` knows about dataset, pipeline, and ontology services.
- `ingestion-replication-service` knows about dataset, pipeline, and ontology services.
- `pipeline-build-service` depends on dataset, workflow, AI, and storage services.
- `lineage-service` depends on dataset, workflow, and AI services.
- `workflow-automation-service` depends on notification, ontology, and pipeline services.
- `ontology-definition-service` depends on audit, AI, and notification services.
- `object-database-service` depends on audit and notification services; all object/link writes go through it.
- `ontology-query-service` depends on `object-database-service`, `ontology-actions-service`, and AI services.
- `ontology-actions-service` depends on `object-database-service` and `ontology-definition-service`.
- `notebook-runtime-service` depends on query and AI services.
- `application-composition-service` owns app-builder/application-curation/developer-console style route families.

## Health convention

Current HTTP services expose `/healthz`. Some services also keep `/health`
as a compatibility alias. Worker binaries generally expose `/healthz` and
`/metrics` on a metrics/health address (`9090`) or omit a product HTTP
surface entirely.

`sql-bi-gateway-service` is gRPC-first on its Flight SQL port (`50133`)
and exposes HTTP `/healthz` plus saved-query, warehousing, and tabular
CRUD on companion port `50134`.
