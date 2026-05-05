# Guia de migracion desde `services/` hacia los microservicios objetivo

## Objetivo

Este documento actualiza la guia original para reflejar el estado real de `services/` a fecha `2026-04-27`.

Ya no parte de una foto teorica donde solo existen 21 macroservicios. Parte de un estado transicional donde conviven servicios legacy y extracciones ya materializadas.

La pregunta que responde ahora es esta:

**dado el arbol real de `services/`, que ownership conserva cada macroservicio legacy, que bounded contexts ya tienen servicio dedicado, y que queda pendiente para cerrar la migracion hacia la taxonomia objetivo.**

## Metodo usado

Esta version combina cuatro fuentes de evidencia:

- la taxonomia objetivo definida en `microservicios-derivados-desde-foundry-docs.md`
- la lectura original del codigo de los 21 macroservicios legacy que sostiene el analisis detallado mas abajo
- el inventario actual de `services/`, que hoy contiene `65` directorios de servicio con `Cargo.toml`
- una medida aproximada de peso por servicio usando numero de ficheros `.rs`, `.sql` y `.toml`, util solo como senal de transicion, no como medida de complejidad absoluta

## Foto actual de `services/`

Hoy `services/` contiene **65 servicios Rust reales** con `Cargo.toml`.

La foto vigente es esta:

- **21 servicios legacy** que siguen presentes como macrodominios historicos: `gateway`, `auth-service`, `audit-service`, `data-connector`, `dataset-service`, `streaming-service`, `query-service`, `pipeline-service`, `ontology-service`, `fusion-service`, `ml-service`, `ai-service`, `workflow-service`, `notebook-service`, `app-builder-service`, `report-service`, `code-repo-service`, `marketplace-service`, `nexus-service`, `geospatial-service`, `notification-service`
- **46 servicios ya extraidos o materializados** en el arbol actual: `approvals-service`, `authorization-policy-service`, `cipher-service`, `oauth-integration-service`, `session-governance-service`, `sds-service`, `connector-management-service`, `ingestion-replication-service`, `virtual-table-service`, `data-asset-catalog-service`, `dataset-versioning-service`, `dataset-quality-service`, `sql-warehousing-service`, `time-series-data-service`, `lineage-service`, `pipeline-authoring-service`, `pipeline-build-service`, `pipeline-schedule-service`, `object-database-service`, `ontology-actions-service`, `ontology-definition-service`, `ontology-functions-service`, `ontology-funnel-service`, `ontology-query-service`, `ontology-security-service`, `ml-experiments-service`, `model-catalog-service`, `model-deployment-service`, `model-evaluation-service`, `model-inference-history-service`, `model-serving-service`, `llm-catalog-service`, `prompt-workflow-service`, `knowledge-index-service`, `retrieval-context-service`, `conversation-state-service`, `tool-registry-service`, `ai-evaluation-service`, `application-curation-service`, `document-reporting-service`, `marketplace-catalog-service`, `product-distribution-service`, `widget-registry-service`, `entity-resolution-service`, `geospatial-intelligence-service`, `global-branch-service`

La implicacion arquitectonica es importante: OpenFoundry ya no esta en una fase “21 macroservicios vs 85 objetivos”, sino en una fase intermedia donde muchos bounded contexts ya tienen nombre y proceso propio, pero el ownership funcional todavia sigue bastante concentrado en los servicios legacy.

## Servicios con mayor sobrecarga estructural

Tomando como referencia numero aproximado de ficheros `.rs`, `.sql` y `.toml` en los **21 servicios legacy**, hoy los mayores candidatos a cierre de ownership o particion adicional son estos:

| Servicio legacy | Ficheros aprox. | Lectura arquitectonica |
| --- | --- | --- |
| `ontology-service` | `72` | sigue siendo el macrodominio operacional con mayor blast radius |
| `data-connector` | `60` | aun concentra conectividad, sync, discovery y virtual tables |
| `auth-service` | `53` | mezcla identidad, autorizacion, sesiones, admin y seguridad |
| `dataset-service` | `51` | mantiene catalogo, CRUD, upload, branches y storage |
| `ml-service` | `47` | conserva overview, features, training y partes de serving |
| `ai-service` | `42` | sigue mezclando chat, providers, agents y runtime conversacional |
| `pipeline-service` | `37` | aun centraliza ejecucion, runs, retries y triggers |
| `marketplace-service` | `36` | catalogo, installs y rollout siguen sin cerrarse del todo |

Esto no significa que los servicios con menos ficheros sean shells. Significa que estos ocho legacy son donde mas valor arquitectonico se recupera si se termina de mover ownership hacia los servicios ya extraidos.

## Conclusion ejecutiva

La conclusion fuerte hoy es esta:

- la version anterior de esta guia quedo desactualizada en su “foto actual”: `services/` ya no contiene solo `21` macroservicios, sino `65` servicios reales
- la mayor parte de las extracciones faciles y de alto valor **ya existe** como servicio con nombre propio
- el trabajo pendiente ya no es “crear directorios”, sino **mover ownership funcional, rutas, handlers y contratos** fuera de los legacy
- ningun servicio legacy puede considerarse todavia un shell puro solo por el estado del arbol actual; incluso los candidatos mas evidentes a shell siguen conservando peso no trivial
- `fusion-service`, `geospatial-service` y `code-repo-service` siguen siendo extensiones code-first validas respecto a la taxonomia derivada solo desde documentacion

## Mapeo resumido

