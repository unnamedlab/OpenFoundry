# 02 — Arquitectura y servicios a encender

> El repo OpenFoundry tiene **~95 microservicios**. Encender todos en una demo es ingobernable y, si alguno falla, rompe la narrativa. Este documento define el **subset mínimo viable para la PoC (≈ 15 servicios)** y deja el resto explícitamente apagado pero "listado como disponible".

---

## 🧩 Subset de servicios a encender ("Foundry Minimum Viable Demo")

### Capa 1 — Infraestructura base (no son servicios OpenFoundry, son dependencias)
| Componente | Tecnología | Para qué |
|---|---|---|
| Object storage | **MinIO** (local) o **S3** (cloud) | Lago de datos Iceberg/Delta |
| Catálogo de tablas | **Apache Iceberg REST catalog** o Hive Metastore | Metadatos de las tablas |
| Cómputo batch | **Apache Spark 3.5** o **DataFusion/Ballista** | Pipelines |
| Mensajería | **Redpanda** (compatible Kafka) | Streaming OpenSky |
| OLTP | **PostgreSQL 16** | Estado de servicios |
| Cache + cola | **Redis 7** | Sesiones, cache |
| Search | **OpenSearch** o **Meilisearch** | Búsqueda en catálogo y ontología |
| Identidad | **Keycloak 24** | OIDC para `identity-federation-service` |
| Observabilidad | **Prometheus + Grafana + Loki + Tempo** | Métricas, logs, trazas |
| LLM | **Ollama (Llama 3.1 70B)** local + fallback **Azure OpenAI GPT-4o** | Copiloto AIP |

### Capa 2 — Servicios OpenFoundry encendidos en la PoC (15)

| # | Servicio | Rol en la demo | Acto del guion |
|---|---|---|---|
| 1 | `connector-management-service` | Define los conectores OpenSky / S3 / NOAA | Acto 1 |
| 2 | `ingestion-replication-service` | Ejecuta ingestas batch y CDC | Acto 1 |
| 3 | `event-streaming-service` | Pipeline streaming OpenSky → Iceberg | Acto 1 |
| 4 | `dataset-versioning-service` | Datasets versionados (branches) | Actos 1, 6 |
| 5 | `data-asset-catalog-service` | Catálogo navegable | Acto 1 |
| 6 | `lineage-service` | Lineage end-to-end | Actos 1, 3 |
| 7 | `dataset-quality-service` | Reglas de calidad sobre datasets | Acto 3 |
| 8 | `ontology-definition-service` | Define el modelo de aviación | Acto 2 |
| 9 | `ontology-query-service` | Consulta de objetos y relaciones | Actos 2, 4, 5 |
| 10 | `ontology-actions-service` | Acciones disparables sobre objetos | Acto 5 |
| 11 | `pipeline-authoring-service` + `pipeline-build-service` + `pipeline-schedule-service` | Pipelines (los 3 cuentan como 1 grupo) | Acto 3 |
| 12 | `geospatial-intelligence-service` | Mapa de tracks ADS-B | Actos 1, 4 |
| 13 | `app-builder-service` + `apps/web` | Workshop App + dashboards | Acto 4 |
| 14 | `ai-application-generation-service` + `mcp-orchestration-service` + `retrieval-context-service` | Copiloto AIP | Acto 5 |
| 15 | `workflow-automation-service` + `notification-alerting-service` + `approvals-service` | Workflow MRO end-to-end | Acto 5 |
| + | `identity-federation-service` + `authorization-policy-service` + `audit-compliance-service` + `session-governance-service` | Seguridad y audit (transversales) | Acto 6 |

### Capa 3 — Servicios apagados pero "documentados como disponibles"

Listar abiertamente al cliente que existen pero no se demuestran hoy:
`agent-runtime-service`, `ai-evaluation-service`, `analytical-logic-service`, `cdc-metadata-service`, `cipher-service`, `code-repository-review-service`, `code-security-scanning-service`, `compute-modules-*`, `conversation-state-service`, `custom-endpoints-service`, `developer-console-service`, `document-intelligence-service`, `document-reporting-service`, `edge-gateway-service`, `entity-resolution-service`, `execution-observability-service`, `federation-product-exchange-service`, `global-branch-service`, `health-check-service`, `knowledge-index-service`, `lineage-deletion-service`, `llm-catalog-service`, `managed-workspace-service`, `marketplace-*`, `ml-experiments-service`, `model-*` (catalog, deployment, evaluation, inference-history, lifecycle, serving), `monitoring-rules-service`, `network-boundary-service`, `nexus-service`, `notebook-runtime-service`, `oauth-integration-service`, `object-database-service`, `ontology-exploratory-analysis-service`, `ontology-functions-service`, `ontology-funnel-service`, `ontology-security-service`, `ontology-timeseries-analytics-service`, `prompt-workflow-service`, `product-distribution-service`, `report-service`, `retention-policy-service`, `scenario-simulation-service`, `sdk-generation-service`, `sds-service`, `security-governance-service`, `solution-design-service`, `spreadsheet-computation-service`, `sql-bi-gateway-service`, `sql-warehousing-service`, `tabular-analysis-service`, `telemetry-governance-service`, `tenancy-organizations-service`, `time-series-data-service`, `tool-registry-service`, `virtual-table-service`, `widget-registry-service`, `workflow-trace-service`, `checkpoints-purpose-service`, `conversation-state-service`.

