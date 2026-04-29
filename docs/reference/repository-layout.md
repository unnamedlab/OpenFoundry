# Repository Layout

Use this page when you need to quickly answer “where should this change live?”

## Runtime Code

| Path | What Lives There |
| --- | --- |
| `services/edge-gateway-service` | edge routing, HTTP compatibility, tenant resolution, and rate limiting |
| `services/gateway` | legacy gateway source kept temporarily for migration compatibility |
| `services/identity-federation-service` | login, MFA, SSO/OIDC/SAML/OAuth flows, service accounts, session authentication |
| `services/auth-service` | user administration and temporary legacy auth compatibility |
| `services/tenancy-organizations-service` | tenant resolution, organizations, enrollments, spaces, projects, and sharing boundaries |
| `services/data-connector` | connectors, discovery, sync |
| `services/dataset-service` | datasets, versions, files, quality |
| `services/sql-bi-gateway-service` | query execution and SQL/BI compatibility |
| `services/pipeline-service` | pipeline runtime |
| `services/ontology-service` | object model, graph, actions, simulation |
| `services/workflow-automation-service` | workflow orchestration |
| `services/notebook-runtime-service` | notebook runtime APIs |
| `services/document-reporting-service` | document reporting and notepad-style surfaces |
| `services/application-composition-service` | app composition runtime |
| `services/fusion-service` | fusion/spreadsheet APIs |
| `services/ml-service` | experiments, training, registry |
| `services/ai-service` | model and tool orchestration |
| `services/document-reporting-service` | report generation, delivery, and notepad documents |
| `services/geospatial-service` | geospatial APIs |
| `services/code-repo-service` | repository APIs |
| `services/marketplace-catalog-service` | marketplace catalog, listings, installs, and discovery APIs |
| `services/federation-product-exchange-service` | federation and sharing runtime outside tenancy-owned spaces |
| `services/notification-alerting-service` | notifications |
| `services/audit-service` | audit ingestion and export |

## Shared Libraries

`libs/` contains cross-cutting crates such as auth middleware, storage abstraction, vector primitives, audit helpers, and testing utilities.

## UI and Contracts

| Path | Purpose |
| --- | --- |
| `apps/web` | main product frontend |
| `apps/web/static/generated/openapi` | committed OpenAPI contract |
| `apps/web/static/generated/terraform` | committed Terraform schema for UI and portal use |

## Tooling

| Path | Purpose |
| --- | --- |
| `tools/of-cli` | generation, smoke, benchmarks, mock provider |
| `smoke/scenarios` | scenario-driven smoke definitions |
| `benchmarks/scenarios` | benchmark definitions |
| `justfile` | contributor entry points |

## Delivery

| Path | Purpose |
| --- | --- |
| `infra/docker-compose*.yml` | local infrastructure |
| `infra/k8s/helm/open-foundry` | Kubernetes delivery |
| `infra/terraform/providers/openfoundry` | Terraform provider schema output |
| `.github/workflows` | CI/CD pipelines |
| `docs/` | technical documentation website |