La tabla de abajo reemplaza el mapeo resumido original. Ya no indica solo “partir o mantener”, sino el estado real de la migracion servicio por servicio.

### Leyenda de estado

- `Alineado`: el servicio legacy ya representa bastante bien el bounded context principal y no necesita particion urgente.
- `Particion inicial`: existe alguna extraccion real, pero el legacy sigue siendo claramente el centro del dominio.
- `Particion avanzada`: existen varias extracciones reales, pero el legacy todavia conserva ownership significativo.
- `Transicion iniciada`: ya existe un sucesor claro para el dominio, pero el legacy aun no es shell ni se puede retirar.

### Matriz completa de estado actual

| Servicio legacy | Peso aprox. | Objetivo documental | Servicios reales relacionados | Estado | Pendiente exacto |
| --- | --- | --- | --- | --- | --- |
| `gateway` | `17` | `1` | `gateway` | `Alineado` | mantenerlo como entrypoint y solo crecer hacia `76` si se materializan custom endpoints versionados |
| `auth-service` | `53` | `2`, `3`, `5`, `11`, `14`, `15` | `auth-service`, `authorization-policy-service`, `cipher-service`, `oauth-integration-service`, `session-governance-service` | `Particion avanzada` | falta un `Identity Federation Service` dedicado; mover login, MFA, SSO y `api_keys`; decidir si `auth-service` queda como facade transitoria o se retira |
| `audit-service` | `30` | `7`, `10`, `6` parcial, `12` parcial, `13` parcial | `audit-service`, `sds-service` | `Particion inicial` | falta separar governance/constraints, retention y deletion; dejar `audit-service` con audit core o partirlo mas |
| `data-connector` | `60` | `18`, `19`, `26`, `27`, `5` parcial | `data-connector`, `connector-management-service`, `ingestion-replication-service`, `virtual-table-service` | `Particion avanzada` | falta `CDC Metadata and Resolution Service`; mover discovery, schema inference, agents y credenciales restantes fuera del legacy |
| `dataset-service` | `51` | `20`, `21`, `12`, `30`, `31` | `dataset-service`, `data-asset-catalog-service`, `dataset-versioning-service`, `dataset-quality-service`, `lineage-service` | `Particion avanzada` | quality, lint, profile, expectations y rules ya viven en `dataset-quality-service`; falta `Retention Policy Service`; mover CRUD, upload, views, export y branches restantes; cerrar dependencia funcional del legacy sobre lineage |
| `streaming-service` | `29` | `25`, `27` parcial, `29` parcial | `streaming-service`, `event-streaming-service`, `ingestion-replication-service CDC metadata module`, `time-series-data-service` | `Particion iniciada` | runtime delegado a `event-streaming-service`; CDC metadata en `ingestion-replication-service CDC metadata module`; modelado time-series en `time-series-data-service`; cerrar replay/windows residuales antes del retiro |
| `query-service` | `22` | `28`, `60` | `query-service`, `sql-warehousing-service` | `Particion iniciada` | jobs y storage intermedio de SQL warehousing ya en `sql-warehousing-service`; vigilar solo si `saved queries` y `60` necesitan separacion hacia analitica |
| `pipeline-service` | `37` | `22`, `23`, `24`, `31` | `pipeline-service`, `pipeline-authoring-service`, `pipeline-build-service`, `pipeline-schedule-service`, `lineage-service` | `Particion avanzada` | scheduling, due runs y backfills ya viven en `pipeline-schedule-service`; cerrar workers y compatibilidad temporal de runs/retries antes del retiro |
| `ontology-service` | `72` | `32`, `33`, `34`, `35`, `36`, `37`, `38`, `71` parcial | `ontology-service`, `ontology-definition-service`, `object-database-service`, `ontology-query-service`, `ontology-actions-service`, `ontology-funnel-service`, `ontology-functions-service`, `ontology-security-service` | `Particion avanzada` | falta `Scenario Simulation Service`; mover projects, access, rules y rutas residuales; reducir el ownership central del legacy |
| `fusion-service` | `27` | extension code-first `Entity Resolution and Golden Record Service` | `fusion-service`, `entity-resolution-service` | `Transicion iniciada` | completar el rename funcional y mover matching, merge, jobs, review y golden records al nuevo servicio; `fusion-service` aun no es shell |
| `ml-service` | `47` | `39`, `40`, `41`, `42`, `43`, `44`, `45`, `46` | `ml-service`, `ml-experiments-service`, `model-catalog-service`, `model-deployment-service`, `model-evaluation-service`, `model-inference-history-service`, `model-serving-service` | `Particion avanzada` | faltan `Model Adapter and Packaging Service` y `Model Lifecycle, Submissions and Objectives Service`; mover features, training y overview restantes |
| `ai-service` | `42` | `47`, `48`, `49`, `50`, `51`, `52`, `53`, `54`, `55` parcial | `ai-service`, `llm-catalog-service`, `prompt-workflow-service`, `knowledge-index-service`, `retrieval-context-service`, `conversation-state-service`, `tool-registry-service`, `ai-evaluation-service` | `Particion avanzada` | faltan `Agent Runtime and Chatbot Orchestration Service` y `Document Intelligence Service`; mover chat, providers y agents restantes fuera del legacy |
| `workflow-service` | `27` | `8`, `65`, `66`, `67` | `workflow-service`, `approvals-service`, `pipeline-schedule-service` | `Particion inicial` | scheduling cron/event y due runs ya salen por `pipeline-schedule-service`; faltan `Workflow Automation Service`, `Automation Operations Control Plane Service` y `Workflow Lineage and Trace Service`, ademas de sacar `approvals` completamente del legacy |
| `notebook-service` | `32` | `61`, `62`, `75` parcial | `notebook-service`, `document-reporting-service` | `Particion inicial` | falta `Managed Workspace Orchestration Service`; separar `notepad`/reporting documental y workspace orchestration |
| `app-builder-service` | `31` | `56` parcial, `68`, `69`, `70`, `73` parcial, `76` parcial | `app-builder-service`, `application-curation-service`, `widget-registry-service` | `Particion inicial` | faltan `Application Composition Service`, `Developer Console and Application Control Plane Service`, `Custom Endpoints Publishing and Gateway Service` y una decision clara sobre `56` |
| `report-service` | `28` | `62`, `17` parcial | `report-service`, `document-reporting-service`, `notification-service` | `Particion inicial` | consolidar reporting en `document-reporting-service` y decidir si el delivery sale por completo hacia notifications |
| `code-repo-service` | `33` | `78` parcial + extension code-first `Code Repository and Review Service` | `code-repo-service`, `global-branch-service` | `Transicion iniciada` | separar branch orchestration transversal del dominio Git/CI/review; `code-repo-service` aun mantiene bastante logica propia |
| `marketplace-service` | `36` | `16`, `69` parcial, `73` parcial | `marketplace-service`, `marketplace-catalog-service`, `product-distribution-service` | `Transicion iniciada` | mover catalogo, installs, reviews, rollout y promotion; resolver fleets y enrollment branches; el legacy aun no es shell |
| `nexus-service` | `29` | `16` | `nexus-service` | `Alineado` | mantenerlo como nucleo tecnico de federacion y extraer `audit_bridge` o telemetry solo si ganan peso propio |
| `geospatial-service` | `23` | extension code-first `Geospatial Intelligence Service` | `geospatial-service`, `geospatial-intelligence-service` | `Transicion iniciada` | mover layers, query, clustering, routing, geocode y tiles al nuevo servicio; `geospatial-service` aun no es shell |
| `notification-service` | `29` | `17` | `notification-service` | `Alineado` | mantenerlo y extraer `digest` o `rules_engine` solo si se convierten en dominio propio |

