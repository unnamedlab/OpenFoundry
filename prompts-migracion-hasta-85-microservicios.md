# Prompts de migracion hasta 85 microservicios

## Objetivo

Este documento convierte la taxonomia de [microservicios-derivados-desde-foundry-docs.md](microservicios-derivados-desde-foundry-docs.md) en una secuencia de prompts ejecutables.

La meta es esta:

1. materializar los `85` bounded contexts documentales como servicios con owner claro
2. cerrar el ownership funcional de cada bounded context fuera de los `21` macroservicios legacy
3. terminar con la retirada completa de los `21` directorios legacy bajo `services/`

## Como usar este documento

1. Ejecuta los prompts en orden.
2. Si el directorio objetivo ya existe, el prompt debe cerrar ownership y mover runtime real; no basta con dejar scaffolding.
3. Si el directorio objetivo no existe, el prompt debe crearlo y mover la funcionalidad desde los servicios fuente.
4. No borres ningun servicio legacy hasta llegar a la seccion final de retirada.
5. Cada iteracion debe actualizar `gateway`, contratos, tests, docs y cualquier wiring del workspace afectado.

## Definicion de hecho global

Una minitarea solo cuenta como cerrada si cumple todo esto:

1. el servicio objetivo existe y compila
2. el servicio objetivo es el owner real de rutas, handlers, dominio, storage y eventos de su bounded context
3. `gateway` enruta al nuevo owner o al servicio consolidado correcto
4. el servicio legacy fuente pierde ownership real y queda reducido a compatibilidad temporal o deja de tocar ese dominio
5. hay validacion minima con tests, build o smoke checks del slice tocado
6. la documentacion y la matriz de migracion quedan actualizadas

## Nota sobre excepciones code-first

La taxonomia documental de `85` servicios no cubre bien tres dominios del codigo actual: entity resolution, geospatial intelligence y code repository review. Por eso este plan incluye una seccion corta de prompts extra para esos dominios. Esos prompts son necesarios para poder apagar los `21` legacy sin perder funcionalidad, aunque no formen parte del conteo estricto de `85` servicios documentales.

## Fase 1. Plataforma, seguridad y gobierno

### P01. Edge Gateway Service

Objetivo: `services/edge-gateway-service`
Fuentes: `services/gateway`

```text
Actua en OpenFoundry. Migra el bounded context "Edge Gateway Service" a `services/edge-gateway-service` usando `services/gateway` como fuente. Deja en el nuevo servicio solo routing, TLS termination, rate limiting, tenant resolution y access enforcement inicial; expulsa cualquier concern de negocio hacia sus owners. Actualiza el wiring del workspace, los proxies y la compatibilidad HTTP, pero no borres todavia `services/gateway`.
```

### P02. Identity Federation Service

Objetivo: `services/identity-federation-service`
Fuentes: `services/auth-service`

```text
Actua en OpenFoundry. Migra el bounded context "Identity Federation Service" a `services/identity-federation-service` usando `services/auth-service` como fuente principal. Mueve login, refresh, MFA, SAML, OIDC, OAuth identity flows y autenticacion de usuarios, cuentas de servicio y sesiones. Deja `auth-service` sin ownership real sobre federation y valida rutas, contratos y tests de autenticacion.
```

### P03. Authorization, Mandatory Controls and Policy Engine

Objetivo: `services/authorization-policy-service`
Fuentes: `services/auth-service`, `services/authorization-policy-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Authorization, Mandatory Controls and Policy Engine" en `services/authorization-policy-service`. Mueve desde `services/auth-service` todo lo relativo a roles, permissions, policies, ABAC, CBAC, markings, restricted views y enforcement de acceso. Deja el servicio objetivo como unico owner del motor de autorizacion y reduce el legacy a compatibilidad temporal.
```

### P04. Tenancy, Organizations, Spaces and Projects Service

Objetivo: `services/tenancy-organizations-service`
Fuentes: `services/gateway`, `services/auth-service`, `services/nexus-service`, `services/ontology-service`

```text
Actua en OpenFoundry. Crea `services/tenancy-organizations-service` y migra ahi tenancy, enrollments, organizations, spaces, projects y sus fronteras de comparticion. Usa como fuentes `gateway`, `auth-service`, `nexus-service` y `ontology-service`, identificando ownership real de tenant, space y project context. Deja contratos claros para resolucion de tenant y elimina cualquier acoplamiento transversal innecesario en los legacy.
```

### P05. Application and OAuth Integration Service

Objetivo: `services/oauth-integration-service`
Fuentes: `services/auth-service`, `services/data-connector`, `services/oauth-integration-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Application and OAuth Integration Service" en `services/oauth-integration-service`. Mueve registro y gobierno de third-party applications, OAuth clients entrantes, app credentials e integraciones externas desde `auth-service` y `data-connector`. Deja el servicio objetivo como owner de app registration y OAuth inbound integration.
```

### P06. Security Governance and Constraint Service

Objetivo: `services/security-governance-service`
Fuentes: `services/audit-service`, `services/auth-service`

```text
Actua en OpenFoundry. Crea `services/security-governance-service` y migra ahi project constraints, governance templates, security rules estructurales y validaciones de integridad entre politicas y recursos. Extrae la funcionalidad desde `audit-service` y `auth-service`, dejando responsabilidades claras con `authorization-policy-service`. Valida que el nuevo servicio sea el owner de constraints y no un simple wrapper.
```

### P07. Audit and Compliance Service

Objetivo: `services/audit-compliance-service`
Fuentes: `services/audit-service`

```text
Actua en OpenFoundry. Migra el bounded context "Audit and Compliance Service" a `services/audit-compliance-service` usando `services/audit-service` como fuente. Mueve immutable audit log, collector, event search, compliance reporting y trazabilidad de cambios administrativos. Deja fuera SDS, retention y deletion para sus servicios propios y prepara el retiro futuro de `audit-service`.
```

### P08. Approvals Service

Objetivo: `services/approvals-service`
Fuentes: `services/workflow-service`, `services/approvals-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Approvals Service" en `services/approvals-service`. Mueve approvals, reviewers, states, request tasks y trazabilidad de cambios sujetos a aprobacion fuera de `workflow-service`. Deja el servicio objetivo como owner real del human-in-the-loop approval flow.
```

### P09. Checkpoints and Purpose Justification Service

Objetivo: `services/checkpoints-purpose-service`
Fuentes: `services/audit-service`, `services/auth-service`, `services/ai-service`

```text
Actua en OpenFoundry. Crea `services/checkpoints-purpose-service` y migra ahi checkpoints, purpose justification, records auditables y configuracion de interacciones sensibles. Usa como fuentes `audit-service`, `auth-service` y cualquier flujo sensible de `ai-service` que requiera justificacion. Deja politicas, APIs y evidencias auditables centralizadas en el nuevo servicio.
```

