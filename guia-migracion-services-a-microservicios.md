# Guia de migracion desde `services/` hacia los microservicios objetivo

## Objetivo

Este documento traduce el estado actual de `services/` a la taxonomia de bounded contexts definida en `microservicios-derivados-desde-foundry-docs.md`.

La idea no es redibujar Foundry en abstracto, sino responder a una pregunta concreta:

**si partiamos el codigo actual de OpenFoundry segun las responsabilidades que ya existen en Rust, en que microservicios objetivo deberia terminar cada servicio actual, y en que orden conviene extraerlos.**

## Metodo usado

El analisis se ha hecho leyendo el codigo real de cada servicio actual:

- `Cargo.toml` de cada servicio
- `src/main.rs` para entender el contrato HTTP y el surface publico
- `src/domain`, `src/handlers` y `src/models` para detectar bounded contexts mezclados
- numero de ficheros Rust, migraciones y volumen aproximado de codigo para estimar riesgo de particion

## Foto actual de `services/`

Hoy `services/` no contiene microservicios finos. Contiene **21 macroservicios** con varios bounded contexts mezclados:

1. `gateway`
2. `auth-service`
3. `audit-service`
4. `data-connector`
5. `dataset-service`
6. `streaming-service`
7. `query-service`
8. `pipeline-service`
9. `ontology-service`
10. `fusion-service`
11. `ml-service`
12. `ai-service`
13. `workflow-service`
14. `notebook-service`
15. `app-builder-service`
16. `report-service`
17. `code-repo-service`
18. `marketplace-service`
19. `nexus-service`
20. `geospatial-service`
21. `notification-service`

## Servicios con mayor sobrecarga estructural

Tomando como referencia numero aproximado de ficheros Rust, migraciones y volumen de codigo, los mayores candidatos a particion son estos:

| Servicio | Rust files aprox. | Migraciones aprox. | LOC aprox. | Lectura arquitectonica |
| --- | --- | --- | --- | --- |
| `ontology-service` | 53 | 15 | 21k | gran macrodominio operacional |
| `auth-service` | 43 | 6 | 8.6k | identidad, policy, sesiones y admin mezclados |
| `data-connector` | 56 | 3 | 8.3k | conectividad, sync y virtual tables juntos |
| `ai-service` | 33 | 5 | 7.2k | prompts, RAG, tools, agents y evals juntos |
| `pipeline-service` | 30 | 3 | 6.3k | authoring, builds, schedules y lineage juntos |
| `ml-service` | 41 | 2 | 6.0k | registry, experiments, serving y monitoring juntos |
| `dataset-service` | 41 | 6 | 5.4k | catalogo, versionado, branching y calidad juntos |

Esto no significa que los servicios pequenos esten perfectamente diseniados. Significa que, en terminos de retorno por particion, estos siete son donde mas valor arquitectonico vas a recuperar primero.

## Conclusion ejecutiva

La conclusion fuerte del analisis es esta:

- `gateway`, `query-service` y `notification-service` ya estan relativamente bien cortados.
- `dataset-service`, `pipeline-service`, `auth-service`, `ml-service`, `ai-service`, `workflow-service`, `app-builder-service` y sobre todo `ontology-service` son **macrodominios**.
- `fusion-service`, `geospatial-service` y `code-repo-service` expresan dominios reales del codigo que **no quedan bien cubiertos** por la taxonomia derivada solo desde documentacion. Conviene tratarlos como extensiones code-first.

## Mapeo resumido

| Servicio actual | Estado | Microservicios objetivo principales |
| --- | --- | --- |
| `gateway` | Mantener | `1. Edge Gateway Service` |
| `auth-service` | Partir | `2`, `3`, `5`, `11`, `14`, `15` |
| `audit-service` | Partir | `7`, `10`, `12`, `13` parcial, `6` parcial |
| `data-connector` | Partir | `18`, `19`, `26`, `27`, `5` parcial |
| `dataset-service` | Partir | `20`, `21`, `12`, `30`, `31` parcial |
| `streaming-service` | Afinar | `25`, `27` parcial |
| `query-service` | Mantener | `60` |
| `pipeline-service` | Partir | `22`, `23`, `24`, `31` |
| `ontology-service` | Partir fuerte | `32`, `33`, `34`, `35`, `36`, `37`, `38`, `71` parcial |
| `fusion-service` | Renombrar o extender taxonomia | extension code-first: `Entity Resolution and Golden Record Service` |
| `ml-service` | Partir | `39`, `40`, `41`, `42`, `43`, `44`, `45`, `46` |
| `ai-service` | Partir | `47`, `48`, `49`, `50`, `51`, `52`, `53`, `54`, `55` parcial |
| `workflow-service` | Partir | `8`, `65`, `66`, `67` parcial |
| `notebook-service` | Partir ligero | `61`, `62`, `75` parcial |
| `app-builder-service` | Partir | `56` parcial, `68`, `69`, `70`, `73` parcial, `76` parcial |
| `report-service` | Afinar | `62`, `17` parcial |
| `code-repo-service` | Renombrar o extender taxonomia | `78` parcial, extension code-first: `Code Repository and Review Service` |
| `marketplace-service` | Partir o reubicar | `16`, `69` parcial, `73` parcial |
| `nexus-service` | Afinar | `16` |
| `geospatial-service` | Mantener transicional | extension code-first: `Geospatial Intelligence Service` |
| `notification-service` | Mantener | `17` |