### Clasificacion practica de los 21 macroservicios legacy

#### Shell transicional claro hoy

- ninguno

#### Candidatos a quedar como shell o a retirarse en la siguiente iteracion

- `fusion-service`
- `geospatial-service`
- `marketplace-service`
- `code-repo-service`

#### Servicios legacy que siguen teniendo logica de negocio fuerte

- `auth-service`
- `audit-service`
- `data-connector`
- `dataset-service`
- `pipeline-service`
- `ontology-service`
- `ml-service`
- `ai-service`
- `workflow-service`
- `notebook-service`
- `app-builder-service`

#### Servicios legacy alineados que no conviene llamar shell

- `gateway`
- `streaming-service`
- `query-service`
- `report-service`
- `nexus-service`
- `notification-service`

## Gaps entre la taxonomia documental y el codigo real

Antes de entrar servicio por servicio, hay cuatro tensiones que conviene dejar explicitas:

### 0. La taxonomia documental sigue teniendo bounded contexts sin servicio dedicado en el arbol

Aunque hoy ya existen `44` extracciones materializadas, todavia hay objetivos documentales que no aparecen como servicio dedicado con nombre propio. Los gaps mas claros son estos:

- `Identity Federation Service`
- `Tenancy, Organizations, Spaces and Projects Service`
- `Security Governance and Constraint Service`
- `Retention Policy Service`
- `Lineage-aware Deletion Service`
- `CDC Metadata and Resolution Service`
- `Model Adapter and Packaging Service`
- `Model Lifecycle, Submissions and Objectives Service`
- `Agent Runtime and Chatbot Orchestration Service`
- `Document Intelligence Service`
- `Workflow Automation Service`, `Automation Operations Control Plane Service` y `Workflow Lineage and Trace Service` como servicios dedicados separados del legacy
- `Application Composition Service`, `Developer Console and Application Control Plane Service`, `Custom Endpoints Publishing and Gateway Service` y `Managed Workspace Orchestration Service`
- `AI Application Generation Service` como runtime o control plane propio, no solo como consumer indirecto
- `Scenario Simulation Service`
- `MCP Orchestration and Exposure Service`
- `Compute Modules Control Plane Service` y `Compute Modules Runtime Service`
- `Monitoring Rules and Scope Engine Service`, `Health Check Evaluation Service`, `Execution Observability Service`, `Telemetry Governance and Export Service` y `Code Security Scanning Service`

El arbol actual, por tanto, ya no esta “antes de empezar”, pero tampoco cubre todavia toda la taxonomia documental como ownership operativo separado.

### 1. `fusion-service` no es Fusion tipo spreadsheet

El codigo actual implementa reglas de matching, merge strategies, clusters, review queue y golden records. Eso es MDM o entity resolution, no `Spreadsheet Computation Service`.

Recomendacion:

- no forzar `fusion-service` dentro del servicio 63
- renombrarlo a algo como `entity-resolution-service`
- anadir una extension temporal a la taxonomia: `Entity Resolution and Golden Record Service`

### 2. `geospatial-service` es un dominio de primer nivel en el codigo

El codigo ya tiene layers, feature query, clustering, routing, geocoding y vector tiles. No es un detalle secundario.

Recomendacion:

