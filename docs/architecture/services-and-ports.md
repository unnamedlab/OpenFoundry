# Services and Ports

This page is the first documentation-sync slice against the current Go source tree. It intentionally documents what is present under `services/` today instead of the older Rust-era target map.

## Source of truth

Use the code when this table drifts:

- Service inventory: `find services -mindepth 1 -maxdepth 1 -type d`.
- HTTP/gRPC defaults: each service's `internal/config/config.go`, plus service-local READMEs for worker-only binaries.
- Edge routing: `services/edge-gateway-service/internal/proxy/router_table.go`.
- Gateway compatibility upstream defaults: `services/edge-gateway-service/internal/config/config.go`.

As of this sync, the repository has **41** top-level service directories and **42** service command entrypoints. The extra entrypoint is `workflow-automation-service/cmd/approvals-timeout-sweep`, a service-local worker command rather than an extra deployable HTTP API service.

## Active service inventory

| Service | Default listener | Type | Primary role in current code |
| --- | --- | --- | --- |
| `edge-gateway-service` | `8080` HTTP | edge/control | Public HTTP gateway, request shaping, route selection, rate limits, auth headers, and stable error envelopes. |
| `identity-federation-service` | `50112` HTTP | control | Login, refresh, MFA, OAuth/OIDC/SAML, guest/scoped sessions, API keys, OAuth clients, and identity admin compatibility. |
| `tenancy-organizations-service` | `50113` HTTP | control/state | Tenant resolution, organizations, enrollments, spaces, projects, and sharing boundaries. |
| `authorization-policy-service` | `50115` HTTP | control/security | Roles, groups, permissions, restricted views, and policy/admin surfaces. |
| `audit-compliance-service` | `50116` HTTP | control/security | Audit overview/events/reports/policies, compliance posture, GDPR erase/export, retention, and lineage deletion surfaces. |
| `connector-management-service` | `50088` HTTP | ingestion | Connector catalog, connection definitions, discovery, credentials metadata, data-connection policy integration, and virtual-table resolution helpers. |
| `ingestion-replication-service` | `50120` HTTP | ingestion | Connector agents, sync jobs, replication runtime, and streaming/CDC orchestration. |
| `dataset-versioning-service` | `50117` HTTP | state/storage | Dataset catalog compatibility plus versions, branches, transactions, files, views, quality, linting, and retention-aware dataset operations. |
| `iceberg-catalog-service` | `50118` HTTP | storage | Iceberg REST catalog/admin endpoints and catalog client registration surfaces. |
| `media-sets-service` | `50121` HTTP / `50122` gRPC | storage/media | Media set metadata, path resolution, signed storage URLs, retention reaping, and gRPC media-set access. |
| `media-transform-runtime-service` | `50173` HTTP | compute/media | Media transform worker runtime with catalog, transform, and image/geospatial/spreadsheet handlers. |
| `pipeline-build-service` | `50081` HTTP | compute | Pipeline definitions/run execution, distributed compute coordination, Python sidecar execution, Spark runner integration, and Iceberg catalog wiring. |
| `pipeline-runner` | none; CLI worker | compute | SparkApplication orchestration binary that fetches pipeline specs from `pipeline-build-service` and delegates execution to `spark-submit`. |
| `lineage-service` | `50083` HTTP | compute/governance | Dataset and column lineage APIs. |
| `reindex-coordinator-service` | `9090` HTTP metrics/health | state/operations | Cassandra-backed ontology reindex queue coordinator and Kafka/Cassandra scan orchestration. |
| `ontology-definition-service` | `50122` HTTP | control/ontology | Ontology control plane for types, properties, interfaces, links, shared properties, bindings, and project governance. |
| `object-database-service` | `50125` HTTP | state/ontology | Object/link instance write authority, object database CRUD, and object storage-backed revision surfaces. |
| `ontology-query-service` | `50123` HTTP | compute/ontology | Ontology search, graph, quiver, object-set, KNN, object view, and read-model query surfaces. |
| `ontology-actions-service` | `50106` HTTP | control/ontology | Action execution, inline edits, batch/funnel ingestion, function runtime, rule execution, storage insights, and action metrics. |
| `ontology-indexer` | `50124` HTTP | compute/ontology | Ontology indexer runtime and projection/materialization worker APIs. |
| `ontology-exploratory-analysis-service` | `50131` HTTP | compute/ontology | Exploratory analysis, maps/geospatial layers, scenario simulation foundation, time-series analysis, and writeback support. |
| `workflow-automation-service` | `50137` HTTP | control | Workflow automation runtime, approval-related workflow execution, and scheduled/event workflow orchestration. |
| `notification-alerting-service` | `50114` HTTP | control | Notification send/history/preferences APIs, websocket fanout, channel delivery, NATS integration, and SMTP delivery configuration. |
| `notebook-runtime-service` | `50134` HTTP | compute | Notebook sessions, kernel/cell execution, and interactive runtime APIs. |
| `sql-bi-gateway-service` | `50133` Flight SQL gRPC / `50134` HTTP sidecar | compute | External BI/Flight SQL gateway plus HTTP health, saved queries, warehousing, and tabular-analysis side routes. |
| `entity-resolution-service` | `50058` HTTP | compute | Entity resolution and fusion-compatible matching surfaces. |
| `model-catalog-service` | `50085` HTTP | compute/ML | ML experiments, runs, model registry, model versions, and model catalog APIs. |
| `model-deployment-service` | `50086` HTTP | compute/ML | Deployments, predictions, batch predictions, drift checks, and serving/deployment lifecycle APIs. |
| `ai-evaluation-service` | `50075` HTTP | compute/AI | AI evaluation and guardrail-evaluation surfaces. |
| `agent-runtime-service` | `50127` HTTP | compute/AI | AI/agent runtime fallback, tools, chat/agent compatibility, and agent execution surfaces. |
| `llm-catalog-service` | `50095` HTTP | compute/AI | AI provider and LLM catalog APIs. |
| `retrieval-context-service` | `50098` HTTP | compute/AI | Retrieval-context and knowledge-base search APIs. |
| `application-composition-service` | `50140` HTTP | control/apps | Widget composition plus public app/template/version/publish composition routes. |
| `solution-design-service` | `50142` HTTP | control/apps | Solution design service APIs. |
| `federation-product-exchange-service` | `50126` HTTP | control/federation | Federation product exchange, nexus, and marketplace/devops compatibility routes. |
| `code-repository-review-service` | `50155` HTTP | control/devex | Code-repository review APIs. |
| `sdk-generation-service` | `50144` HTTP | developer toolchain | SDK/OpenAPI generation service APIs. |
| `telemetry-governance-service` | `50153` HTTP | governance/observability | Telemetry governance APIs. |
| `ai-sink` | `METRICS_ADDR` default `0.0.0.0:9090` | background worker | Kafka `ai.events.v1` consumer that writes AI events to JSONL/Iceberg writer backends and exposes health/metrics. |
| `audit-sink` | `METRICS_ADDR` default `0.0.0.0:9090` | background worker | Kafka `audit.events.v1` consumer that writes audit events to JSONL/Iceberg writer backends and exposes health/metrics. |
| `template` | `server.addr` in `services/template/config.yaml` | scaffold | Service template/scaffold; not a production domain service. |

