# Repository Layout

Use this page when you need to quickly answer "where should this change live?"
The implementation inventory is source-owned: `services/*` and `libs/*`
are the source of truth, and this page should be re-checked with
`find services -mindepth 1 -maxdepth 1 -type d` and
`find libs -mindepth 1 -maxdepth 1 -type d` whenever service or library
counts change.

## Runtime Code

`services/` currently contains **50 service directories** under the single
Go module rooted at `github.com/openfoundry/openfoundry-go`. Not every
service is a public gateway target: some are workers, some are active
stubs, and some have implementation complete while gateway/Helm/frontend
cutover remains pending. The textual boilerplate every new service starts
from lives at `docs/templates/service-skeleton/` (its `.go` files carry
`//go:build ignore` so the toolchain skips them in place).

Grouping below follows the Helm app releases under `infra/helm/apps/`
(`of-platform`, `of-data-engine`, `of-ontology`, `of-ml-aip`,
`of-apps-ops`, `of-web`) plus explicit notes for workers and integration
pending services.

### Platform (gateway, identity, authz, tenancy)

| Path | What Lives There | Current status |
| --- | --- | --- |
| `services/edge-gateway-service` | edge routing, JWT validation, rate limiting, request fan-out | active+routed ingress |
| `services/identity-federation-service` | login, MFA, WebAuthn, OIDC/SAML/OAuth flows, service accounts, sessions, SCIM, JWKS rotation | active+routed |
| `services/authorization-policy-service` | Cedar-backed ABAC/RBAC decision point, policy CRUD, restricted views, security-governance aliases | active+routed |
| `services/tenancy-organizations-service` | tenant resolution, organizations, workspace enrollments, projects, sharing boundaries | active+routed |
| `services/cipher-service` | cipher route backing for `/api/v1/auth/cipher` | active stub; gateway override-sensitive |
| `services/network-boundary-service` | network boundary and egress-policy route backing | active stub; gateway override-sensitive |

### Data engine (ingestion, datasets, lineage, pipelines, BI)

| Path | What Lives There | Current status |
| --- | --- | --- |
| `services/connector-management-service` | data sources, REST webhooks, connector runtime, discovery and HyperAuto-style route families | active+routed |
| `services/ingestion-replication-service` | batch + streaming ingestion, connector agents, replication, CDC metadata | active+routed |
| `services/dataset-versioning-service` | datasets, branches, transactions, file APIs, dataset quality aliases | active+routed |
| `services/iceberg-catalog-service` | Iceberg REST catalog (Foundry flavor) over Lakekeeper plus table-writer adapter | active+routed |
| `services/iceberg-object-indexer` | Iceberg table to object-database indexing job | worker only |
| `services/lineage-service` | OpenLineage sink, lineage graph queries | active+routed |
| `services/media-sets-service` | media set CRUD, items, branches, storage references | active+not routed |
| `services/media-transform-runtime-service` | image / PDF / OCR / geospatial transforms | worker only |
| `services/pipeline-build-service` | pipeline authoring, validation, preview/build orchestration, run history and schedules | active+routed |
| `services/pipeline-runner` | generic Go pipeline-step runner | worker only |
| `services/pipeline-runner-spark` | Spark/JVM transform runtime retained for Spark-specific jobs | worker only |
| `services/sql-bi-gateway-service` | Apache Arrow Flight SQL server, BI edge, saved-query/warehouse/tabular HTTP CRUD | active+routed |
| `services/action-log-sink` | Kafka `ontology.actions.applied.v1` → Iceberg `action_log` consumer | worker only |

### Ontology

| Path | What Lives There | Current status |
| --- | --- | --- |
| `services/ontology-definition-service` | object types, properties, link types, action types, function package metadata | active+routed |
| `services/object-database-service` | object/link storage (Cassandra/Scylla) — write authority | active+routed |
| `services/ontology-query-service` | read plane: search, graph, object views, KNN, object sets | active+routed |
| `services/ontology-actions-service` | action validation/planning/execution; also hosts funnels, rules, and ontology function metadata | active+routed |
| `services/ontology-indexer` | Kafka/Cassandra worker projecting ontology changes into the search backend | worker only |
| `services/ontology-exploratory-analysis-service` | time-series, geospatial, scenarios | active+routed |
| `services/reindex-coordinator-service` | Foundry-pattern reindex coordinator | worker only |

### ML / AIP

| Path | What Lives There | Current status |
| --- | --- | --- |
| `services/model-catalog-service` | model adapter, lifecycle CRUD, experiments | active+routed |
| `services/model-deployment-service` | model serving runtime adapter, prediction, drift, batch prediction | active+routed |
| `services/agent-runtime-service` | agent runtime API + OpenAI-compatible chat endpoint | active+routed |
| `services/llm-catalog-service` | LLM provider/model catalog | active+routed |
| `services/retrieval-context-service` | RAG context retrieval/search surface | active+routed |
| `services/knowledge-index-service` | knowledge-base management placeholder backing typed 501 responses | active stub; gateway override-sensitive |
| `services/ai-evaluation-service` | LLM evaluation + guardrail benchmarking | active+routed |
| `services/ai-sink` | Kafka `ai.events.v1` → Iceberg consumer | worker only |

### Apps & ops