### P10. Sensitive Data Discovery and Automated Remediation Service

Objetivo: `services/sds-service`
Fuentes: `services/audit-service`, `services/sds-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Sensitive Data Discovery and Automated Remediation Service" en `services/sds-service`. Mueve SDS scan jobs, match conditions, issue creation, markings y remediaciones automaticas fuera de `audit-service`. Deja el servicio objetivo como owner total de discovery y remediation de datos sensibles.
```

### P11. Cryptographic Obfuscation Service

Objetivo: `services/cipher-service`
Fuentes: `services/auth-service`, `services/cipher-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Cryptographic Obfuscation Service" en `services/cipher-service`. Mueve cipher, hash, sign, verify, encrypt y decrypt governados desde `auth-service`, junto con permisos, channels y licencias asociadas. Deja `cipher-service` como unico owner del dominio criptografico.
```

### P12. Retention Policy Service

Objetivo: `services/retention-policy-service`
Fuentes: `services/dataset-service`, `services/audit-service`

```text
Actua en OpenFoundry. Crea `services/retention-policy-service` y migra ahi las politicas clasicas de retencion sobre datasets y transacciones no lineage-aware. Usa `dataset-service` y `audit-service` como fuentes para reglas, tablas, jobs y APIs relacionadas con retention. Deja el ownership de retencion claramente fuera de los legacy.
```

### P13. Lineage-aware Deletion Service

Objetivo: `services/lineage-deletion-service`
Fuentes: `services/dataset-service`, `services/lineage-service`, `services/audit-service`

```text
Actua en OpenFoundry. Crea `services/lineage-deletion-service` y migra ahi las capacidades de borrado lineage-aware estilo data lifetime. Usa como fuentes `dataset-service`, `lineage-service` y los flujos GDPR erase de `audit-service`, dejando claro que el nuevo servicio calcula impacto aguas abajo y ejecuta deletions seguras. Valida integracion con lineage y trazabilidad audit.
```

### P14. Network Boundary Policy Service

Objetivo: `services/network-boundary-service`
Fuentes: `services/gateway`, `services/auth-service`, `services/data-connector`

```text
Actua en OpenFoundry. Crea `services/network-boundary-service` y migra ahi ingress, egress, private link, proxies y network boundary policies. Usa `gateway`, `auth-service` y `data-connector` para localizar ownership real de network policy y enforcement estructural. Deja el nuevo servicio como owner del dominio de fronteras de red.
```

### P15. Platform Experience, Scoped Sessions and Enablement Service

Objetivo: `services/session-governance-service`
Fuentes: `services/auth-service`, `services/session-governance-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Platform Experience, Scoped Sessions and Enablement Service" en `services/session-governance-service`. Mueve scoped sessions, guest sessions, branding, home pages, guided experiences y enablement flows fuera de `auth-service`. Deja el servicio objetivo como owner de session scoping y platform experience.
```

### P16. Federation, Connected Hubs and Product Exchange Service

Objetivo: `services/federation-product-exchange-service`
Fuentes: `services/nexus-service`, `services/marketplace-service`, `services/marketplace-catalog-service`, `services/product-distribution-service`

```text
Actua en OpenFoundry. Crea `services/federation-product-exchange-service` y concentra ahi connected hubs, remote stores, sharing entre enrollments, product exchange, installs y distribution contracts. Usa `nexus-service`, `marketplace-service`, `marketplace-catalog-service` y `product-distribution-service` como fuentes y define APIs claras entre federation y curation. Prepara el retiro futuro de `nexus-service` y `marketplace-service` como macroservicios.
```

### P17. Notification and Alerting Service

Objetivo: `services/notification-alerting-service`
Fuentes: `services/notification-service`

```text
Actua en OpenFoundry. Migra el bounded context "Notification and Alerting Service" a `services/notification-alerting-service` usando `services/notification-service` como fuente. Mueve send, history, preferences, WebSocket delivery, connectors a email y otros canales, alerting y throttling. Deja el nuevo servicio como owner completo del dominio de notificaciones.
```

## Fase 2. Integracion e ingenieria de datos

### P18. Connector Management Service

Objetivo: `services/connector-management-service`
Fuentes: `services/data-connector`, `services/connector-management-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Connector Management Service" en `services/connector-management-service`. Mueve connections, catalog, capabilities, credentials metadata y connection testing fuera de `data-connector`. Deja el servicio objetivo como owner del alta y gobierno de conectores.
```

### P19. Ingestion and Replication Service

Objetivo: `services/ingestion-replication-service`
Fuentes: `services/data-connector`, `services/ingestion-replication-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Ingestion and Replication Service" en `services/ingestion-replication-service`. Mueve sync jobs, batch loads, micro-batch, export flows, refresh policies, agents y scheduler desde `data-connector`. Deja el nuevo servicio como owner real de ingestion y replication.
```

### P20. Data Asset Catalog and Metadata Service

Objetivo: `services/data-asset-catalog-service`
Fuentes: `services/dataset-service`, `services/data-asset-catalog-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Data Asset Catalog and Metadata Service" en `services/data-asset-catalog-service`. Mueve catalog facets, metadata, asset registration, schemas y discovery surfaces desde `dataset-service`. Deja el servicio objetivo como unico owner del catalogo de activos de datos.
```

### P21. Dataset Versioning and Transaction Service

Objetivo: `services/dataset-versioning-service`
Fuentes: `services/dataset-service`, `services/dataset-versioning-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Dataset Versioning and Transaction Service" en `services/dataset-versioning-service`. Mueve snapshots, append, update, delete, branching, transactions, checkout, merge y promote desde `dataset-service`. Deja el servicio objetivo como owner total del lifecycle transaccional de datasets.
```

### P22. Pipeline Authoring and Compilation Service

Objetivo: `services/pipeline-authoring-service`
Fuentes: `services/pipeline-service`, `services/pipeline-authoring-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Pipeline Authoring and Compilation Service" en `services/pipeline-authoring-service`. Mueve definicion de pipelines, validacion, compilation, pruning y generacion de planes ejecutables fuera de `pipeline-service`. Deja el nuevo servicio como owner del authoring y compilation layer.
```

### P23. Pipeline Build Orchestration Service

Objetivo: `services/pipeline-build-service`
Fuentes: `services/pipeline-service`, `services/pipeline-build-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Pipeline Build Orchestration Service" en `services/pipeline-build-service`. Mueve DAG resolution, workers, retries, locks, staleness y build execution fuera de `pipeline-service`. Deja el servicio objetivo como owner de la orquestacion de builds.
```

### P24. Schedule Orchestration Service

