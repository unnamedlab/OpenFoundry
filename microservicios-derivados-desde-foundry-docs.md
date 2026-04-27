# Lista definitiva de microservicios derivada de la documentacion tecnica de Foundry

## Alcance

Este documento propone una arquitectura de microservicios inferida exclusivamente a partir de la documentacion en `docs_original_palantir_foundry/foundry-docs`, ignorando por completo el codigo actual del repositorio.

No pretende describir la arquitectura interna real de Palantir Foundry. Lo que hace es traducir sus capacidades documentadas a bounded contexts y microservicios plausibles para una plataforma inspirada en Foundry.

La lista de abajo ya incorpora los refinamientos que surgieron tras analizar en detalle `AI Platform (AIP)`, `Analytics`, `Data connectivity & integration`, `Developer toolchain`, `Management & enablement`, `Model connectivity & development`, `Observability`, `Ontology building`, `Security & governance` y `Use case development`.

## Criterio de lectura

Esta es una lista definitiva de bounded contexts, no una recomendacion de desplegar cada item como un proceso aislado desde el dia 1.

Cuando la documentacion mostraba una capacidad con recursos propios, control plane propio, politicas propias o runtime propio, la he hecho emerger como servicio. Cuando solo mostraba una UI o una composicion sobre capacidades previas, la he dejado absorbida dentro de otros dominios.

## Microservicios definitivos

### Plataforma, seguridad y gobierno

1. **Edge Gateway Service**: punto de entrada unico para APIs y aplicaciones, con routing, terminacion TLS, rate limiting, resolucion de tenant y enforcement inicial de acceso.
2. **Identity Federation Service**: integra SAML, OIDC, OAuth e identity providers corporativos para autenticar usuarios, cuentas de servicio y sesiones.
3. **Authorization, Mandatory Controls and Policy Engine**: evalua roles, permissions, markings, CBAC, granular policies y restricciones de acceso sobre datos, objetos, acciones y recursos.
4. **Tenancy, Organizations, Spaces and Projects Service**: administra enrollments, organizaciones, espacios, proyectos y sus fronteras de comparticion.
5. **Application and OAuth Integration Service**: registra y gobierna third-party applications, OAuth clients entrantes y aplicaciones externas que acceden a la plataforma.
6. **Security Governance and Constraint Service**: administra project constraints, reglas de seguridad estructural y validaciones de integridad entre politicas y recursos.
7. **Audit and Compliance Service**: registra de forma inmutable accesos, cambios administrativos, decisiones, invocaciones de acciones, uso de modelos y eventos de seguridad.
8. **Approvals Service**: orquesta requests, tasks, reviewers, estados, invocacion y trazabilidad de cambios sujetos a aprobacion.
9. **Checkpoints and Purpose Justification Service**: solicita justificaciones para interacciones sensibles y almacena configuraciones y records auditables.
10. **Sensitive Data Discovery and Automated Remediation Service**: escanea datos sensibles, aplica match conditions y ejecuta acciones automaticas como markings, issues u ofuscacion.
11. **Cryptographic Obfuscation Service**: aplica cifrado, descifrado y hashing gobernado mediante channels, licenses y permisos de uso.
12. **Retention Policy Service**: administra politicas clasicas de retencion sobre datasets y transacciones no lineage-aware.
13. **Lineage-aware Deletion Service**: elimina datos y descendientes aguas abajo mediante politicas lineage-aware estilo Data Lifetime.
14. **Network Boundary Policy Service**: gobierna ingress, egress, private link, proxies y otras fronteras de red de la plataforma.
15. **Platform Experience, Scoped Sessions and Enablement Service**: administra branding, home pages, sesiones acotadas por markings, walkthroughs y experiencia guiada para usuarios.
16. **Federation, Connected Hubs and Product Exchange Service**: gobierna sharing entre enrollments, connected hubs, remote stores y distribucion de productos entre dominios.
17. **Notification and Alerting Service**: centraliza notificaciones internas, alertas operativas y conectores hacia email, Slack, PagerDuty, webhooks u otros canales.

### Integracion e ingenieria de datos