- mantenerlo como dominio propio
- tratarlo como extension code-first: `Geospatial Intelligence Service`

### 3. `code-repo-service` tambien es un dominio real

Repositorios, ramas, commits, diffs, merge requests, comentarios, integraciones y CI no encajan completos dentro de `78. Global Branch Orchestration Service`.

Recomendacion:

- usar `78` para la parte transversal de branching
- mantener un bounded context adicional tipo `Code Repository and Review Service`

## Guia de particion servicio por servicio

### 1. `gateway`

**Lo que hace hoy**

- enruta por prefijo a todos los backends
- aplica enforcement inicial de sesiones scoped y zero-trust
- propaga cabeceras de contexto de auth y tenant

**Evidencia en codigo**

- `src/proxy/service_router.rs`
- enruta auth, datasets, pipelines, ontology, workflows, notebooks, ml, ai, reports, marketplace, nexus y apps

**Destino**

- `1. Edge Gateway Service`

**Decision**

- mantenerlo como esta
- solo crecerlo si extraes `76. Custom Endpoints Publishing and Gateway Service`

**Prioridad**

- baja

### 2. `auth-service`

**Lo que hace hoy**

- autenticacion basica
- refresh y MFA
- SSO providers
- users, groups, roles, permissions y policies
- restricted views
- scoped sessions y guest sessions
- control panel administrativo
- operaciones cryptograficas tipo `cipher/hash/sign/verify`

**Evidencia en codigo**

- handlers: `login`, `register`, `mfa`, `sso`, `user_mgmt`, `group_mgmt`, `role_mgmt`, `permission_mgmt`, `policy_mgmt`, `restricted_views`, `sessions`, `control_panel`, `security_ops`
- dominios: `oauth`, `saml`, `rbac`, `abac`, `sessions`, `api_keys`, `security`

**Destino**

- `2. Identity Federation Service`
- `3. Authorization, Mandatory Controls and Policy Engine`
- `5. Application and OAuth Integration Service`
- `11. Cryptographic Obfuscation Service`
- `14. Network Boundary Policy Service` solo si aqui acaba viviendo parte del enforcement de egress
- `15. Platform Experience, Scoped Sessions and Enablement Service` por `control_panel` y `scoped sessions`

**Extracciones recomendadas**

1. extraer `security_ops` a `11`
2. extraer `sessions` y `restricted_views` hacia `15` y parte de `3`
3. extraer `sso`, `oauth`, `saml`, `api_keys` hacia `2` y `5`
4. dejar `roles`, `permissions`, `policies`, `abac`, `rbac` como nucleo de `3`

**Prioridad**

- muy alta

### 3. `audit-service`

**Lo que hace hoy**

- audit log y collector
- busqueda de eventos y anomalias
- escaneo de datos sensibles
- governance templates y policy posture
- reportes de compliance
- flujos GDPR export y erase

**Evidencia en codigo**

- handlers: `events`, `policies`, `reports`
- dominios: `immutable_log`, `collector`, `sds`, `governance`, `gdpr`, `alerting`

**Destino**

- `7. Audit and Compliance Service`
- `10. Sensitive Data Discovery and Automated Remediation Service`
- `12. Retention Policy Service` de forma indirecta
- `13. Lineage-aware Deletion Service` de forma parcial si GDPR acaba siendo cascada lineage-aware
- `6. Security Governance and Constraint Service` para governance templates

**Extracciones recomendadas**

1. mover `sds` a `10`
2. dejar `events`, `collector`, `reports` como `7`
3. mover `governance templates` a `6`
4. decidir si `gdpr erase` vive en `13` o se queda provisionalmente en `7`

**Prioridad**

- alta

### 4. `data-connector`

**Lo que hace hoy**

- catalogo de conectores y capabilities
- conexiones y prueba de conexiones
- descubrimiento de fuentes
- registros masivos
- virtual table query
- sync jobs
- connector agents y heartbeat
- generacion `hyperauto`

**Evidencia en codigo**

- handlers: `catalog`, `connections`, `registrations`, `sync_ops`, `agents`, `hyperauto`
- dominios: `scheduler`, `sync_engine`, `discovery`, `schema_inference`, `secret_manager`, `egress`

**Destino**

- `18. Connector Management Service`
- `19. Ingestion and Replication Service`
- `26. Virtual Table and External Table Orchestration Service`
- `27. CDC Metadata and Resolution Service`
- `5. Application and OAuth Integration Service` de forma parcial por credenciales y third-party auth

**Extracciones recomendadas**

1. separar `connections`, `catalog`, `capabilities` como `18`
2. mover `sync`, `agents`, `scheduler` a `19`
3. aislar `virtual-tables/query` en `26`
4. si la parte incremental crece, extraer `27` desde `sync_engine`

**Prioridad**

- alta

### 5. `dataset-service`

**Lo que hace hoy**

- CRUD de datasets
- catalog facets
- upload, preview y schema
- versions y transactions
- filesystem/files export
- views
- branching, checkout, merge y promote
- quality, profile, lint y quality rules

**Evidencia en codigo**

- handlers: `crud`, `catalog`, `preview`, `versions`, `transactions`, `export`, `views`, `branches`, `quality`, `lint`
- dominios: `catalog`, `transactions`, `retention`, `lineage`, `quality`, `storage`

**Destino**

- `20. Data Asset Catalog and Metadata Service`
- `21. Dataset Versioning and Transaction Service`
- `12. Retention Policy Service`
- `30. Data Quality and Expectations Service`
- `31. Data Lineage and Impact Analysis Service` solo en lo que hoy ya se toca