Objetivo: `services/pipeline-schedule-service`
Fuentes: `services/pipeline-service`, `services/workflow-service`, `services/pipeline-schedule-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Schedule Orchestration Service" en `services/pipeline-schedule-service`. Mueve cron jobs, event triggers, windows, due runs y backfills fuera de `pipeline-service` y del scheduling residual de `workflow-service`. Deja el nuevo servicio como owner del scheduling compartido.
```

### P25. Streaming Service

Objetivo: `services/event-streaming-service`
Fuentes: `services/streaming-service`

```text
Actua en OpenFoundry. Migra el bounded context "Streaming Service" a `services/event-streaming-service` usando `services/streaming-service` como fuente. Mueve stream processing, push de eventos, replay, dead letters, windows, checkpoints y topologies, dejando el nuevo servicio como owner claro del runtime de streaming. No borres todavia `streaming-service`.
```

### P26. Virtual Table and External Table Orchestration Service

Objetivo: `services/virtual-table-service`
Fuentes: `services/data-connector`, `services/virtual-table-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Virtual Table and External Table Orchestration Service" en `services/virtual-table-service`. Mueve virtual table query, external table orchestration y bindings a sistemas externos fuera de `data-connector`. Deja el servicio objetivo como owner de tablas virtuales y external tables.
```

### P27. CDC Metadata and Resolution Service

Objetivo: `services/ingestion-replication-service/src/cdc_metadata`
Fuentes: `services/data-connector`, `services/event-streaming-service`, `services/pipeline-service`

```text
Actua en OpenFoundry. Crea `services/ingestion-replication-service/src/cdc_metadata` y migra ahi change data capture metadata, incremental sync semantics, resolution state y contratos CDC compartidos. Usa `data-connector`, `event-streaming-service` y `pipeline-service` como fuentes y separa este dominio de la logica general de ingestion o streaming. Deja el nuevo servicio como owner de CDC metadata y resolution.
```

### P28. SQL Warehousing Service

Objetivo: `services/sql-warehousing-service`
Fuentes: `services/query-service`, `services/pipeline-service`, `services/dataset-service`

```text
Actua en OpenFoundry. Crea `services/sql-warehousing-service` y migra ahi workflows de SQL warehousing, persistencia intermedia y transformaciones SQL a gran escala. Usa `query-service`, `pipeline-service` y `dataset-service` como fuentes para aislar este dominio del gateway SQL general. Define ownership claro de jobs, storage intermedio y contratos SQL warehouse.
```

### P29. Time-Series Data Service

Objetivo: `services/time-series-data-service`
Fuentes: `services/event-streaming-service`, `services/ontology-service`, `services/geospatial-service`

```text
Actua en OpenFoundry. Crea `services/time-series-data-service` y migra ahi modelado, almacenamiento y serving especializado para workloads time-series. Usa `event-streaming-service`, `ontology-service` y `geospatial-service` como fuentes para separar este dominio de analytics y ontology general. Deja APIs y storage del tiempo como ownership claro del nuevo servicio.
```

### P30. Data Quality and Expectations Service

Objetivo: `services/dataset-quality-service`
Fuentes: `services/dataset-service`, `services/dataset-quality-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Data Quality and Expectations Service" en `services/dataset-quality-service`. Mueve quality rules, profile, lint, expectations y checks operacionales fuera de `dataset-service`. Deja el servicio objetivo como owner del dominio de calidad de datos.
```

### P31. Data Lineage and Impact Analysis Service

Objetivo: `services/lineage-service`
Fuentes: `services/dataset-service`, `services/pipeline-service`, `services/lineage-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Data Lineage and Impact Analysis Service" en `services/lineage-service`. Mueve lineage reconstruction, impact analysis y provenance desde `dataset-service` y `pipeline-service`. Deja el servicio objetivo como owner unico del grafo de lineage y sus APIs.
```

## Fase 3. Ontologia operacional

### P32. Ontology Definition and Type System Service

Objetivo: `services/ontology-definition-service`
Fuentes: `services/ontology-service`, `services/ontology-definition-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Ontology Definition and Type System Service" en `services/ontology-definition-service`. Mueve object types, link types, action types, interfaces, shared properties y versionado de tipos fuera de `ontology-service`. Deja el servicio objetivo como owner del type system ontologico.
```

### P33. Object Database Service

Objetivo: `services/object-database-service`
Fuentes: `services/ontology-service`, `services/object-database-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Object Database Service" en `services/object-database-service`. Mueve storage materializado de objetos, links y vistas operacionales de baja latencia fuera de `ontology-service`. Deja el servicio objetivo como owner del object store operacional.
```

### P34. Ontology Query, Search and Semantic Retrieval Service

Objetivo: `services/ontology-query-service`
Fuentes: `services/ontology-service`, `services/ontology-query-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Ontology Query, Search and Semantic Retrieval Service" en `services/ontology-query-service`. Mueve search, graph traversals, filters, aggregates, object sets, KNN y semantic retrieval fuera de `ontology-service`. Deja el servicio objetivo como owner de query y search ontologico.
```

### P35. Actions and Operational Writeback Service

Objetivo: `services/ontology-actions-service`
Fuentes: `services/ontology-service`, `services/ontology-actions-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Actions and Operational Writeback Service" en `services/ontology-actions-service`. Mueve execute action, writeback, side effects, action log, batch mutations e inline edits fuera de `ontology-service`. Deja el servicio objetivo como owner de actions y operational writeback.
```

### P36. Object Data Funnel and Indexing Service

Objetivo: `services/ontology-funnel-service`
Fuentes: `services/ontology-service`, `services/ontology-funnel-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Object Data Funnel and Indexing Service" en `services/ontology-funnel-service`. Mueve source funneling, sync, indexing, storage insights y transformacion de datasets o streams en objetos fuera de `ontology-service`. Deja el servicio objetivo como owner del pipeline de ingestion hacia objetos.
```

### P37. Ontology Functions Runtime Service

Objetivo: `services/ontology-functions-service`
Fuentes: `services/ontology-service`, `services/ontology-functions-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Ontology Functions Runtime Service" en `services/ontology-functions-service`. Mueve functions, validate, simulate, runs, metrics y runtime tipado fuera de `ontology-service`. Deja el servicio objetivo como owner del runtime de funciones ontologicas.
```

### P38. Ontology Security and Permission Resolution Service

Objetivo: `services/ontology-security-service`
Fuentes: `services/ontology-service`, `services/auth-service`, `services/ontology-security-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Ontology Security and Permission Resolution Service" en `services/ontology-security-service`. Mueve memberships, resource access, permission resolution y reglas de acceso ontologicas fuera de `ontology-service`, integrando lo necesario con autorizacion central. Deja el servicio objetivo como owner del access resolution ontologico.
```

