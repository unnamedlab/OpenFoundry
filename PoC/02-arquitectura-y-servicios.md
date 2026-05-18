# 02 — Architecture and services to spin up

> Snapshot note: this PoC document is intentionally a demo-scope snapshot, not the canonical repository inventory. The current code-first inventory is **50 service directories** under `services/`; use [`docs/reference/repository-layout.md`](../docs/reference/repository-layout.md) for the authoritative service/library list. Spinning up all services for a demo is unmanageable and, if any one of them fails, it breaks the narrative. This document defines the **minimum viable subset for the PoC (~15 services)** and explicitly leaves the rest off but "listed as available".

---

## 🧩 Subset of services to spin up ("Foundry Minimum Viable Demo")

### Layer 1 — Base infrastructure (not OpenFoundry services, these are dependencies)
| Component | Technology | Purpose |
|---|---|---|
| Object storage | **MinIO** (local) or **S3** (cloud) | Iceberg/Delta data lake |
| Table catalog | **Apache Iceberg REST catalog** or Hive Metastore | Table metadata |
| Batch compute | **Apache Spark 3.5** or **DataFusion/Ballista** | Pipelines |
| Messaging | **Redpanda** (Kafka-compatible) | OpenSky streaming |
| OLTP | **PostgreSQL 16** | Service state |
| Cache + queue | **Redis 7** | Sessions, cache |
| Search | **OpenSearch** or **Meilisearch** | Catalog and ontology search |
| Identity | **Keycloak 24** | OIDC for `identity-federation-service` |
| Observability | **Prometheus + Grafana + Loki + Tempo** | Metrics, logs, traces |
| LLM | **Ollama (Llama 3.1 70B)** local + fallback **Azure OpenAI GPT-4o** | AIP copilot |

### Layer 2 — OpenFoundry services running in the PoC (~17)

> The subset uses only service names that physically exist in `services/<name>/cmd/<name>/main.go`. Where the "logical" capability (e.g. pipeline authoring + build + scheduling) is consolidated into a single binary, this is called out explicitly.

| # | Service | Role in the demo | Script act |
|---|---|---|---|
| 1 | `connector-management-service` | Defines data sources, REST webhooks, and OpenSky/S3/NOAA connectors | Act 1 |
| 2 | `ingestion-replication-service` | Batch + streaming ingestion (with `event-bus-data` lib over Kafka), branching and replication | Act 1 |
| 3 | `iceberg-catalog-service` | Iceberg REST catalog (Foundry-flavor): Iceberg lake metadata | Act 1 |
| 4 | `dataset-versioning-service` | Versioned datasets (branches, transactions, file APIs) | Acts 1, 6 |
| 5 | `lineage-service` | OpenLineage sink + end-to-end lineage graph | Acts 1, 3 |
| 6 | `pipeline-build-service` | Pipeline authoring, build, and orchestration (quality "expectations" are evaluated with the `pipeline-expression` lib) | Act 3 |
| 7 | `pipeline-runner` + `pipeline-runner-spark` | Spark orchestrator + Scala transforms JAR (Iceberg read/write) | Act 3 |
| 8 | `ontology-definition-service` | Defines the aviation model (object types, link types, action types) | Act 2 |
| 9 | `ontology-query-service` | Ontology reads (objects, links, schemas) | Acts 2, 4, 5 |
| 10 | `object-database-service` | Storage of ontological objects (Cassandra/Scylla) | Act 2 |
| 11 | `ontology-indexer` | Kafka worker that projects changes into the search backend | Act 2 |
| 12 | `ontology-actions-service` | Actions, funnels, functions, inline-edit on objects | Act 5 |
| 13 | `ontology-exploratory-analysis-service` | Time-series, geospatial, scenarios (covers what was previously thought of as "geospatial-intelligence") | Acts 1, 4 |
| 14 | `application-composition-service` + `apps/web` | Workshop App (composition, pages, widgets, publish runtime) + React 19 frontend | Act 4 |
| 15 | `agent-runtime-service` + `retrieval-context-service` + `llm-catalog-service` | AIP copilot: OpenAI-compatible chat, tools, RAG, LLM catalog | Act 5 |
| 16 | `workflow-automation-service` + `notification-alerting-service` | Workflow definitions, sagas, **approvals**, and notifications (approvals are part of `workflow-automation-service`, not a separate service) | Act 5 |
| + | `identity-federation-service` + `authorization-policy-service` + `audit-compliance-service` + `tenancy-organizations-service` | Security, audit, and multi-tenancy (cross-cutting — sessions live inside `identity-federation-service` + the `auth-middleware` lib) | Act 6 |
| + | `edge-gateway-service` | HTTP edge gateway: JWT, routing, and rate-limiting in front of `apps/web` | cross-cutting |