**Extracciones recomendadas**

1. separar catalogo y metadata como `20`
2. mover versions, transactions y branching a `21`
3. mover quality, lint y profile a `30`
4. si `retention` tiene tablas y reglas propias, sacarlo a `12`

**Prioridad**

- muy alta

### 6. `streaming-service`

**Lo que hace hoy**

- streams
- push de eventos
- dead letters y replay
- windows
- topologies
- runtime y live tail
- catalogo de connectors de streaming

**Evidencia en codigo**

- handlers: `streams`, `topologies`
- dominios: `engine`, `backpressure`, `connectors`

**Destino**

- `25. Streaming Service`
- `27. CDC Metadata and Resolution Service` de forma parcial si se hace mas fuerte la semantica incremental

**Decision**

- mantenerlo como servicio casi independiente
- solo extraer conectores si se solapan demasiado con `data-connector`

**Prioridad**

- media-baja

### 7. `query-service`

**Lo que hace hoy**

- execute
- explain
- saved queries

**Evidencia en codigo**

- handlers: `execute`, `explain`, `saved`
- dominios: `parser`, `planner`, `executor`, `cache`, `federation`

**Destino**

- `60. SQL and BI Gateway Service`

**Decision**

- mantenerlo
- solo vigilar si `saved queries` deberia vivir mas cerca de `57`, `58` o `59`

**Prioridad**

- baja

### 8. `pipeline-service`

**Lo que hace hoy**

- CRUD de pipelines
- ejecucion manual y retry
- scheduler interno de cron
- runs
- lineage por dataset, columnas e impacto
- triggers internos de reconstruccion

**Evidencia en codigo**

- handlers: `crud`, `execute`, `runs`, `lineage`
- dominios: `engine`, `executor`, `retry`, `versioning`, `lineage`

**Destino**

- `22. Pipeline Authoring and Compilation Service`
- `23. Pipeline Build Orchestration Service`
- `24. Schedule Orchestration Service`
- `31. Data Lineage and Impact Analysis Service`

**Extracciones recomendadas**

1. separar definicion y validacion de pipeline en `22`
2. extraer ejecucion, retries y workers a `23`
3. extraer cron, run-due y ventanas a `24`
4. dejar lineage como servicio propio `31`

**Prioridad**

- muy alta

### 9. `ontology-service`

**Lo que hace hoy**

- tipos, propiedades, interfaces y shared property types
- functions con validate, simulate, runs y metrics
- funnel de fuentes, health y runs
- proyectos, memberships y resources
- reglas y machinery queue
- actions, execute y branches what-if
- objetos, queries, knn, neighbors, views y simulate
- search, graph, quiver visual functions
- object sets
- links y link instances

**Evidencia en codigo**

- handlers: `types`, `properties`, `interfaces`, `shared_properties`, `functions`, `funnel`, `projects`, `rules`, `actions`, `objects`, `search`, `object_sets`, `links`, `storage`
- dominios: `type_system`, `object_sets`, `function_runtime`, `graph`, `indexer`, `search`, `rules`, `access`, `sync`, `time_series`

**Destino**

- `32. Ontology Definition and Type System Service`
- `33. Object Database Service`
- `34. Ontology Query, Search and Semantic Retrieval Service`
- `35. Actions and Operational Writeback Service`
- `36. Object Data Funnel and Indexing Service`
- `37. Ontology Functions Runtime Service`
- `38. Ontology Security and Permission Resolution Service`
- `71. Scenario Simulation Service` de forma parcial

**Extracciones recomendadas**

1. sacar `types`, `interfaces`, `shared_properties`, `link types` a `32`
2. sacar `objects`, `links`, `object views` a `33`
3. sacar `search`, `knn`, `graph`, `object_sets`, `quiver visual functions` a `34`
4. sacar `actions`, `inline-edit`, `what-if`, `execute-batch` a `35`
5. sacar `funnel`, `storage insights`, sincronizacion e indexing a `36`
6. sacar `functions`, `validate`, `simulate`, `metrics` a `37`
7. mover `projects`, `memberships`, `rules de acceso`, `access` a `38`
8. si los `what-if branches` crecen, materializar `71`

**Prioridad**

- maxima

### 10. `fusion-service`

**Lo que hace hoy**

- reglas de matching
- merge strategies
- jobs de fusion
- clusters
- review queue
- golden records

**Evidencia en codigo**

- handlers: `rules`, `jobs`, `clusters`
- dominios: `deduplication`, `merge`, `feedback`, `engine`

**Destino**

- no encaja bien en la taxonomia documental actual
- extension code-first recomendada: `Entity Resolution and Golden Record Service`

**Decision**

- renombrar antes de partir
- no mezclarlo con `63. Spreadsheet Computation Service`

**Prioridad**

- media

### 11. `ml-service`

**Lo que hace hoy**

- overview de plataforma ML
- experimentos y runs
- asset lineage
- modelos y versiones
- features y materializacion online
- training jobs
- deployments
- drift report
- realtime predict y batch predictions

**Evidencia en codigo**

- handlers: `overview`, `experiments`, `models`, `features`, `training`, `deployments`, `predictions`
- dominios: `training`, `serving`, `feature_store`, `monitoring`, `drift`, `interop`

**Destino**