## Fase 4. Modelos y ML

### P39. Model Adapter and Packaging Service

Objetivo: `services/model-adapter-service`
Fuentes: `services/ml-service`

```text
Actua en OpenFoundry. Crea `services/model-adapter-service` y migra ahi packaging de artifacts, adapters, sidecars y contratos de inferencia para modelos internos o externos. Extrae la funcionalidad desde `ml-service` y deja al nuevo servicio como owner de adapter packaging y artifact shaping. Define interfaces claras con catalog, deployment y serving.
```

### P40. Model Catalog and Registry Service

Objetivo: `services/model-catalog-service`
Fuentes: `services/ml-service`, `services/model-catalog-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Model Catalog and Registry Service" en `services/model-catalog-service`. Mueve modelos, versiones, permisos, metadata y lineage de modelo fuera de `ml-service`. Deja el servicio objetivo como owner del inventario de modelos.
```

### P41. Model Experiment Tracking Service

Objetivo: `services/ml-experiments-service`
Fuentes: `services/ml-service`, `services/ml-experiments-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Model Experiment Tracking Service" en `services/ml-experiments-service`. Mueve experiments, runs, params, metrics y artifacts de entrenamiento fuera de `ml-service`. Deja el servicio objetivo como owner del tracking de experimentacion.
```

### P42. Model Lifecycle, Submissions and Objectives Service

Objetivo: `services/model-lifecycle-service`
Fuentes: `services/ml-service`

```text
Actua en OpenFoundry. Crea `services/model-lifecycle-service` y migra ahi submissions, checks, reviews, releases, promotion flow y objetivos de modelado. Extrae el ownership desde `ml-service` y separa este control plane del registry, del deployment y del serving. Deja el nuevo servicio como owner del lifecycle de modelos.
```

### P43. Model Deployment Control Plane Service

Objetivo: `services/model-deployment-service`
Fuentes: `services/ml-service`, `services/model-deployment-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Model Deployment Control Plane Service" en `services/model-deployment-service`. Mueve deployments, autoscaling policies, rollout, version pinning y deployment control plane fuera de `ml-service`. Deja el servicio objetivo como owner del plano de control de despliegue de modelos.
```

### P44. Model Serving and Inference Runtime Service

Objetivo: `services/model-serving-service`
Fuentes: `services/ml-service`, `services/model-serving-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Model Serving and Inference Runtime Service" en `services/model-serving-service`. Mueve realtime predict, batch inferencia, contracts estables y runtime de serving fuera de `ml-service`. Deja el servicio objetivo como owner del runtime de inferencia.
```

### P45. Model Evaluation and Metric Pipelines Service

Objetivo: `services/model-evaluation-service`
Fuentes: `services/ml-service`, `services/model-evaluation-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Model Evaluation and Metric Pipelines Service" en `services/model-evaluation-service`. Mueve fairness, robustness, evaluators, subsets y metric pipelines fuera de `ml-service`. Deja el servicio objetivo como owner del dominio de evaluacion de modelos.
```

### P46. Model Inference History and Feedback Ledger Service

Objetivo: `services/model-inference-history-service`
Fuentes: `services/ml-service`, `services/model-inference-history-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Model Inference History and Feedback Ledger Service" en `services/model-inference-history-service`. Mueve inference history, output ledger, errors y feedback loops fuera de `ml-service`. Deja el servicio objetivo como owner de la historia operacional de inferencia.
```

## Fase 5. AIP, LLMs y AI operativa

### P47. LLM Catalog and Discovery Service

Objetivo: `services/llm-catalog-service`
Fuentes: `services/ai-service`, `services/llm-catalog-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "LLM Catalog and Discovery Service" en `services/llm-catalog-service`. Mueve providers, model catalog, capability metadata y discovery de modelos LLM fuera de `ai-service`. Deja el servicio objetivo como owner del catalogo LLM.
```

### P48. AIP Logic and Prompt Workflow Service

Objetivo: `services/prompt-workflow-service`
Fuentes: `services/ai-service`, `services/prompt-workflow-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "AIP Logic and Prompt Workflow Service" en `services/prompt-workflow-service`. Mueve prompt templates, render, blocks, variables y workflow logic LLM fuera de `ai-service`. Deja el servicio objetivo como owner del prompt orchestration layer.
```

### P49. Knowledge Source Registration and Indexing Service

Objetivo: `services/knowledge-index-service`
Fuentes: `services/ai-service`, `services/knowledge-index-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Knowledge Source Registration and Indexing Service" en `services/knowledge-index-service`. Mueve knowledge bases, document registration, indexing y source governance fuera de `ai-service`. Deja el servicio objetivo como owner de la base documental y su indexacion.
```

### P50. Retrieval and Knowledge Context Service

Objetivo: `services/retrieval-context-service`
Fuentes: `services/ai-service`, `services/retrieval-context-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Retrieval and Knowledge Context Service" en `services/retrieval-context-service`. Mueve retrieval, grounding, context assembly y knowledge search para prompts fuera de `ai-service`. Deja el servicio objetivo como owner del retrieval layer.
```

### P51. Conversation Session and Thread State Service

Objetivo: `services/conversation-state-service`
Fuentes: `services/ai-service`, `services/conversation-state-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Conversation Session and Thread State Service" en `services/conversation-state-service`. Mueve session state, thread state, continuity y persistence conversacional fuera de `ai-service`. Deja el servicio objetivo como owner del estado de conversacion.
```

### P52. Tool Registry and Execution Service

Objetivo: `services/tool-registry-service`
Fuentes: `services/ai-service`, `services/tool-registry-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Tool Registry and Execution Service" en `services/tool-registry-service`. Mueve tool catalog, execution metadata, command modes y tool contracts fuera de `ai-service`. Deja el servicio objetivo como owner del registry y del dispatch de herramientas.
```

### P53. Agent Runtime and Chatbot Orchestration Service

Objetivo: `services/agent-runtime-service`
Fuentes: `services/ai-service`, `services/tool-registry-service`, `services/conversation-state-service`

```text
Actua en OpenFoundry. Crea `services/agent-runtime-service` y migra ahi agent runtime, chatbot orchestration, tool call planning, humano en el loop y coordinacion de pasos conversacionales. Extrae la logica desde `ai-service` y apoya el nuevo runtime en `tool-registry-service` y `conversation-state-service`. Deja `ai-service` sin ownership real sobre agents.
```

### P54. AI Evaluation and Regression Service

Objetivo: `services/ai-evaluation-service`
Fuentes: `services/ai-service`, `services/ai-evaluation-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "AI Evaluation and Regression Service" en `services/ai-evaluation-service`. Mueve guardrails evaluate, benchmarks, regression suites y metricas de prompts o agents fuera de `ai-service`. Deja el servicio objetivo como owner del dominio de evaluacion AI.
```