### Layer 3 — Services turned off but "documented as available"

Full list of real monorepo services that are **not** spun up in the PoC but are listed:

`ai-evaluation-service`, `ai-sink`, `audit-sink`, `code-repository-review-service`, `compute-module-service`, `entity-resolution-service`, `federation-product-exchange-service`, `media-sets-service`, `media-transform-runtime-service`, `model-catalog-service`, `model-deployment-service`, `notebook-runtime-service`, `reindex-coordinator-service`, `sdk-generation-service`, `solution-design-service`, `sql-bi-gateway-service`, `telemetry-governance-service`.

> 👉 Message to the customer: *"The PoC spins up ~15 services to keep the demo simple. The current platform inventory has 50 service directories in the monorepo, grouped into 6 Helm releases (`of-platform`, `of-data-engine`, `of-ontology`, `of-ml-aip`, `of-apps-ops`, `of-web`), ready to be enabled according to your roadmap."*

> 🧹 **Note on names**: earlier versions of this document mentioned services such as `event-streaming-service`, `data-asset-catalog-service`, `dataset-quality-service`, `pipeline-authoring-service`, `pipeline-schedule-service`, `geospatial-intelligence-service`, `app-builder-service`, `ai-application-generation-service`, `mcp-orchestration-service`, `approvals-service` or `session-governance-service`. Those pieces **do not exist as separate binaries**; their capabilities live inside the services listed above (e.g. streaming uses `ingestion-replication-service` + the `event-bus-data` lib; approvals are a workflow step in `workflow-automation-service`).

---

## 🗺️ Logical diagram (ASCII)

```
                ┌────────────────────────────────────────────────────┐
                │     apps/web (React 19 · Vite · TypeScript)        │
                │  Dashboard operacional · Workshop App · Copiloto   │
                └───────────────┬─────────────────────┬──────────────┘
                                │                     │
                       edge-gateway-service (JWT, routing, rate-limit)
                                │                     │
              ┌─────────────────▼─────┐   ┌───────────▼──────────────┐
              │ ontology-query-svc    │   │ agent-runtime-svc        │
              │ ontology-actions-svc  │   │ retrieval-context-svc    │
              │ object-database-svc   │   │ llm-catalog-svc          │
              │ ontology-exploratory- │   │ (Ollama / Azure OpenAI)  │
              │   analysis-svc        │   │                          │
              └─────────────┬─────────┘   └───────────┬──────────────┘
                            │                         │
              ┌─────────────▼─────────────────────────▼──────────────┐
              │            ontology-definition-service                │
              │     (Aircraft, Flight, Airport, MaintenanceEvent…)    │
              │      + ontology-indexer (Kafka → search backend)      │
              └─────────────┬─────────────────────────┬──────────────┘
                            │                         │
   ┌────────────────────────▼───────────┐  ┌──────────▼─────────────┐
   │ pipeline-build-service             │  │ workflow-automation-svc│
   │ pipeline-runner / pipeline-runner- │  │  (incluye aprobaciones │
   │   spark (Spark + Iceberg)          │  │   y workflow traces)   │
   │ pipeline-expression (lib: quality  │  │ notification-alerting- │
   │   checks declarativos)             │  │   service              │
   │ lineage-service (OpenLineage sink) │  │                        │
   └────────────────────────┬───────────┘  └────────────────────────┘
                            │
   ┌────────────────────────▼─────────────────────────────────────────┐
   │ dataset-versioning-service · iceberg-catalog-service             │
   │  + application-composition-service (Workshop apps + widgets)     │
   └────────────────────────┬─────────────────────────────────────────┘
                            │
   ┌────────────────────────▼─────────────────────────────────────────┐
   │ Iceberg on Ceph S3 (Lakekeeper)  ◀──  Spark (operator + jobs)    │
   └────────────────────────▲─────────────────────────────────────────┘
                            │
   ┌────────────────────────┴────────────────────────────────────────┐
   │ ingestion-replication-service                                   │
   │   (batch + streaming via event-bus-data lib sobre Kafka/Strimzi)│
   │ connector-management-service (REST/webhooks/data sources)       │
   └─────┬──────────┬────────┬──────────────────────────────┬────────┘
         │          │        │                              │
       NOAA       BTS    Synthetic                       OpenSky
       HRRR    On-Time     MRO                         (ADS-B live)

  [Transversal] identity-federation-service · authorization-policy-service
                audit-compliance-service · tenancy-organizations-service
```