| Path | What Lives There | Current status |
| --- | --- | --- |
| `services/application-composition-service` | Workshop app composition, pages, widgets, publish runtime, application curation aliases | active+routed |
| `services/notebook-runtime-service` | notebooks: CRUD, cells, sessions, kernels, export, notepad/reporting-style absorbed surfaces | active+routed |
| `services/workflow-automation-service` | workflow definitions, sagas, automation conditions, **approval steps** | active+routed |
| `services/notification-alerting-service` | notifications inbox, delivery, WebSocket fan-out | active+routed |
| `services/telemetry-governance-service` | telemetry permissions, export policies, monitoring rules | active+routed |
| `services/federation-product-exchange-service` | marketplace, product distribution, federation registry and Nexus collaboration capability | active+routed |
| `services/code-repository-review-service` | code repository review plane and legacy global branch route shape | active+routed |
| `services/global-branch-service` | Milestone A global branch lifecycle CRUD and participation coordinator | integration pending; gateway/frontend cutover pending |
| `services/sdk-generation-service` | SDK + OpenAPI contract generation/publication | active+not routed |
| `services/solution-design-service` | solution design plane | active+not routed |
| `services/entity-resolution-service` | match rules, merge strategies, fuzzy-matching (Fusion/MDM) | active+routed |
| `services/compute-module-service` | compute module resources | active+not routed |
| `services/function-runtime-service` | user-authored TypeScript/Python function registry, versioning, and invocation | integration pending; gateway/Helm wiring pending |
| `services/report-service` | typed 501 backing for `/api/v1/reports*` until notebook/reporting cutover completes | active stub; gateway override-sensitive |
| `services/audit-compliance-service` | audit ledger, retention policies, lineage deletion subsystem, SDS/GDPR surfaces | active+routed |
| `services/audit-sink` | Kafka → Iceberg consumer for `audit.events.v1` | worker only |

> Older docs referenced legacy service names such as `ontology-service`,
> `auth-service`, `audit-service`, `data-connector`, `pipeline-service`,
> `dataset-service`, `ai-service`, `ml-service`, `marketplace-service`,
> `document-reporting-service`, `fusion-service`, `streaming-service`,
> `nexus-service`, `dataset-quality-service`, `lineage-deletion-service`,
> `event-streaming-service`, and `data-asset-catalog-service`. Those names
> are not current service directories. Their capabilities are consolidated
> in the services above or represented by gateway alias fields documented in
> [Services and Ports](../architecture/services-and-ports.md). Do not list a
> service as "non-existent" if a `services/<name>` directory is present;
> instead mark it with one of the statuses above.

## Shared Libraries

`libs/` currently contains **36 cross-cutting Go packages**:

- `ai-kernel-go`
- `analytical-logic`
- `audit-trail`
- `auth-middleware`
- `authz-cedar-go`
- `capabilities`
- `cassandra-kernel`
- `core-models`
- `db-pool`
- `event-bus-control`
- `event-bus-data`
- `event-scheduler`
- `geospatial-core`
- `geospatial-tiles`
- `idempotency`
- `media-scanner`
- `ml-kernel-go`
- `observability`
- `ontology-kernel`
- `outbox`
- `pipeline-expression`
- `pipeline-plan`
- `pipeline-runtime`
- `plugin-sdk`
- `proto-gen`
- `python-sidecar`
- `query-engine`
- `restrictedview`
- `saga`
- `scheduling-cron`
- `scheduling-linter`
- `search-abstraction`
- `state-machine`
- `storage-abstraction`
- `testing`
- `vector-store`

## UI and Contracts

| Path | Purpose |
| --- | --- |
| `apps/web` | main product frontend (React 19 + Vite + TypeScript) |
| `apps/web/src/routes` | route components (Workshop, ontology, datasets, pipelines, AI, audit, …) |
| `proto/` | Protobuf source of truth; Go generated to `libs/proto-gen/` |
| `sdks/typescript`, `sdks/python`, `sdks/java` | generated client SDKs |

## Tooling

| Path | Purpose |
| --- | --- |
| `tools/of-cli` | OpenAPI/SDK generation, smoke, benchmarks, mock provider |
| `smoke/scenarios` | scenario-driven smoke definitions |
| `benchmarks/scenarios` | benchmark definitions |
| `Makefile` | canonical task runner (`make tools`, `make ci`, `make gen`, …) |
| `justfile` | thin shim over `make` (Makefile is authoritative) |

## Delivery

| Path | Purpose |
| --- | --- |
| `infra/compose/docker-compose.yml` + `docker-compose.dev.yml` | local infrastructure |
| `infra/helm/infra` | Kubernetes platform layer: third-party charts, operator CRs, bootstrap manifests |
| `infra/helm/operators` | operator charts (cert-manager, CNPG, Flink, K8ssandra, Rook-Ceph, Strimzi, kube-prometheus-stack, Loki, Tempo, OTel Collector, Promtail) |
| `infra/helm/apps` | Kubernetes app layer: `of-platform`, `of-data-engine`, `of-ontology`, `of-ml-aip`, `of-apps-ops`, `of-web` |
| `infra/terraform/providers/openfoundry` | Terraform provider schema output |
| `infra/argocd` | ArgoCD GitOps assets |
| `.github/workflows` | CI/CD pipelines (`openfoundry-go.yml`, `ci-frontend.yml`, `proto-check.yml`, `security-audit.yml`, `chaos-smoke.yml`) |
| `docs/` | technical documentation website (VitePress) |