### P55. Document Intelligence Service

Objetivo: `services/document-intelligence-service`
Fuentes: `services/ai-service`, `services/notebook-service`

```text
Actua en OpenFoundry. Crea `services/document-intelligence-service` y migra ahi OCR, layout-aware extraction, document parsing y entity extraction asistida por VLMs. Usa como fuentes `ai-service` y cualquier flujo documental de `notebook-service` que hoy mezcle este dominio. Deja el nuevo servicio como owner del procesamiento inteligente de documentos.
```

### P56. AI Application Generation Service

Objetivo: `services/ai-application-generation-service`
Fuentes: `services/ai-service`, `services/app-builder-service`, `services/application-curation-service`

```text
Actua en OpenFoundry. Crea `services/ai-application-generation-service` y migra ahi la generacion guiada de aplicaciones a partir de lenguaje natural. Extrae la orquestacion desde `ai-service` y `app-builder-service`, integrando composition, curation y seed workflows sin dejar ownership ambiguo. Deja el nuevo servicio como owner del runtime de AI app generation.
```

## Fase 6. Analitica, workflows y aplicaciones operacionales

### P57. Tabular Analysis Service

Objetivo: `services/tabular-analysis-service`
Fuentes: `services/query-service`, `services/dataset-service`, `services/report-service`

```text
Actua en OpenFoundry. Crea `services/tabular-analysis-service` y migra ahi analisis tabular persistente a gran escala. Usa `query-service`, `dataset-service` y `report-service` como fuentes para separar este dominio del SQL gateway y del reporting documental. Deja el nuevo servicio como owner del tabular analysis runtime.
```

### P58. Ontology Exploratory Analysis Service

Objetivo: `services/ontology-exploratory-analysis-service`
Fuentes: `services/ontology-service`, `services/app-builder-service`, `services/geospatial-service`

```text
Actua en OpenFoundry. Crea `services/ontology-exploratory-analysis-service` y migra ahi exploracion visual sobre ontology, object sets, links, maps y writeback asistido. Usa `ontology-service`, `app-builder-service` y `geospatial-service` como fuentes para aislar este dominio de analitica operacional. Deja el nuevo servicio como owner de la exploracion ontologica.
```

### P59. Ontology and Time-Series Analytics Service

Objetivo: `services/ontology-timeseries-analytics-service`
Fuentes: `services/ontology-service`, `services/time-series-data-service`, `services/geospatial-service`

```text
Actua en OpenFoundry. Crea `services/ontology-timeseries-analytics-service` y migra ahi dashboards, analitica sobre ontology y workloads combinados con series temporales. Usa `ontology-service`, `time-series-data-service` y `geospatial-service` como fuentes, dejando separado este dominio de la exploracion visual y del query gateway SQL. Deja ownership claro de analytics ontologico-temporal.
```

### P60. SQL and BI Gateway Service

Objetivo: `services/sql-bi-gateway-service`
Fuentes: `services/query-service`

```text
Actua en OpenFoundry. Migra el bounded context "SQL and BI Gateway Service" a `services/sql-bi-gateway-service` usando `services/query-service` como fuente. Mueve execute, explain, saved queries, SQL connectivity y compatibilidad con herramientas BI, dejando el nuevo servicio como owner completo del gateway SQL. No borres todavia `query-service`.
```

### P61. Interactive Code Analysis and Notebook Runtime Service

Objetivo: `services/notebook-runtime-service`
Fuentes: `services/notebook-service`

```text
Actua en OpenFoundry. Migra el bounded context "Interactive Code Analysis and Notebook Runtime Service" a `services/notebook-runtime-service` usando `services/notebook-service` como fuente. Mueve notebooks, cells, execute, kernels, sessions y colaboracion interactiva, dejando fuera reporting documental y workspace orchestration. Deja el nuevo servicio como owner del notebook runtime.
```

### P62. Document Reporting Service

Objetivo: `services/document-reporting-service`
Fuentes: `services/report-service`, `services/notebook-service`, `services/document-reporting-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Document Reporting Service" en `services/document-reporting-service`. Mueve notepad-style documents, report definitions, generation, execution history, downloads y export narrativo desde `report-service` y `notebook-service`. Deja el servicio objetivo como owner del reporting documental.
```

### P63. Spreadsheet Computation Service

Objetivo: `services/spreadsheet-computation-service`
Fuentes: `services/app-builder-service`, `services/notebook-service`, `services/dataset-service`

```text
Actua en OpenFoundry. Crea `services/spreadsheet-computation-service` y migra ahi hojas de calculo colaborativas, formulas, recalculo y writeback tipo spreadsheet. Usa `app-builder-service`, `notebook-service` y `dataset-service` como fuentes si hoy existen piezas dispersas; no intentes meter aqui `fusion-service`, porque ese dominio es entity resolution. Deja el nuevo servicio como owner del spreadsheet runtime.
```

### P64. Analytical Reusable Logic Service

Objetivo: `services/analytical-logic-service`
Fuentes: `services/query-service`, `services/report-service`, `services/notebook-service`

```text
Actua en OpenFoundry. Crea `services/analytical-logic-service` y migra ahi saved expressions, reusable logic, visual functions y plantillas analiticas compartidas. Usa `query-service`, `report-service` y `notebook-service` como fuentes para separar este dominio de los runtimes que lo consumen. Deja el nuevo servicio como owner de reusable analytical logic.
```

### P65. Workflow Automation Service

Objetivo: `services/workflow-automation-service`
Fuentes: `services/workflow-service`

```text
Actua en OpenFoundry. Crea `services/workflow-automation-service` y migra ahi definicion, ejecucion y reglas continuas o programadas de automatizaciones de negocio. Usa `workflow-service` como fuente y deja approvals, operational control plane y trace fuera del nuevo servicio. Deja ownership claro del workflow runtime de automatizacion.
```

### P66. Automation Operations Control Plane Service

Objetivo: `services/automation-operations-service`
Fuentes: `services/workflow-service`

```text
Actua en OpenFoundry. Crea `services/automation-operations-service` y migra ahi colas, estados operativos, liveness, retries, dependencia entre runs y ejecucion por objeto de automatizaciones. Extrae este control plane desde `workflow-service` y separalo del runtime de automatizacion. Deja el nuevo servicio como owner del control plane operacional.
```

### P67. Workflow Lineage and Trace Service

Objetivo: `services/workflow-trace-service`
Fuentes: `services/workflow-service`, `services/lineage-service`

