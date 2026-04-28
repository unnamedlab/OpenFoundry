# Services and Ports

All backend services expose a health endpoint and bind to fixed default ports in local development. The edge gateway listens on `8080` and proxies public traffic to these internal services.

## Service Map

| Service | Default Port | Primary Role |
| --- | --- | --- |
| `gateway` | `8080` | Edge routing, HTTP compatibility, CORS, request IDs, rate limiting, tenant-aware access enforcement |
| `identity-federation-service` | `50112` | Login, refresh, MFA, SAML/OIDC/OAuth flows, service account tokens, scoped/guest sessions |
| `auth-service` | `50051` | User administration and temporary auth compatibility |
| `tenancy-organizations-service` | `50113` | Tenant resolution, organizations, enrollments, spaces, projects, and sharing boundaries |
| `data-connector` | `50052` | Connector hyperautomation and discovery orchestration |
| `connector-management-service` | `50088` | Connector catalog, connections, capabilities, credentials metadata, and connection testing |
| `ingestion-replication-service` | `50090` | Sync jobs, batch and micro-batch ingestion, export flows, refresh policies, connector agents, and scheduler runtime |
| `dataset-service` | `50053` | Datasets, versions, branches, filesystem, quality, linting |
| `streaming-service` | `50054` | Streaming pipelines and archive management |
| `sql-bi-gateway-service` | `50133` | Query execution surface and SQL/BI compatibility gateway |
| `pipeline-service` | `50056` | Pipeline compatibility shell during service decomposition |
| `pipeline-authoring-service` | `50080` | Pipeline definitions, validation, compilation, pruning, and executable plan generation |
| `pipeline-build-service` | `50081` | Pipeline run execution and retry orchestration |
| `pipeline-schedule-service` | `50082` | Shared schedule orchestration for pipeline and workflow cron/event triggers, due runs, windows, and backfills |
| `lineage-service` | `50083` | Dataset and column lineage APIs |
| `ontology-service` | `50057` | Object types, links, search, graph, actions, simulation |
| `fusion-service` | `50058` | Fusion and spreadsheet-oriented interactions |
| `ml-service` | `50059` | Experiments, training, registry, model lifecycle |
| `ai-service` | `50060` | AI providers, chat, tools, workflows |
| `workflow-automation-service` | `50137` | Workflow orchestration and execution runtime |
| `notebook-runtime-service` | `50134` | Notebook kernels, cells, sessions, and interactive execution |
| `document-reporting-service` | `50102` | Notepad-style documents and document reporting surfaces |
| `app-builder-service` | `50063` | App composition and runtime surfaces |
| `report-service` | `50064` | Report generation and delivery |
| `code-repo-service` | `50065` | Code repository APIs |
| `marketplace-service` | `50066` | Marketplace and catalog APIs |
| `nexus-service` | `50067` | Federation, sharing, and multi-org collaboration |
| `geospatial-service` | `50068` | Geospatial and mapping APIs |
| `notification-alerting-service` | `50114` | Notification transport, inbox APIs, delivery channels, alerting, and websocket fanout |
| `audit-service` | `50070` | Audit collection and export |

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
- `/api/v1/ontology` -> `ontology-service`
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
- `ontology-service` depends on audit and AI services
- `report-service` depends on dataset and geospatial services
- `notebook-runtime-service` depends on query and AI services
- `marketplace-service` depends on app-builder

## Health Convention

Every service exposes a `/health` route. This shared convention is used by:

- local runtime scripts
- GitHub Actions smoke jobs
- Helm health probes and operational checks
