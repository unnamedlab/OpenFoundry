# Repository Layout

Use this page when you need to quickly answer “where should this change live?” The active backend source is Go, not Rust: services live under `services/<name>/cmd/<binary>/main.go`, shared packages live under `libs/`, and root backend commands are driven primarily by `make`.

## Runtime Code

| Path | What Lives There |
| --- | --- |
| `services/edge-gateway-service` | HTTP edge routing, compatibility proxying, tenant-aware headers, request middleware, health, and rate-limit integration points. |
| `services/identity-federation-service` | Authentication, OIDC/SAML/OAuth flows, service accounts, session security, SCIM, MFA/WebAuthn, JWKS, and Cedar-aware authz integration. |
| `services/authorization-policy-service` | Cedar policy storage/evaluation surfaces and policy bundle/runtime support. |
| `services/tenancy-organizations-service` | Tenant, organization, enrollment, space, project, and sharing-boundary APIs. |
| `services/connector-management-service` | Connectors, discovery, sync, catalog bridge, egress, HTTP runtime, and open table catalog integration. |
| `services/dataset-versioning-service` | Dataset versioning, backing filesystem integration, retention, domain models, handlers, and repository code. |
| `services/sql-bi-gateway-service` | SQL/BI gateway APIs and query-facing service runtime. |
| `services/iceberg-catalog-service` | Iceberg REST catalog APIs, auth, markings, audit, token handling, metrics, and repository code. |
| `services/pipeline-build-service` | Pipeline build APIs, authoring/runtime parity surfaces, handlers, repository code, and server runtime. |
| `services/pipeline-runner` | Pipeline execution runner entrypoint and runtime packages. |
| `services/object-database-service` | Object storage/query-facing service APIs and object database runtime code. |
| `services/ontology-definition-service` | Ontology definition APIs backed by ontology kernel models and handlers. |
| `services/ontology-actions-service` | Ontology action execution, upload, inline edit, what-if, side effects, and action metrics surfaces. |
| `services/ontology-query-service` | Ontology query APIs and object-set/query runtime integration. |
| `services/ontology-exploratory-analysis-service` | Ontology exploratory analytics APIs and related service runtime. |
| `services/ontology-indexer` / `services/reindex-coordinator-service` | Ontology indexing and reindex coordination runtime. |
| `services/workflow-automation-service` | Workflow orchestration service plus the `approvals-timeout-sweep` command. |
| `services/notebook-runtime-service` | Notebook runtime APIs and service runtime. |
| `services/application-composition-service` | Application composition APIs, models, handlers, and repository code. |
| `services/ai-*`, `services/llm-catalog-service`, `services/retrieval-context-service` | AI event sinks, agent/evaluation/runtime surfaces, LLM catalog, and retrieval context services. |
| `services/model-*`, `services/media-*`, `services/entity-resolution-service` | ML/model catalog and deployment services, media set/transform services, and entity resolution APIs. |
| `services/audit-*`, `services/lineage-service`, `services/telemetry-governance-service` | Audit sink/compliance APIs, lineage, telemetry governance, and related runtime code. |
| `services/federation-product-exchange-service`, `services/notification-alerting-service`, `services/sdk-generation-service`, `services/code-repository-review-service`, `services/solution-design-service` | Federation, notification, SDK generation, repository review, and solution design services. |
| `services/template` | Go service scaffold/reference layout for new services. |

## Shared Libraries

| Path | Purpose |
| --- | --- |
| `libs/core-models` | Shared IDs, errors, health, pagination, timestamp, dataset, media, and security model primitives. |
| `libs/auth-middleware` / `libs/authz-cedar-go` | JWT/HTTP auth middleware and Cedar authorization helpers. |
| `libs/observability` | Logging, tracing, metrics, and cost model helpers. |
| `libs/event-bus-control` / `libs/event-bus-data` | Control-plane and data-plane messaging abstractions. |
| `libs/audit-trail`, `libs/outbox`, `libs/idempotency`, `libs/saga`, `libs/state-machine` | Reliability, audit, idempotency, saga, and state-machine primitives. |
| `libs/cassandra-kernel`, `libs/ontology-kernel`, `libs/query-engine`, `libs/pipeline-expression` | Domain kernels and expression/query runtime logic shared by services. |
| `libs/search-abstraction`, `libs/vector-store`, `libs/storage-abstraction` | Search, vector, and object/storage abstraction layers. |
| `libs/ai-kernel-go`, `libs/ml-kernel-go`, `libs/geospatial-*`, `libs/media-scanner` | AI, ML, geospatial, and media runtime helpers. |
| `libs/proto-gen` | Generated Go protobuf packages emitted by Buf. |
| `libs/testing` | Test helpers and fixtures for Go package and integration tests. |

## UI and Contracts

| Path | Purpose |
| --- | --- |
| `apps/web` | Main React + Vite product frontend. |
| `apps/web/src` | Frontend source, including `main.tsx`, `router.tsx`, route components, styles, and type shims. |
| `apps/web/public/generated/openapi` | Committed OpenAPI contract consumed by frontend and downstream tooling. |
| `apps/web/public/generated/terraform` | Committed Terraform schema for UI and portal use. |
| `proto` | Protobuf source contracts and Buf configuration. |
| `libs/proto-gen` | Generated Go protobuf packages. |
| `sdks/typescript`, `sdks/python`, `sdks/java` | Generated SDK packages and package metadata. |
| `python/openfoundry_pyruntime` | Python runtime package for sidecar/runtime flows. |

## Tooling

| Path | Purpose |
| --- | --- |
| `Makefile` | Primary Go build/test/generate/lint/CI command surface. |
| `justfile` | Cross-cutting developer recipes for frontend, docs, infra, proto, smoke, benchmarks, and legacy parity gates. |
| `tools/of-cli` | Generation, smoke, benchmarks, mock providers, and related platform tooling. |
| `smoke/scenarios` | Scenario-driven smoke definitions. |
| `benchmarks/scenarios` | Benchmark definitions. |
| `tests/integration/pyiceberg` | PyIceberg integration tests for Iceberg catalog compatibility. |

## Delivery

| Path | Purpose |
| --- | --- |
| `compose.yaml` | Root local Compose entrypoint. |
| `infra/compose` | Compose overlays, local assets, init scripts, and Nginx support. |
| `infra/helm` | Split Helm charts for apps, docs, infra, operators, shared templates, and profiles. |
| `infra/terraform` | Terraform modules and generated/provider schema outputs. |
| `infra/observability` | Prometheus rules and Grafana dashboards. |
| `infra/test-tools` | Chaos and load-benchmark tooling. |
| `.github/workflows` | CI/CD, docs, integration, security, infra, and release pipelines. |
| `docs/` | Technical documentation website. |