```text
Actua en OpenFoundry. Crea `services/workflow-trace-service` y migra ahi run history, traces, logs y provenance de workflows a traves de funciones, acciones, modelos y apps. Usa `workflow-service` y `lineage-service` como fuentes, manteniendo la frontera clara entre lineage de datos y lineage de workflow. Deja el nuevo servicio como owner de trace y history de workflows.
```

### P68. Application Composition Service

Objetivo: `services/application-composition-service`
Fuentes: `services/app-builder-service`

```text
Actua en OpenFoundry. Crea `services/application-composition-service` y migra ahi composition backend, views, state, bindings, page layout y event orchestration de aplicaciones tipo Workshop o Slate. Usa `app-builder-service` como fuente y deja curation, widgets y developer control plane fuera del nuevo servicio. Deja ownership claro del composition runtime.
```

### P69. Workspace and Application Curation Service

Objetivo: `services/application-curation-service`
Fuentes: `services/app-builder-service`, `services/marketplace-service`, `services/application-curation-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Workspace and Application Curation Service" en `services/application-curation-service`. Mueve workspaces, modules, home pages, promoted apps y portales curados desde `app-builder-service` y `marketplace-service`. Deja el servicio objetivo como owner de curation y workspace surfacing.
```

### P70. Custom Widget Registry and Host Bridge Service

Objetivo: `services/widget-registry-service`
Fuentes: `services/app-builder-service`, `services/widget-registry-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Custom Widget Registry and Host Bridge Service" en `services/widget-registry-service`. Mueve widget sets, widget catalog, parameter contracts, event bridge e integracion con el host fuera de `app-builder-service`. Deja el servicio objetivo como owner del registry y host bridge de widgets.
```

### P71. Scenario Simulation Service

Objetivo: `services/scenario-simulation-service`
Fuentes: `services/ontology-service`, `services/ontology-actions-service`, `services/model-serving-service`

```text
Actua en OpenFoundry. Crea `services/scenario-simulation-service` y migra ahi what-if branches, immutable forks, scenario runs y simulacion apoyada en acciones y modelos. Usa `ontology-service`, `ontology-actions-service` y `model-serving-service` como fuentes. Deja el nuevo servicio como owner del dominio de scenario simulation.
```

### P72. Solution Design and Architecture Knowledge Service

Objetivo: `services/solution-design-service`
Fuentes: `docs/`, `services/app-builder-service`, `services/code-repo-service`

```text
Actua en OpenFoundry. Crea `services/solution-design-service` y migra ahi diagramas, patrones, architecture knowledge y referencias enlazadas a recursos de plataforma. Usa `docs/`, `app-builder-service` y `code-repo-service` como fuentes si hoy ese conocimiento esta disperso. Deja el nuevo servicio como owner del knowledge base de arquitectura y solucion.
```

## Fase 7. Developer platform, extensibilidad y observabilidad

### P73. Developer Console and Application Control Plane Service

Objetivo: `services/developer-console-service`
Fuentes: `services/app-builder-service`, `services/marketplace-service`

```text
Actua en OpenFoundry. Crea `services/developer-console-service` y migra ahi application admin, scopes, subdomains, releases y configuracion de apps custom. Usa `app-builder-service` y `marketplace-service` como fuentes, separando este control plane del composition runtime y de la curation. Deja el nuevo servicio como owner del developer console.
```

### P74. SDK Generation and Publication Service

Objetivo: `services/sdk-generation-service`
Fuentes: `services/ontology-definition-service`, `sdks/`, `proto/`

```text
Actua en OpenFoundry. Crea `services/sdk-generation-service` y migra ahi la generacion, publicacion y versionado de SDKs y contratos OpenAPI a partir de la ontologia y los contratos del repo. Usa `ontology-definition-service`, `sdks/` y `proto/` como fuentes. Deja el nuevo servicio como owner de SDK generation y publication.
```

### P75. Managed Workspace Orchestration Service

Objetivo: `services/managed-workspace-service`
Fuentes: `services/notebook-service`, `services/app-builder-service`

```text
Actua en OpenFoundry. Crea `services/managed-workspace-service` y migra ahi provisioning de entornos de desarrollo, profiles, dataset aliases, branches de builder y context resolution de workspaces. Usa `notebook-service` y `app-builder-service` como fuentes y separa este dominio del notebook runtime. Deja el nuevo servicio como owner de managed workspace orchestration.
```

### P76. Custom Endpoints Publishing and Gateway Service

Objetivo: `services/custom-endpoints-service`
Fuentes: `services/gateway`, `services/app-builder-service`

```text
Actua en OpenFoundry. Crea `services/custom-endpoints-service` y migra ahi endpoint set publishing, versionado de endpoints y remapeo HTTP hacia actions, functions u otros backends. Usa `gateway` y `app-builder-service` como fuentes y separa esta capacidad del edge gateway general. Deja el nuevo servicio como owner del publishing de custom endpoints.
```

### P77. MCP Orchestration and Exposure Service

Objetivo: `services/mcp-orchestration-service`
Fuentes: `services/ai-service`, `services/tool-registry-service`, `services/gateway`

```text
Actua en OpenFoundry. Crea `services/mcp-orchestration-service` y migra ahi exposicion de herramientas MCP internas y ontologicas para agentes, apps y consumidores externos. Usa `ai-service`, `tool-registry-service` y `gateway` como fuentes, dejando claro el boundary entre tool registry y MCP exposure. Deja el nuevo servicio como owner del dominio MCP.
```

### P78. Global Branch Orchestration Service

Objetivo: `services/global-branch-service`
Fuentes: `services/code-repo-service`, `services/global-branch-service`, `services/pipeline-service`, `services/ontology-service`, `services/app-builder-service`

```text
Actua en OpenFoundry. Cierra la migracion del bounded context "Global Branch Orchestration Service" en `services/global-branch-service`. Mueve branching transversal sobre repos, pipelines, ontologia, acciones y aplicaciones fuera de `code-repo-service` y de cualquier servicio legacy que aun lo controle. Deja el servicio objetivo como owner del branch orchestration cross-domain.
```

### P79. Compute Modules Control Plane Service

Objetivo: `services/compute-modules-control-plane-service`
Fuentes: `services/ml-service`, `services/pipeline-service`, `services/notebook-service`

```text
Actua en OpenFoundry. Crea `services/compute-modules-control-plane-service` y migra ahi lifecycle, deployment, replicas, diagnostics y configuracion de compute modules. Usa `ml-service`, `pipeline-service` y `notebook-service` como fuentes para separar este plano de control de los runtimes que lo consumen. Deja el nuevo servicio como owner del control plane de compute modules.
```

### P80. Compute Modules Runtime Service

Objetivo: `services/compute-modules-runtime-service`
Fuentes: `services/ml-service`, `services/pipeline-service`, `services/notebook-service`