- `39. Model Adapter and Packaging Service`
- `40. Model Catalog and Registry Service`
- `41. Model Experiment Tracking Service`
- `42. Model Lifecycle, Submissions and Objectives Service`
- `43. Model Deployment Control Plane Service`
- `44. Model Serving and Inference Runtime Service`
- `45. Model Evaluation and Metric Pipelines Service`
- `46. Model Inference History and Feedback Ledger Service`

**Extracciones recomendadas**

1. sacar experimentos y runs a `41`
2. dejar modelos y versiones en `40`
3. si hay workflow de promotion, checks y releases, moverlo a `42`
4. separar deployments como `43`
5. separar predict, online serving y batch inferencia a `44`
6. mover drift y metricas a `45` y `46`

**Prioridad**

- muy alta

### 12. `ai-service`

**Lo que hace hoy**

- providers
- prompt templates y render
- knowledge bases y documentos
- search sobre conocimiento
- tools
- agents y execute
- conversations
- chat completions
- copilot ask
- guardrails evaluate
- benchmarks

**Evidencia en codigo**

- handlers: `chat`, `prompts`, `knowledge`, `tools`, `agents`
- dominios: `llm`, `rag`, `copilot`, `evaluation`, `agents`

**Destino**

- `47. LLM Catalog and Discovery Service`
- `48. AIP Logic and Prompt Workflow Service`
- `49. Knowledge Source Registration and Indexing Service`
- `50. Retrieval and Knowledge Context Service`
- `51. Conversation Session and Thread State Service`
- `52. Tool Registry and Execution Service`
- `53. Agent Runtime and Chatbot Orchestration Service`
- `54. AI Evaluation and Regression Service`
- `55. Document Intelligence Service` solo si esa capacidad aparece de verdad mas adelante

**Extracciones recomendadas**

1. separar `providers` a `47`
2. mover prompts y render a `48`
3. mover knowledge bases y documents a `49`
4. mover search de conocimiento a `50`
5. mover conversations a `51`
6. mover tools a `52`
7. mover agents y copilot runtime a `53`
8. mover guardrails y benchmarks a `54`

**Prioridad**

- maxima

### 13. `workflow-service`

**Lo que hace hoy**

- CRUD de workflows
- approvals
- triggers por eventos
- triggers cron
- manual runs
- lineage interno de runs

**Evidencia en codigo**

- handlers: `crud`, `approvals`, `execute`, `runs`
- dominios: `executor`, `human_in_loop`, `lineage`, `branching`, `simulation`, `parallel`, `compensation`

**Destino**

- `8. Approvals Service`
- `65. Workflow Automation Service`
- `66. Automation Operations Control Plane Service`
- `67. Workflow Lineage and Trace Service`

**Extracciones recomendadas**

1. sacar `approvals` a `8`
2. dejar definicion y ejecucion de workflows en `65`
3. si la cola y el estado operativo crecen, sacar `66`
4. mover tracing, lineage y run history a `67`

**Prioridad**

- alta

### 14. `notebook-service`

**Lo que hace hoy**

- notebooks, cells y ejecucion
- sessions
- workspace files
- documentos tipo notepad
- presence colaborativa
- export de documentos

**Evidencia en codigo**

- handlers: `crud`, `execute`, `sessions`, `workspace`, `notepad`, `collaborate`
- dominios: `kernel`, `environment`, `collaboration`, `scheduler`

**Destino**

- `61. Interactive Code Analysis and Notebook Runtime Service`
- `62. Document Reporting Service`
- `75. Managed Workspace Orchestration Service` de forma parcial

**Extracciones recomendadas**

1. dejar notebooks, cells, kernels y sessions en `61`
2. mover `notepad` a `62`
3. si workspaces crecen como dominio fuerte, llevarlos a `75`

**Prioridad**

- media-alta

### 15. `app-builder-service`

**Lo que hace hoy**

- apps
- creacion desde templates
- widgets catalog
- preview
- import y export de slate package
- pages
- versions
- publish
- public app hosting por slug

**Evidencia en codigo**

- handlers: `apps`, `widgets`, `preview`, `slate`, `pages`, `publish`
- dominios: `renderer`, `templating`, `slate`, `permissions`, `data_resolver`, `embedding`

**Destino**

- `56. AI Application Generation Service` de forma indirecta como consumer
- `68. Application Composition Service`
- `69. Workspace and Application Curation Service`
- `70. Custom Widget Registry and Host Bridge Service`
- `73. Developer Console and Application Control Plane Service` de forma parcial
- `76. Custom Endpoints Publishing and Gateway Service` de forma parcial si el publishing evoluciona a endpoints

**Extracciones recomendadas**

1. dejar app composition, bindings, events y pages en `68`
2. mover widgets y widget catalog a `70`
3. mover templates, promoted apps y publicacion curada a `69`
4. si releases, domains y deployment policies crecen, extraer `73`

**Prioridad**

- alta

### 16. `report-service`

**Lo que hace hoy**

- definiciones de reportes
- generacion
- historia de ejecuciones
- schedules
- descarga
- delivery por SMTP u object store

**Evidencia en codigo**

- handlers: `crud`, `generate`, `schedule`, `download`
- dominios: `cron`, `distribution`, `data_fetcher`, `generators`

**Destino**

- `62. Document Reporting Service`
- `17. Notification and Alerting Service` de forma parcial para la entrega

**Decision**

- mantenerlo bastante entero
- solo desacoplar delivery si acaba duplicando demasiado al `notification-service`

**Prioridad**

- media

### 17. `code-repo-service`

**Lo que hace hoy**

- repos
- branches
- commits
- files, diff y search
- CI
- integrations y sync
- merge requests y comments