18. **Connector Management Service**: da de alta conexiones a bases de datos, APIs, SaaS, ficheros y otras fuentes externas, incluyendo credenciales y metadatos.
19. **Ingestion and Replication Service**: ejecuta cargas batch, micro-batch, streaming y exportaciones, manteniendo sincronizacion y politicas de refresh.
20. **Data Asset Catalog and Metadata Service**: mantiene el catalogo de datasets, streams, views, media sets, virtual tables, Iceberg tables y sus metadatos.
21. **Dataset Versioning and Transaction Service**: gestiona snapshots, append, update, delete, branching, transacciones y reglas de consistencia sobre datasets.
22. **Pipeline Authoring and Compilation Service**: transforma definiciones de pipeline en planes ejecutables, valida integridad, aplica pruning y genera logica intermedia.
23. **Pipeline Build Orchestration Service**: resuelve DAGs, dependencias, locks, retries, staleness y ejecucion de builds.
24. **Schedule Orchestration Service**: administra cron jobs, triggers por eventos, ventanas temporales, backfills y relacion entre schedules y builds.
25. **Streaming Service**: ofrece procesamiento de eventos con hot buffer, cold storage, checkpoints, particiones y semanticas at-least-once o exactly-once.
26. **Virtual Table and External Table Orchestration Service**: expone tablas virtuales y external tables sobre sistemas externos sin ingerir fisicamente todos los datos.
27. **CDC Metadata and Resolution Service**: gestiona change data capture, resolucion de cambios y metadatos para pipelines incrementales y sincronizacion continua.
28. **SQL Warehousing Service**: soporta workflows de SQL warehousing, persistencia intermedia y patrones de transformacion SQL a gran escala.
29. **Time-Series Data Service**: modela, almacena y sirve datos temporales especializados para workloads de series temporales.
30. **Data Quality and Expectations Service**: define expectativas, checks de calidad e invariantes operacionales sobre activos de datos.
31. **Data Lineage and Impact Analysis Service**: reconstruye linaje extremo a extremo, analisis de impacto y proveniencia entre fuentes, pipelines y productos de datos.

### Ontologia operacional

32. **Ontology Definition and Type System Service**: define y versiona object types, link types, action types, interfaces, shared properties y object type groups.
33. **Object Database Service**: almacena representaciones materializadas de objetos y relaciones para lecturas operacionales de baja latencia.
34. **Ontology Query, Search and Semantic Retrieval Service**: resuelve busquedas, filtros, agregaciones, traversals, object sets y retrieval semantico sobre objetos.
35. **Actions and Operational Writeback Service**: aplica mutaciones de negocio sobre objetos y links, con side effects, action log y writeback consistente.
36. **Object Data Funnel and Indexing Service**: transforma datasets, restricted views, streams y user edits en objetos indexados y sincronizados.
37. **Ontology Functions Runtime Service**: ejecuta funciones tipadas que leen la ontologia, recorren relaciones, enriquecen datos o disparan logica operacional.
38. **Ontology Security and Permission Resolution Service**: resuelve permisos sobre recursos ontologicos, objetos, links, propiedades y consultas operacionales.

### Modelos y ML

39. **Model Adapter and Packaging Service**: empaqueta artifacts, adapters, sidecars y contratos de inferencia para modelos internos o externos.
40. **Model Catalog and Registry Service**: publica el inventario de modelos ML disponibles, versiones, metadatos, permisos y linaje.
41. **Model Experiment Tracking Service**: registra experimentos, corridas, parametros, metricas y artefactos para entrenamiento y evaluacion.
42. **Model Lifecycle, Submissions and Objectives Service**: gobierna submissions, checks, metadata, reviews, releases y objetivos de modelado.
43. **Model Deployment Control Plane Service**: administra despliegues batch y live, autoscaling, rollout, versionado y politicas de despliegue.
44. **Model Serving and Inference Runtime Service**: ejecuta inferencia online o batch con contratos estables, type safety y observabilidad operacional.
45. **Model Evaluation and Metric Pipelines Service**: ejecuta evaluaciones clasicas de modelos, subsets, evaluators, fairness, robustness y metric sets.
46. **Model Inference History and Feedback Ledger Service**: registra inputs, outputs, errores y feedback para drift, retraining y analitica de uso.

### AIP, LLMs y AI operativa

47. **LLM Catalog and Discovery Service**: publica el catalogo de LLMs disponibles para AIP y sus capacidades, limites y metadatos de uso.
48. **AIP Logic and Prompt Workflow Service**: ejecuta workflows LLM no-code o low-code con prompts, bloques, variables y acceso gobernado a la ontologia.
49. **Knowledge Source Registration and Indexing Service**: registra, indexa y sirve fuentes documentales o de conocimiento para asistentes y chatbots.
50. **Retrieval and Knowledge Context Service**: construye y recupera contexto para prompts, agentes y asistentes desde ontologia, documentos y otras fuentes.
51. **Conversation Session and Thread State Service**: persiste session state, thread state, continuidad de conversaciones y contexto de interaccion.
52. **Tool Registry and Execution Service**: registra herramientas, comandos y modos de ejecucion disponibles para chatbots, agentes y workflows LLM.
53. **Agent Runtime and Chatbot Orchestration Service**: coordina agentes conversacionales, tool calls, consultas a objetos, acciones y pasos con humano en el loop.
54. **AI Evaluation and Regression Service**: define suites de prueba, casos y metricas para prompts, agentes, chatbots y regresiones de calidad.
55. **Document Intelligence Service**: procesa documentos con OCR, layout-aware OCR o VLMs para extraer estructura, texto y entidades.
56. **AI Application Generation Service**: genera aplicaciones a partir de lenguaje natural, orquestando ontologia, diseño, frontend, seed data y despliegue guiado.

### Analitica, workflows y aplicaciones operacionales