---

## 🛠️ How to bring up the stack (when the time comes)

### Local (powerful laptop or dedicated Hetzner)
```bash
# From the repo root
cp .env.example .env
# Edit .env: set MinIO, Keycloak, etc. credentials.

# Base stack
docker compose -f compose.yaml up -d

# If we add a PoC-specific overlay (to be created in infra/)
docker compose -f compose.yaml -f infra/docker-compose.poc-aviation.yml up -d
```

> Pending task when running the PoC: **create `infra/docker-compose.poc-aviation.yml`** with ONLY the 15 subset services, generous resource profiles, and healthchecks. **Do not create it now** — that is implementation work.

### Cloud (recommended for the customer demo)
- **Deployment:** existing Helm charts in `infra/helm/apps/` (`of-platform`, `of-data-engine`, `of-ontology`, `of-ml-aip`, `of-apps-ops`, `of-web`) on **K3s** or **EKS Managed**.
- Operators (`infra/helm/operators/`): cert-manager, CNPG, Flink, K8ssandra, kube-prometheus-stack, Loki, OTel Collector, Promtail, Rook-Ceph, Strimzi, Tempo.
- Base infra (`infra/helm/infra/`): Cassandra, Ceph, Debezium, Flink jobs, Kafka, Kite, Lakekeeper, local registry, Mimir, observability, Postgres clusters, Spark jobs/operator, Trino, Vespa.
- **Minimum size:** see [`04-infraestructura-y-despliegue.md`](04-infraestructura-y-despliegue.md).

---

## ⚙️ Critical configuration per service

| Service | Key environment variables | Notes |
|---|---|---|
| `ingestion-replication-service` | `KAFKA_BROKERS`, `OPENSKY_USER`, `OPENSKY_PASS`, `S3_ENDPOINT`, `S3_BUCKET`, `MAX_PARALLELISM=8` | Covers batch ingestion + REST polling to OpenSky (live) and the NOAA sync |
| `iceberg-catalog-service` | `LAKEKEEPER_ENDPOINT`, `CATALOG_WAREHOUSE=s3://acme-poc/warehouse` | Iceberg REST API; consumed by Spark/Trino |
| `pipeline-build-service` | `SPARK_MASTER`, `EXECUTOR_MEMORY=8g`, `EXECUTORS=12` | Tune to the actual hardware; launches Spark jobs via `pipeline-runner` |
| `ontology-query-service` | `VESPA_ENDPOINT`, `CACHE_TTL=300` | Cache is key for p95 latency < 2s |
| `agent-runtime-service` | `LLM_PROVIDER=ollama\|azure`, `LLM_MODEL`, `EMBEDDING_MODEL` | Dual provider in case the network fails; OpenAI-compatible chat endpoint |
| `audit-compliance-service` | `AUDIT_SINK=postgres+s3`, `IMMUTABLE_RETENTION=7y` | Show the customer the immutability |
| `edge-gateway-service` | `JWT_ISSUER=keycloak.poc.openfoundry.dev`, `RATE_LIMIT_PER_IP=200` | Single HTTP frontend |

---

## ✅ Concrete actions (when the PoC is executed)

1. Audit with `go build ./services/<each-one-in-the-subset>` that the ~17 services compile and start.
2. Create `infra/docker-compose.poc-aviation.yml` with ONLY those services + dependencies (or, in cloud, a `values.yaml` that activates only the necessary Helm releases).
3. Document the port, healthcheck (`/healthz`), and dependencies of each service in a table in that YAML.
4. Configure Prometheus / kube-prometheus-stack to scrape the `/metrics` endpoints of the subset (the rest silenced).
5. Test cold start end-to-end and measure the time (target < 4 min).