**Evidencia en codigo**

- handlers: `repos`, `branches`, `commits`, `files`, `diff`, `integrations`, `merge_requests`
- dominios: `git`, `ci`, `review`, `search`

**Destino**

- `78. Global Branch Orchestration Service` solo para la parte transversal
- extension code-first recomendada: `Code Repository and Review Service`

**Decision**

- no romperlo todavia por la mitad si no existe antes el servicio transversal de branching
- primero extraer capacidades de branch orchestration compartidas y luego dejar el resto como dominio Git

**Prioridad**

- media

### 18. `marketplace-service`

**Lo que hace hoy**

- categories, listings, versions, reviews, search, installs
- fleets, promotion gates, enrollment branches y sync devops

**Evidencia en codigo**

- handlers: `browse`, `publish`, `reviews`, `install`, `devops`
- dominios: `registry`, `activation`, `dependency`, `validator`, `devops`

**Destino**

- `16. Federation, Connected Hubs and Product Exchange Service`
- `69. Workspace and Application Curation Service` de forma parcial
- `73. Developer Console and Application Control Plane Service` de forma parcial

**Extracciones recomendadas**

1. marketplace catalog, listings, installs y reviews a `16`
2. promotion gates y fleets a un control plane de distribucion de producto
3. branches de enrollment a `78` si de verdad pasan a ser branching transversal

**Prioridad**

- media

### 19. `nexus-service`

**Lo que hace hoy**

- peers
- spaces
- contracts
- shares
- federated query
- replication plans
- schema compatibility
- audit bridge

**Evidencia en codigo**

- handlers: `peers`, `spaces`, `contracts`, `shares`, `consume`
- dominios: `federation`, `replication`, `schema_compat`, `access_proxy`, `audit_bridge`

**Destino**

- `16. Federation, Connected Hubs and Product Exchange Service`

**Decision**

- mantenerlo como el nucleo tecnico de `16`
- solo extraer `audit_bridge` si crece mas cerca de `7` o `84`

**Prioridad**

- media-baja

### 20. `geospatial-service`

**Lo que hace hoy**

- layers
- geospatial query
- clustering
- routing
- geocode y reverse geocode
- vector tiles

**Evidencia en codigo**

- handlers: `layers`, `features`, `geocode`, `tiles`
- dominios: `engine`, `indexer`, `tile_server`

**Destino**

- extension code-first recomendada: `Geospatial Intelligence Service`

**Decision**

- mantenerlo
- no esconderlo dentro de analytics generica porque ya tiene modelos y APIs propios

**Prioridad**

- baja

### 21. `notification-service`

**Lo que hace hoy**

- historial
- send
- preferencias
- WebSocket
- bus interno de eventos
- email y potencialmente otros canales

**Evidencia en codigo**

- handlers: `history`, `send`, `preferences`, `ws`
- dominios: `channels`, `digest`, `rules_engine`, `throttle`

**Destino**

- `17. Notification and Alerting Service`

**Decision**

- mantenerlo casi tal cual
- si `rules_engine` y `digest` crecen, ampliarlo sin partirlo primero

**Prioridad**

- baja

## Orden recomendado de migracion

## Estado actualizado de la migracion

### Lo que ya esta materializado en el arbol actual

- la antigua **Fase 1** esta muy avanzada: ya existen `cipher-service`, `session-governance-service`, `sds-service`, `approvals-service`, `widget-registry-service`, `dataset-quality-service` y `ai-evaluation-service`
- la antigua **Fase 2** ya existe como arbol de servicios, aunque no como ownership totalmente cerrado: `data-asset-catalog-service`, `dataset-versioning-service`, `lineage-service`, `pipeline-authoring-service`, `pipeline-build-service`, `pipeline-schedule-service`, `connector-management-service`, `ingestion-replication-service` y `virtual-table-service`
- la antigua **Fase 3** esta tambien materializada en nombres de servicio: `authorization-policy-service`, `oauth-integration-service`, `llm-catalog-service`, `prompt-workflow-service`, `knowledge-index-service`, `retrieval-context-service`, `conversation-state-service`, `tool-registry-service`, `application-curation-service` y `widget-registry-service`
- la antigua **Fase 4** ya empezo de verdad: existen `ontology-definition-service`, `object-database-service`, `ontology-query-service`, `ontology-actions-service`, `ontology-funnel-service`, `ontology-functions-service` y `ontology-security-service`
- la antigua **Fase 5** tambien esta iniciada: existen `entity-resolution-service`, `geospatial-intelligence-service`, `global-branch-service`, `marketplace-catalog-service` y `product-distribution-service`

La lectura correcta hoy no es “hay que crear las fases 1 a 5”, sino “hay que cerrar el ownership y retirar la autoridad residual de los legacy”.

### Lo que sigue pendiente por cerrar

1. cerrar `auth-service` en torno a `authorization-policy-service`, `cipher-service`, `oauth-integration-service` y `session-governance-service`, y decidir si falta un `identity-federation-service` dedicado
2. cerrar `data-connector`, `dataset-service` y `pipeline-service` para que los servicios ya extraidos sean la fuente de verdad operativa y no solo slices auxiliares
3. cerrar `ontology-service`, que es el legacy con mas peso y el mayor riesgo de ownership compartido
4. cerrar `ai-service` y `ml-service` introduciendo los dos gaps mas claros de cada dominio: `agent-runtime` y `model-lifecycle/adapter`
5. decidir si `workflow-service`, `app-builder-service`, `notebook-service` y `report-service` terminan en servicios nuevos adicionales o si se consolidan como bounded contexts mas gruesos
6. convertir `fusion-service`, `geospatial-service`, `marketplace-service` y `code-repo-service` en shells reales de compatibilidad o retirarlos del todo cuando el sucesor absorba el runtime