## Gateway route ownership

The gateway routes by ordered prefix/suffix checks. The active route ownership is now consolidated around the services above:

| Route family | Routed to |
| --- | --- |
| `/api/v1/auth/sso`, `/api/v1/auth/login`, `/api/v1/auth/refresh`, `/api/v1/auth/mfa`, `/api/v1/auth/sessions`, `/api/v1/api-keys`, `/api/v1/applications`, `/api/v1/oauth/clients`, `/api/v1/external-integrations`, `/api/v1/control-panel`, `/api/v1/users/me`, and auth/admin catch-alls | `identity-federation-service` |
| `/api/v1/roles`, `/api/v1/permissions`, `/api/v1/groups`, `/api/v1/policies`, `/api/v1/restricted-views`, and role/group mutations under users | `authorization-policy-service` |
| `/api/v1/security-governance`, audit governance/classification/compliance posture routes | compatibility upstream `SecurityGovernance` in gateway config (default `localhost:50114`, which currently overlaps `notification-alerting-service`); implementation should be checked before adding new routes. |
| `/api/v1/network-boundaries`, `/api/v1/network-boundary`, `/api/v1/data-connection/egress-policies` | compatibility upstream `NetworkBoundary` in gateway config; no matching active service directory in this snapshot. |
| `/api/v1/checkpoints-purpose` | compatibility upstream `CheckpointsPurpose` in gateway config; no matching active service directory in this snapshot. |
| `/api/v1/retention`, dataset transaction retention suffixes, `/api/v1/lineage-deletions`, `/api/v1/audit/gdpr/erase`, `/api/v1/audit/overview`, `/api/v1/audit/events`, `/api/v1/audit/reports`, `/api/v1/audit/policies`, `/api/v1/audit/gdpr/export`, and remaining `/api/v1/audit` catch-all | `audit-compliance-service` |
| `/api/v1/tenancy`, `/api/v1/organizations`, `/api/v1/enrollments`, `/api/v1/spaces`, `/api/v1/projects`, `/api/v1/nexus/spaces`, `/api/v1/ontology/projects` | `tenancy-organizations-service` |
| `/api/v1/data-connection`, `/api/v1/connectors/catalog`, `/api/v1/connections` discovery/registration/virtual-table/hyperauto routes | `connector-management-service` |
| `/api/v1/connector-agents`, connection sync/sync-jobs routes | `ingestion-replication-service` |
| `/api/v1/datasets`, `/api/v2/filesystem`, dataset versions/transactions/branches/views/files/storage-details/quality/lint routes | `dataset-versioning-service` |
| `/api/v1/iceberg-tables`, `/iceberg/v1`, `/v1/iceberg-clients` | `iceberg-catalog-service` |
| `/api/v1/queries` | `sql-bi-gateway-service` |
| `/api/v1/pipelines` and `/api/v1/pipelines/triggers/cron` | `pipeline-build-service` |
| `/api/v1/lineage` | `lineage-service` |
| Ontology actions/functions/funnel/storage insights/rules/inline-edit/rule-runs routes | `ontology-actions-service` |
| Ontology search/graph/quiver/object-set/query/KNN routes | `ontology-query-service` |
| Ontology object and link-instance write routes | `object-database-service` |
| Ontology interfaces/shared-property-types/links/types and ontology catch-all | `ontology-definition-service` |
| `/api/v1/workflows` | `workflow-automation-service`; approval subroutes still point at a compatibility `Approvals` upstream in gateway config. |
| `/api/v1/notebooks`, `/api/v1/notepad` | `notebook-runtime-service` |
| `/api/v1/notifications` | `notification-alerting-service` |
| ML experiment/run/model/model-version routes | `model-catalog-service` |
| ML deployment/predict/batch-prediction/drift routes | `model-deployment-service` |
| `/api/v1/ml` fallback | compatibility `ML` upstream in gateway config; model catalog/deployment routes are more specific. |
| AI guardrails/evaluations | `ai-evaluation-service` |
| AI providers | `llm-catalog-service` |
| AI knowledge-base search | `retrieval-context-service` |
| AI tools and AI catch-all | `agent-runtime-service` |
| AI prompts, knowledge-base non-search, and conversations | compatibility upstreams in gateway config; no matching active service directories in this snapshot. |
| `/api/v1/entity-resolution`, `/api/v1/fusion` | `entity-resolution-service` |
| `/api/v1/streaming` | compatibility `Streaming` upstream in gateway config; the current active service is `media-sets-service` on `50121`, so route ownership needs follow-up before new streaming docs are added. |
| `/api/v1/reports` | compatibility `Report` upstream in gateway config; no matching active service directory in this snapshot. |
| `/api/v1/geospatial` | compatibility `GeospatialIntelligence` upstream in gateway config; geospatial behavior currently appears in ontology exploratory analysis and shared geospatial libraries. |
| `/api/v1/code-repos` | compatibility `CodeRepo` / `GlobalBranch` upstreams in gateway config; the current active service directory is `code-repository-review-service`. |
| `/api/v1/federation-product-exchange`, `/api/v1/nexus`, `/api/v1/marketplace`, marketplace devops/install routes | `federation-product-exchange-service` |
| `/api/v1/widgets`, selected `/api/v1/apps/public`, `/api/v1/apps/templates`, app version/publish/slate-package routes | `application-composition-service` |
| `/api/v1/apps` fallback | compatibility `AppBuilder` upstream in gateway config; no matching active service directory in this snapshot. |