```text
Actua en OpenFoundry. Crea `services/compute-modules-runtime-service` y migra ahi la ejecucion de compute modules arbitrarios bajo identidad de plataforma, con escalado y metricas integradas. Usa `ml-service`, `pipeline-service` y `notebook-service` como fuentes y deja separado el runtime del control plane. Deja el nuevo servicio como owner del compute runtime.
```

### P81. Monitoring Rules and Scope Engine Service

Objetivo: `services/monitoring-rules-service`
Fuentes: `services/notification-service`, `services/audit-service`, `services/workflow-service`

```text
Actua en OpenFoundry. Crea `services/monitoring-rules-service` y migra ahi monitores, scopes, severidades, subscribers y reglas de observabilidad a escala. Usa `notification-service`, `audit-service` y `workflow-service` como fuentes y separa este dominio del delivery de alertas. Deja el nuevo servicio como owner del monitoring rules engine.
```

### P82. Health Check Evaluation Service

Objetivo: `services/health-check-service`
Fuentes: `services/monitoring-rules-service`, `services/workflow-service`, `services/pipeline-service`

```text
Actua en OpenFoundry. Crea `services/health-check-service` y migra ahi checks de salud, semantics de evaluacion, scheduling propio y resultados de health para recursos y workflows. Usa `monitoring-rules-service`, `workflow-service` y `pipeline-service` como fuentes. Deja el nuevo servicio como owner del dominio de health checks.
```

### P83. Execution Observability Service

Objetivo: `services/execution-observability-service`
Fuentes: `services/pipeline-service`, `services/workflow-service`, `services/ml-service`, `services/ai-service`, `services/audit-service`

```text
Actua en OpenFoundry. Crea `services/execution-observability-service` y migra ahi run history, log search, distributed tracing y debug de ejecuciones. Usa `pipeline-service`, `workflow-service`, `ml-service`, `ai-service` y `audit-service` como fuentes, dejando fuera governance general de telemetria. Deja el nuevo servicio como owner de la observabilidad de ejecucion.
```

### P84. Telemetry Governance and Export Service

Objetivo: `services/telemetry-governance-service`
Fuentes: `services/audit-service`, `services/notification-service`, `services/execution-observability-service`

```text
Actua en OpenFoundry. Crea `services/telemetry-governance-service` y migra ahi permisos de telemetria, export de logs, metricas y eventos a sistemas externos, junto con las politicas de gobierno asociadas. Usa `audit-service`, `notification-service` y `execution-observability-service` como fuentes. Deja el nuevo servicio como owner del governance y export de telemetria.
```

### P85. Code Security Scanning Service

Objetivo: `services/code-security-scanning-service`
Fuentes: `services/code-repo-service`, `.github/`, `tools/`

```text
Actua en OpenFoundry. Crea `services/code-security-scanning-service` y migra ahi analisis estatico de seguridad, code smells y enforcement de politicas de calidad integrado en CI. Usa `code-repo-service`, `.github/` y `tools/` como fuentes para separar este dominio del repo hosting general. Deja el nuevo servicio como owner del scanning de seguridad de codigo.
```

## Fase 8. Excepciones code-first necesarias para apagar legacy

### X01. Entity Resolution and Golden Record Service

Objetivo: `services/entity-resolution-service`
Fuentes: `services/fusion-service`, `services/entity-resolution-service`

```text
Actua en OpenFoundry. Cierra la migracion del dominio code-first "Entity Resolution and Golden Record Service" en `services/entity-resolution-service`. Mueve matching rules, merge strategies, deduplication jobs, clusters, review queue y golden records fuera de `fusion-service`. Deja el servicio objetivo como owner real del dominio antes de retirar el legacy.
```

### X02. Geospatial Intelligence Service

Objetivo: `services/geospatial-intelligence-service`
Fuentes: `services/geospatial-service`, `services/geospatial-intelligence-service`

```text
Actua en OpenFoundry. Cierra la migracion del dominio code-first "Geospatial Intelligence Service" en `services/geospatial-intelligence-service`. Mueve layers, geospatial query, clustering, routing, geocode, reverse geocode y vector tiles fuera de `geospatial-service`. Deja el servicio objetivo como owner real del dominio geoespacial antes de retirar el legacy.
```

### X03. Code Repository and Review Service

Objetivo: `services/code-repository-review-service`
Fuentes: `services/code-repo-service`, `services/global-branch-service`

```text
Actua en OpenFoundry. Crea `services/code-repository-review-service` y migra ahi repos, commits, files, diff, search, CI, merge requests, comments e integraciones Git. Deja en `global-branch-service` solo el branching transversal y saca el runtime de hosting y review fuera de `code-repo-service`. Este paso es obligatorio para retirar el legacy sin perder el dominio Git.
```

## Fase 9. Retirada de los 21 macroservicios legacy

### R01. Retirar `services/gateway`

```text
Actua en OpenFoundry. Retira `services/gateway` solo si `services/edge-gateway-service` y `services/custom-endpoints-service` ya son owners de todas sus rutas y concerns residuales. Verifica que no queden handlers, tablas, jobs ni wiring exclusivos en el legacy, actualiza el workspace, elimina el directorio y valida build, rutas y docs despues del borrado.
```

### R02. Retirar `services/auth-service`

```text
Actua en OpenFoundry. Retira `services/auth-service` solo si `identity-federation-service`, `authorization-policy-service`, `oauth-integration-service`, `cipher-service`, `network-boundary-service` y `session-governance-service` ya son owners reales de sus dominios. Corta cualquier facade residual, actualiza contratos y elimina el directorio legacy con validacion completa.
```

### R03. Retirar `services/audit-service`

```text
Actua en OpenFoundry. Retira `services/audit-service` solo si `audit-compliance-service`, `sds-service`, `security-governance-service`, `retention-policy-service`, `lineage-deletion-service` y `telemetry-governance-service` ya absorbieron todo su ownership. Elimina wiring residual, migra cualquier storage pendiente y borra el directorio legacy con validacion posterior.
```

### R04. Retirar `services/data-connector`

```text
Actua en OpenFoundry. Retira `services/data-connector` solo si `connector-management-service`, `ingestion-replication-service`, `virtual-table-service`, `ingestion-replication-service CDC metadata module` y `oauth-integration-service` ya son owners reales. Elimina cualquier query, agent, scheduler o credential flow residual y borra el legacy tras validar el slice de conectividad.
```

### R05. Retirar `services/dataset-service`

```text
Actua en OpenFoundry. Retira `services/dataset-service` solo si `data-asset-catalog-service`, `dataset-versioning-service`, `dataset-quality-service`, `lineage-service`, `retention-policy-service` y `lineage-deletion-service` ya absorbieron todo el dominio. Borra el legacy solo cuando CRUD, branches, exports y views ya no dependan de el.
```