> 👉 Mensaje al cliente: *"La PoC enciende ~15 servicios para mantener la demo simple. La plataforma cuenta con 95 servicios listos para activarse según vuestra hoja de ruta."*

---

## 🗺️ Diagrama lógico (ASCII)

```
                ┌────────────────────────────────────────────────────┐
                │                  apps/web (UI)                     │
                │  Dashboard operacional · Workshop App · Copiloto   │
                └───────────────┬─────────────────────┬──────────────┘
                                │                     │
              ┌─────────────────▼─────┐   ┌───────────▼──────────────┐
              │ ontology-query-svc    │   │ ai-app-generation-svc    │
              │ ontology-actions-svc  │   │ mcp-orchestration-svc    │
              │ geospatial-intel-svc  │   │ retrieval-context-svc    │
              └─────────────┬─────────┘   └───────────┬──────────────┘
                            │                         │
              ┌─────────────▼─────────────────────────▼──────────────┐
              │            ontology-definition-service                │
              │     (Aircraft, Flight, Airport, MaintenanceEvent…)    │
              └─────────────┬─────────────────────────┬──────────────┘
                            │                         │
   ┌────────────────────────▼───────────┐  ┌──────────▼─────────────┐
   │ pipeline-authoring/build/schedule  │  │ workflow-automation-svc│
   │  + dataset-quality-service          │  │ + notification-alerting│
   │  + lineage-service                  │  │ + approvals-service    │
   └────────────────────────┬───────────┘  └────────────────────────┘
                            │
   ┌────────────────────────▼─────────────────────────────────────────┐
   │              dataset-versioning-service · data-asset-catalog     │
   └────────────────────────┬─────────────────────────────────────────┘
                            │
   ┌────────────────────────▼─────────────────────────────────────────┐
   │ Iceberg/Delta on MinIO/S3  ◀──  Spark / DataFusion               │
   └────────────────────────▲─────────────────────────────────────────┘
                            │
   ┌────────────────────────┴──────────────┐    ┌────────────────────┐
   │ ingestion-replication-service (batch) │    │ event-streaming-svc│
   │   + connector-management-service      │    │  (Redpanda/Kafka)  │
   └─────┬──────────┬────────┬─────────────┘    └─────────┬──────────┘
         │          │        │                            │
       NOAA       BTS    Synthetic                     OpenSky
       HRRR    On-Time     MRO                       (ADS-B live)

     [Transversal]  identity-federation · authorization-policy · audit-compliance
```

---

## 🛠️ Cómo levantar el stack (cuando llegue el momento)

### Local (laptop potente o Hetzner dedicado)
```bash
# Desde la raíz del repo
cp .env.example .env
# Editar .env: poner credenciales MinIO, Keycloak, etc.

# Stack base
docker compose -f compose.yaml up -d

# Si añadimos overlay específico de la PoC (a crear en infra/)
docker compose -f compose.yaml -f infra/docker-compose.poc-aviation.yml up -d
```

> Tarea pendiente al ejecutar la PoC: **crear `infra/docker-compose.poc-aviation.yml`** con SOLO los 15 servicios del subset, perfiles de recursos generosos y healthchecks. **No crear ahora** — es trabajo de implementación.

### Cloud (recomendado para la demo al cliente)
- **Despliegue:** Helm charts en `infra/` (a completar) sobre **K3s** o **EKS Managed**.
- **Tamaño mínimo:** ver [`04-infraestructura-y-despliegue.md`](04-infraestructura-y-despliegue.md).

---

## ⚙️ Configuración crítica por servicio

| Servicio | Variables de entorno clave | Notas |
|---|---|---|
| `event-streaming-service` | `KAFKA_BROKERS`, `OPENSKY_USER`, `OPENSKY_PASS` | Usar cuenta gratuita OpenSky; ratelimit 1 req/5s |
| `ingestion-replication-service` | `S3_ENDPOINT`, `S3_BUCKET`, `MAX_PARALLELISM=8` | Subir bandwidth para NOAA |
| `pipeline-build-service` | `SPARK_MASTER`, `EXECUTOR_MEMORY=8g`, `EXECUTORS=12` | Ajustar al hardware real |
| `ontology-query-service` | `OPENSEARCH_URL`, `CACHE_TTL=300` | Cache es clave para latencia p95 < 2s |
| `ai-application-generation-service` | `LLM_PROVIDER=ollama|azure`, `LLM_MODEL`, `EMBEDDING_MODEL` | Doble proveedor por si falla red |
| `audit-compliance-service` | `AUDIT_SINK=postgres+s3`, `IMMUTABLE_RETENTION=7y` | Mostrar al cliente la inmutabilidad |

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Auditar con `cargo build --workspace -p <cada-uno-del-subset>` que los 15 servicios compilan y arrancan.
2. Crear `infra/docker-compose.poc-aviation.yml` con SOLO esos 15 + dependencias.
3. Documentar puerto, healthcheck y dependencias de cada servicio en una tabla en ese YAML.
4. Configurar Prometheus para scrape de los 15 (los demás silenciados).
5. Probar arranque en frío end-to-end y medir tiempo (objetivo < 4 min).