57. **Tabular Analysis Service**: habilita analisis tabular persistente a gran escala al estilo Contour.
58. **Ontology Exploratory Analysis Service**: soporta exploracion visual sobre ontology, object sets, links, maps y writeback al estilo Insight.
59. **Ontology and Time-Series Analytics Service**: ejecuta dashboards, analitica sobre ontologia y series temporales al estilo Quiver.
60. **SQL and BI Gateway Service**: expone consultas, conectividad SQL, ODBC, JDBC y compatibilidad con herramientas BI externas.
61. **Interactive Code Analysis and Notebook Runtime Service**: ejecuta workloads interactivos de Python, R o SQL y experiencias tipo Code Workbook.
62. **Document Reporting Service**: soporta artefactos narrativos, reporting colaborativo y documentos operacionales tipo Notepad.
63. **Spreadsheet Computation Service**: soporta hojas de calculo colaborativas, formulas y writeback tipo Fusion.
64. **Analytical Reusable Logic Service**: publica expresiones guardadas, reusable logic, visual functions y plantillas analiticas compartidas.
65. **Workflow Automation Service**: ejecuta automatizaciones basadas en condiciones y efectos, con reglas continuas o programadas sobre estados de negocio.
66. **Automation Operations Control Plane Service**: visualiza colas, estados, dependencias, liveness, retries y ejecucion por objeto de automatizaciones.
67. **Workflow Lineage and Trace Service**: proporciona run history, trazas, logs y proveniencia de workflows a traves de funciones, acciones, modelos y apps.
68. **Application Composition Service**: sirve como backend de composicion para experiencias tipo Workshop o Slate, gestionando vistas, estado, bindings y eventos.
69. **Workspace and Application Curation Service**: administra workspaces, modulos, home pages, promoted apps y portales curados tipo Carbon y Applications Portal.
70. **Custom Widget Registry and Host Bridge Service**: versiona widget sets, widgets, contratos de parametros/eventos y su integracion segura con el host.
71. **Scenario Simulation Service**: crea forks inmutables de la ontologia para escenarios what-if basados en acciones y modelos.
72. **Solution Design and Architecture Knowledge Service**: almacena diagramas, patrones y conocimiento arquitectonico enlazado a recursos de plataforma.

### Developer platform, extensibilidad y observabilidad

73. **Developer Console and Application Control Plane Service**: administra aplicaciones, scopes, subdominios, releases y configuracion de apps custom.
74. **SDK Generation and Publication Service**: genera, publica y versiona SDKs y contratos OpenAPI a partir de la ontologia.
75. **Managed Workspace Orchestration Service**: aprovisiona entornos de desarrollo, perfiles, branches, dataset aliases y resolucion de contexto para builders.
76. **Custom Endpoints Publishing and Gateway Service**: publica endpoint sets versionados y remapea HTTP hacia acciones, funciones u otros backends.
77. **MCP Orchestration and Exposure Service**: expone herramientas MCP internas y ontologicas para agentes, apps y consumidores externos.
78. **Global Branch Orchestration Service**: gobierna branching transversal sobre repos, pipelines, ontologia, acciones y aplicaciones.
79. **Compute Modules Control Plane Service**: administra lifecycle, despliegue, replicas, diagnostico y configuracion de compute modules.
80. **Compute Modules Runtime Service**: ejecuta modulos de computo arbitrarios bajo identidad de plataforma, con escalado y metricas integradas.
81. **Monitoring Rules and Scope Engine Service**: administra monitores, scopes, severidades, suscriptores y reglas de observabilidad a escala.
82. **Health Check Evaluation Service**: ejecuta checks de salud con semanticas y scheduling propios sobre recursos y workflows.
83. **Execution Observability Service**: centraliza run history, log search, tracing distribuido y depuracion de ejecuciones.
84. **Telemetry Governance and Export Service**: gobierna permisos sobre telemetria y exporta logs, metricas y eventos a Foundry o sistemas externos.
85. **Code Security Scanning Service**: ejecuta analisis estatico integrado en CI para vulnerabilidades, code smells y politicas de calidad.

## Lectura arquitectonica

Si quisiera construir una plataforma inspirada en esta documentacion, estos 85 servicios serian mi mapa definitivo de bounded contexts. No los desplegaria todos por separado desde el primer dia.

Una forma pragmatica de arrancar seria agruparlos en siete macro dominios de despliegue:

1. `platform-security-core`: servicios 1 a 17.
2. `data-platform`: servicios 18 a 31.
3. `ontology-core`: servicios 32 a 38.
4. `model-ml-platform`: servicios 39 a 46.
5. `aip-platform`: servicios 47 a 56.
6. `analytics-workflow-apps`: servicios 57 a 72.
7. `developer-observability-platform`: servicios 73 a 85.

La recomendacion practica es usar esta lista como mapa de responsabilidades, no como una obligacion de tener 85 despliegues independientes. Lo correcto seria empezar con menos unidades de despliegue, medir acoplamiento, carga y ownership de equipo, y extraer microservicios solo cuando haya una razon operativa clara.