## Gaps entre la taxonomia documental y el codigo real

Antes de entrar servicio por servicio, hay tres tensiones que conviene dejar explicitas:

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

### Fase 1. Extracciones faciles con valor alto

- `auth-service` -> sacar `security_ops`, `sessions`, `restricted_views`
- `audit-service` -> sacar `sds`
- `workflow-service` -> sacar `approvals`
- `app-builder-service` -> sacar `widgets`
- `dataset-service` -> sacar `quality`
- `ai-service` -> sacar `evaluations`

### Fase 2. Romper los macrodominios de datos y ML

- `dataset-service` -> `20`, `21`, `30`
- `pipeline-service` -> `22`, `23`, `24`, `31`
- `ml-service` -> `40`, `41`, `43`, `44`, `45`, `46`
- `data-connector` -> `18`, `19`, `26`

### Fase 3. Romper los macrodominios de plataforma operacional

- `auth-service` -> `2`, `3`, `5`, `11`, `15`
- `ai-service` -> `47`, `48`, `49`, `50`, `51`, `52`, `53`, `54`
- `app-builder-service` -> `68`, `69`, `70`
- `notebook-service` -> `61`, `62`

### Fase 4. Particion grande de ontologia

- `ontology-service` -> `32`, `33`, `34`, `35`, `36`, `37`, `38`

Esta fase debe ir tarde porque es la de mayor blast radius.

### Fase 5. Ajuste de dominios especiales

- renombrar `fusion-service` a `entity-resolution-service`
- formalizar `code-repo-service` como dominio Git propio mas `78`
- formalizar `geospatial-service` como dominio propio
- separar en `marketplace-service` la parte de catalogo frente a la de rollout y promotion

## Reglas practicas para ejecutar la migracion

### 1. Primero separar modulos internos, luego procesos

Antes de crear procesos nuevos:

- mover cada bounded context a crates internos o modulos independientes
- dejar APIs explicitas entre contextos
- evitar acceder a tablas de otro dominio desde handlers cruzados

### 2. Extraer por rutas ya existentes

El codigo actual ya ayuda mucho porque la mayoria de bounded contexts aparecen por prefijos de rutas:

- `/api/v1/ai/prompts`
- `/api/v1/ai/tools`
- `/api/v1/ai/knowledge-bases`
- `/api/v1/ontology/functions`
- `/api/v1/ontology/object-sets`
- `/api/v1/workflows/approvals`
- `/api/v1/datasets/{id}/quality`

Cuando ya existe un prefijo claro, la extraccion es mucho menos traumática.

### 3. No partir bases de datos al principio

Durante una primera migracion:

- mantener una BD compartida por dominio grande
- partir esquemas logicos primero
- pasar a BD fisica por servicio solo cuando el contrato API ya este estable

### 4. Usar `gateway` como capa de compatibilidad

Cada extraccion debe esconderse detras de `gateway` para no romper a `web` ni a clientes internos.

### 5. Separar primero control plane de runtime cuando aplique

Aplica especialmente a:

- ML
- AI
- pipeline execution
- compute-style runtimes
- ontology functions

## Propuesta de estado intermedio realista

No recomiendo pasar de 21 servicios actuales a 85 despliegues.

El mejor estado intermedio para OpenFoundry seria algo parecido a esto:

1. `edge-gateway`
2. `platform-security-core`
3. `audit-governance`
4. `data-connectivity`
5. `data-assets`
6. `pipeline-lineage`
7. `streaming`
8. `ontology-core`
9. `ml-platform`
10. `aip-platform`
11. `workflow-automation`
12. `notebook-reporting`
13. `app-composition`
14. `marketplace-federation`
15. `notifications`
16. `query-bi`
17. `entity-resolution`
18. `code-repo`
19. `geospatial`

Eso ya te acerca mucho al mapa definitivo sin convertir la operacion en una explosion de despliegues.

## Recomendacion final

Si tuviera que elegir donde empezar, empezaria aqui:

1. `ontology-service`
2. `ai-service`
3. `auth-service`
4. `pipeline-service`
5. `dataset-service`
6. `ml-service`

Son los servicios donde el codigo actual muestra mas mezcla de responsabilidades y donde la particion te devuelve mas claridad arquitectonica por unidad de esfuerzo.