### R06. Retirar `services/streaming-service`

```text
Actua en OpenFoundry. Retira `services/streaming-service` solo si `event-streaming-service`, `ingestion-replication-service CDC metadata module` y `time-series-data-service` ya cubren todo su ownership residual. Elimina rutas y runtime legacy, actualiza observabilidad y borra el directorio tras validar replay, windows y topologies.
```

### R07. Retirar `services/query-service`

```text
Actua en OpenFoundry. Retira `services/query-service` solo si `sql-bi-gateway-service`, `sql-warehousing-service`, `tabular-analysis-service` y `analytical-logic-service` ya son owners reales de SQL, BI y reusable query logic. Borra el legacy solo cuando no queden execute, explain ni saved queries bajo su ownership.
```

### R08. Retirar `services/pipeline-service`

```text
Actua en OpenFoundry. Retira `services/pipeline-service` solo si `pipeline-authoring-service`, `pipeline-build-service`, `pipeline-schedule-service`, `lineage-service`, `compute-modules-control-plane-service` y `compute-modules-runtime-service` ya absorbieron el dominio. Elimina workers, retries y triggers residuales antes del borrado.
```

### R09. Retirar `services/ontology-service`

```text
Actua en OpenFoundry. Retira `services/ontology-service` solo si `ontology-definition-service`, `object-database-service`, `ontology-query-service`, `ontology-actions-service`, `ontology-funnel-service`, `ontology-functions-service`, `ontology-security-service` y `scenario-simulation-service` ya son owners reales. Borra el legacy solo cuando no queden rutas, jobs ni tablas ontologicas exclusivas.
```

### R10. Retirar `services/fusion-service`

```text
Actua en OpenFoundry. Retira `services/fusion-service` solo si `entity-resolution-service` ya absorbio matching, merge, clusters, review y golden records. Asegura que `spreadsheet-computation-service` siga siendo un dominio separado y que no exista confusion funcional antes del borrado.
```

### R11. Retirar `services/ml-service`

```text
Actua en OpenFoundry. Retira `services/ml-service` solo si `model-adapter-service`, `model-catalog-service`, `ml-experiments-service`, `model-lifecycle-service`, `model-deployment-service`, `model-serving-service`, `model-evaluation-service` y `model-inference-history-service` ya son owners reales. Elimina cualquier feature, training o overview residual antes del borrado.
```

### R12. Retirar `services/ai-service`

```text
Actua en OpenFoundry. Retira `services/ai-service` solo si `llm-catalog-service`, `prompt-workflow-service`, `knowledge-index-service`, `retrieval-context-service`, `conversation-state-service`, `tool-registry-service`, `agent-runtime-service`, `ai-evaluation-service`, `document-intelligence-service`, `ai-application-generation-service` y `mcp-orchestration-service` ya absorben todo el dominio. Borra el legacy solo cuando no queden chat, providers ni agent flows bajo su ownership.
```

### R13. Retirar `services/workflow-service`

```text
Actua en OpenFoundry. Retira `services/workflow-service` solo si `approvals-service`, `workflow-automation-service`, `automation-operations-service`, `workflow-trace-service` y `pipeline-schedule-service` ya cubren todo su ownership. Elimina cualquier trigger, manual run o queue residual antes de borrar el directorio.
```

### R14. Retirar `services/notebook-service`

```text
Actua en OpenFoundry. Retira `services/notebook-service` solo si `notebook-runtime-service`, `document-reporting-service`, `managed-workspace-service`, `spreadsheet-computation-service` y `document-intelligence-service` ya absorbieron todo el dominio. Borra el legacy solo cuando notebooks, sessions, workspaces y notepad ya no dependan de el.
```

### R15. Retirar `services/app-builder-service`

```text
Actua en OpenFoundry. Retira `services/app-builder-service` solo si `application-composition-service`, `application-curation-service`, `widget-registry-service`, `developer-console-service`, `custom-endpoints-service`, `ai-application-generation-service` y `managed-workspace-service` ya son owners reales. Elimina preview, pages, publish y templating residual antes del borrado.
```

### R16. Retirar `services/report-service`

```text
Actua en OpenFoundry. Retira `services/report-service` solo si `document-reporting-service`, `notification-alerting-service`, `tabular-analysis-service` y `analytical-logic-service` ya absorbieron reporting, schedules, downloads y delivery. Borra el legacy solo cuando no queden generators ni distribution flows bajo su ownership.
```

### R17. Retirar `services/code-repo-service`

```text
Actua en OpenFoundry. Retira `services/code-repo-service` solo si `code-repository-review-service`, `global-branch-service`, `sdk-generation-service` y `code-security-scanning-service` ya absorben hosting Git, review, branching transversal y CI security concerns. Elimina cualquier ruta o storage exclusivo antes del borrado.
```

### R18. Retirar `services/marketplace-service`

```text
Actua en OpenFoundry. Retira `services/marketplace-service` solo si `federation-product-exchange-service`, `application-curation-service`, `developer-console-service` y cualquier catalogo o rollout asociado ya son owners reales. Borra el legacy solo cuando installs, reviews, listings, fleets y promotion gates ya no dependan de el.
```

### R19. Retirar `services/nexus-service`

```text
Actua en OpenFoundry. Retira `services/nexus-service` solo si `tenancy-organizations-service`, `federation-product-exchange-service` y `telemetry-governance-service` ya absorbieron spaces, contracts, shares, federation, replication y audit bridge. Elimina todo ownership residual y borra el legacy con validacion de sharing y contracts.
```

### R20. Retirar `services/geospatial-service`

```text
Actua en OpenFoundry. Retira `services/geospatial-service` solo si `geospatial-intelligence-service`, `time-series-data-service`, `ontology-exploratory-analysis-service` y `ontology-timeseries-analytics-service` ya absorben todo el dominio geoespacial. Borra el legacy solo cuando layers, features, geocode y tiles ya no dependan de el.
```

### R21. Retirar `services/notification-service`

```text
Actua en OpenFoundry. Retira `services/notification-service` solo si `notification-alerting-service`, `monitoring-rules-service`, `health-check-service` y `telemetry-governance-service` ya absorben delivery, alerting y observabilidad relacionada. Elimina wiring residual y borra el legacy con validacion de send, preferences y WebSocket channels.
```

## Cierre esperado del programa

El programa se considera completado cuando se cumplan estas tres condiciones:

1. los `85` bounded contexts documentales tienen owner claro y servicio objetivo operativo
2. las tres excepciones code-first tienen owner claro fuera de los legacy
3. los `21` macroservicios legacy han sido eliminados de `services/`