## Health and metrics conventions

Most HTTP services expose `/healthz`; several also alias `/health` for legacy probes. Worker services (`ai-sink`, `audit-sink`, and `reindex-coordinator-service`) expose health/metrics on their metrics listener rather than a product API port.

`sql-bi-gateway-service` has two listeners: the primary Flight SQL gRPC port (`50133`) and an HTTP side router (`HEALTHZ_PORT`, default `50134`) for `/healthz`, saved queries, warehousing, and tabular-analysis routes.

## Known follow-ups

This sync deliberately keeps compatibility upstreams visible instead of silently deleting them. The next documentation pass should reconcile these gateway upstream names with active service directories and either document the missing active implementation or remove the stale route target from code/config:

- `Approvals`, `AppBuilder`, `CodeRepo`, `ConversationState`, `GeospatialIntelligence`, `GlobalBranch`, `KnowledgeIndex`, `ML`, `NetworkBoundary`, `ProductDistribution`, `PromptWorkflow`, `Report`, `SecurityGovernance`, `Streaming`, and `CheckpointsPurpose` compatibility upstreams.
- Port collisions that are intentional or need follow-up: `ontology-definition-service` HTTP `50122` and `media-sets-service` gRPC `50122`; `notebook-runtime-service` HTTP `50134` and `sql-bi-gateway-service` HTTP side router `50134`.