## Reglas practicas para ejecutar la migracion desde el estado actual

### 1. Primero mover ownership, luego apagar legacy

La migracion ya no consiste solo en “crear un servicio nuevo”. Consiste en que el servicio nuevo pase a ser la fuente de verdad de rutas, handlers, tablas y contratos de su bounded context.

### 2. Mantener `gateway` como capa de compatibilidad

Cada cierre de ownership debe seguir escondiendose detras de `gateway` para no romper `web` ni clientes internos mientras los legacy se vacian.

### 3. Separar modulos internos antes de separar bases de datos

Durante una iteracion de cierre conviene:

- mover cada bounded context a crates o modulos claros dentro del dominio
- dejar APIs explicitas entre contextos legacy y extraidos
- partir esquemas logicos antes de partir fisicamente la base de datos

### 4. Extraer por prefijos de rutas ya existentes

El codigo sigue dando buenas fronteras de extraccion por prefijo HTTP, por ejemplo:

- `/api/v1/ai/prompts`
- `/api/v1/ai/tools`
- `/api/v1/ai/knowledge-bases`
- `/api/v1/ontology/functions`
- `/api/v1/ontology/object-sets`
- `/api/v1/workflows/approvals`
- `/api/v1/datasets/{id}/quality`

Cuando la ruta ya existe, la extraccion sigue siendo menos traumática que una rotura basada solo en capas internas.

### 5. Separar control plane de runtime cuando todavia esten mezclados

Sigue aplicando especialmente a:

- ML
- AI
- pipeline execution
- ontology functions
- app publishing y product rollout

## Propuesta actualizada de estado intermedio realista

No recomiendo pasar de `65` servicios en el arbol a `85` despliegues independientes. Lo que si recomiendo es agrupar ownership y despliegue alrededor de macrodominios claros como estos:

1. `edge-gateway`: `gateway`
2. `platform-security-core`: `auth-service`, `authorization-policy-service`, `cipher-service`, `oauth-integration-service`, `session-governance-service`, `approvals-service`
3. `audit-governance`: `audit-service`, `sds-service`
4. `data-connectivity`: `data-connector`, `connector-management-service`, `ingestion-replication-service`, `virtual-table-service`
5. `data-assets`: `dataset-service`, `data-asset-catalog-service`, `dataset-versioning-service`, `dataset-quality-service`
6. `pipeline-lineage`: `pipeline-service`, `pipeline-authoring-service`, `pipeline-build-service`, `pipeline-schedule-service`, `lineage-service`
7. `streaming`: `streaming-service`
8. `ontology-core`: `ontology-service`, `ontology-definition-service`, `object-database-service`, `ontology-query-service`, `ontology-actions-service`, `ontology-funnel-service`, `ontology-functions-service`, `ontology-security-service`
9. `ml-platform`: `ml-service`, `ml-experiments-service`, `model-catalog-service`, `model-deployment-service`, `model-evaluation-service`, `model-inference-history-service`, `model-serving-service`
10. `aip-platform`: `ai-service`, `llm-catalog-service`, `prompt-workflow-service`, `knowledge-index-service`, `retrieval-context-service`, `conversation-state-service`, `tool-registry-service`, `ai-evaluation-service`
11. `workflow-automation`: `workflow-service`, `approvals-service`
12. `notebook-reporting`: `notebook-service`, `report-service`, `document-reporting-service`
13. `app-composition`: `app-builder-service`, `application-curation-service`, `widget-registry-service`
14. `marketplace-federation`: `marketplace-service`, `marketplace-catalog-service`, `product-distribution-service`, `nexus-service`
15. `notifications`: `notification-service`
16. `query-bi`: `query-service`
17. `entity-resolution`: `fusion-service`, `entity-resolution-service`
18. `code-repo`: `code-repo-service`, `global-branch-service`
19. `geospatial`: `geospatial-service`, `geospatial-intelligence-service`

Este es el estado intermedio mas realista: muchos nombres de servicio ya existen, pero todavia conviene pensar en ownership y despliegue por macrodominio antes de empujar una explosion operacional.

## Recomendacion final actualizada

Si tuviera que priorizar el siguiente tramo de trabajo, el orden seria este:

1. `ontology-service`, porque es el legacy mas pesado y el mas peligroso si sigue como centro del ownership
2. `auth-service`, porque ya tiene varias extracciones alrededor y aun concentra demasiadas responsabilidades incompatibles
3. `ai-service`, porque el arbol ya materializo casi todo el dominio salvo el runtime conversacional fuerte
4. `dataset-service` y `pipeline-service`, porque ya tienen slices claros y el retorno de cerrar ownership es alto
5. `ml-service`, para completar lifecycle/adapter y vaciar el legacy hacia catalog, experiments, deployment, serving y evaluation
6. `fusion-service`, `geospatial-service`, `marketplace-service` y `code-repo-service`, para decidir si de verdad pasan a shell o si siguen siendo dominios legacy con nombre viejo

La definicion de hecho para esta migracion no es “existe una carpeta nueva en `services/`”. La definicion de hecho es que el servicio nuevo deja al legacy sin ownership real sobre ese bounded context.
