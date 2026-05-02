# Auditoría No-SPOF + Autoría de Arquitectura de Referencia

> Documento técnico unificado.
> Parte I: Auditoría No-SPOF de OpenFoundry (estado actual, evidencias, gaps).
> Parte II: Autoría de arquitectura de referencia (objetivo, ADRs, escenarios, roadmap).
>
> Audiencia: comité técnico, CTO, SRE Lead, Security Architect, Platform Lead.
> Tono: principal architect / staff SRE / auditor independiente. Sin diplomacia innecesaria.

---

# PARTE I — AUDITORÍA NO-SPOF

## 1. Veredicto ejecutivo

- **Estado global:** **PARTIALLY COMPLIANT** — Architectural ambition exceeds operational maturity.
- **Puntuación global:** **62 / 100**.
  - Diseño y descomposición: 78/100.
  - Resiliencia stateful (HA real): 70/100.
  - Seguridad zero-trust: 48/100.
  - Operabilidad y delivery: 45/100.
  - Recuperación ante desastres: 55/100.
  - Coherencia entre intención declarada y código desplegable: 60/100.

### Top 10 hallazgos (resumen)

1. **GitOps ausente.** No hay ArgoCD/Flux. El estado se aplica con `helm upgrade` manual y módulos Terraform sueltos. Drift inevitable a medio plazo.
2. **mTLS no existe entre servicios.** Solo NetworkPolicy/CiliumNetworkPolicy a nivel L3/L4. Zero-trust este-oeste declarado pero no implementado.
3. **Secret management primitivo.** Vault, External Secrets Operator y Sealed Secrets están **mencionados** pero **no integrados**. Las contraseñas viven en `Secret` de Kubernetes con placeholders `change-me` en commits.
4. **Identity federation casero.** No se usa Keycloak. Hay un servicio `identity-federation-service` propio sin evidencia de hardening (token revocation, key rotation, JWKS rotation, MFA, SCIM).
5. **Workflow engine casero.** `workflow-automation-service` reimplementa scheduler + orquestación con Postgres + crate `cron`. Sin Temporal/Argo Workflows. Idempotencia, retries con backoff y deduplicación están por demostrar.
6. **No hay routing a réplicas Postgres.** CNPG provisiona endpoints `-ro` y `-r`, pero ningún servicio los usa. Toda la carga de lectura cae sobre el primario, anulando 2/3 de la capacidad provisionada.
7. **84 servicios y 71 clusters Postgres en un solo Helm release.** El blast radius de un `helm upgrade` malo es la plataforma entera. No hay separación entre release del control plane y release del data/workload plane.
8. **CI/CD construye solo ~6 imágenes.** Las otras ~88 imágenes (`ghcr.io/open-foundry/*:latest`) **no existen publicadas**. La promesa de `helm install` es por ahora vaporware salvo build local.
9. **OTEL collector ausente.** Las dependencias `opentelemetry` están en Cargo pero no hay collector ni backend (Tempo/Jaeger). Solo hay métricas (Prometheus) y logs (Loki sin verificar). Tracing distribuido no demostrado.
10. **Anti-patrón de descomposición.** 84 microservicios es excesivo para el volumen real de dominios. Hay servicios casi vacíos (`widget-registry`, `health-check-service`) y agrupaciones forzadas que copian la nomenclatura de Foundry sin un dominio Bounded Context detrás.

### Top 5 riesgos de caída sistémica

| # | Riesgo | Probabilidad | Impacto | Blast radius |
|---|---|---|---|---|
| R1 | Pérdida de quorum en CNPG operator (instalación única) | Baja | Crítico | Todos los 71 clusters Postgres se vuelven read-only o pierden failover |
| R2 | Saturación del `edge-gateway-service` (único punto de entrada lógico) | Media | Crítico | 100% de tráfico externo cae hasta scale-up |
| R3 | Fallo del `identity-federation-service` o de su Postgres | Media | Crítico | Bloqueo de autenticación → plataforma inaccesible |
| R4 | Migración SQLx in-process que falla en startup tras `helm upgrade` | Media | Alto | Servicio queda en CrashLoopBackOff sin rollback automático |
| R5 | Ceph perdido (cluster Rook degradado) | Baja | Crítico | Iceberg + WAL Postgres + backups → pérdida de datos persistentes |

### Top 5 decisiones correctas ya presentes

1. **CloudNativePG con failover automático** (3 instancias, `minSyncReplicas: 1`, WAL archiving a S3/Ceph).
2. **Strimzi Kafka KRaft** (3 brokers, RF=3, `min.insync.replicas=2`, sin ZooKeeper).
3. **Rook Ceph correctamente dimensionado** (5 MONs con quorum 3, 2 MGRs, EC 8+3 para Iceberg, failureDomain=zone).
4. **CiliumNetworkPolicy default-deny en Kafka** y NetworkPolicy por servicio en chart Helm.
5. **PDB y topologySpreadConstraints en chart base** + `requiredDuringScheduling` anti-affinity para Vespa y MONs Ceph.

---

## 2. Arquitectura inferida

### Descripción narrativa

OpenFoundry replica conceptualmente la separación de planos de Palantir Foundry pero **con un grado de fragmentación excesivo**. La arquitectura efectiva observada consiste en:

- **Edge plane:** un único `edge-gateway-service` (Rust, axum+tower) actúa como API gateway, rate limiter (Valkey) y router lógico. No hay sidecar de mesh ni mTLS; la identidad de cliente viaja como JWT emitido por `identity-federation-service`.
- **Control plane:** 84 microservicios Rust comunicándose por HTTP REST (axum) y, en algunos casos, gRPC (tonic). El descubrimiento es por **DNS de Kubernetes con URLs hardcoded en config TOML** del gateway. No hay service mesh, ni service registry, ni sidecar.
- **Data plane:** Iceberg (vía Lakekeeper REST catalog) sobre Ceph Object Gateway (Rook). Datasets grandes en parquet/Iceberg, metadatos en Postgres por servicio.
- **Event backbone:** Kafka (Strimzi KRaft) como dorsal de eventos. Por ahora **infrautilizado**: el audit interno (`docs/architecture/bus-audit.md`) confirma que ninguno de los servicios produce/consume topics activos en `event-bus-data`. NATS aparece en compose pero no en Helm.
- **Semantic plane:** 9 servicios `ontology-*` con bases Postgres independientes. Lecturas y escrituras compartiendo el mismo store. Sin write/read split. Vespa como motor de búsqueda hibrida (BM25 + ANN), opcional y desactivado por defecto en algunos overlays.
- **Security plane:** identity-federation-service (broker custom), policy-decision-service ausente, OPA/OpenFGA ausente, audit-compliance-service presente pero sin sumidero verificado.
- **Observability plane:** Prometheus Operator + Grafana + reglas/dashboards custom. **Sin OTEL collector ni backend de tracing.**
- **Storage para metadatos:** **71 clusters CloudNativePG independientes** — uno por bounded context. Anti-patrón de operabilidad (ver §10).

### Diagrama ASCII de la arquitectura **real** observada

```
                                  ┌──────────────────────────────────┐
                                  │   Usuarios + apps externas       │
                                  └────────────────┬─────────────────┘
                                                   │ HTTPS
                                          (Traefik/k3s ingress)
                                                   │
                                  ┌────────────────▼─────────────────┐
                                  │  edge-gateway-service (1 ingress)│
                                  │  axum + JWT + rate limit (Valkey)│
                                  └────────────────┬─────────────────┘
                                                   │ HTTP DNS resolución directa
              ┌────────────────────────────────────┼────────────────────────────────────┐
              │                                    │                                    │
  ┌───────────▼──────────┐         ┌───────────────▼──────────────┐         ┌───────────▼──────────┐
  │ identity-federation  │         │ ontology-* (9 servicios)     │         │ pipeline-* (3)       │
  │ + auth y sesión      │         │ definition / query / actions │         │ authoring / build /  │
  │ (custom broker)      │         │ funnel / functions / etc.    │         │ schedule             │
  └───────────┬──────────┘         └───────────┬──────────────────┘         └───────────┬──────────┘
              │                                │                                        │
              └────────────────────────────────┴────────────────────────────────────────┘
                                               │
              ┌────────────────────────────────┴────────────────────────────────────────┐
              │                  + 70 microservicios adicionales                        │
              │  (sql-bi-gateway, report, marketplace, ml-*, dataset-*, audit, ...)     │
              └────────────────────────────────┬────────────────────────────────────────┘
                                               │
       ┌─────────────────────┬─────────────────┴──────────────────┬─────────────────────┐
       │                     │                                    │                     │
┌──────▼──────────┐  ┌───────▼────────┐  ┌────────────────────────▼───┐  ┌──────────────▼────┐
│ 71 CNPG         │  │ Strimzi Kafka  │  │ Rook Ceph                   │  │ Vespa (opcional)  │
│ Postgres x 3    │  │ KRaft (3 brk)  │  │  - 5 MON / 2 MGR            │  │ 3 cs / 2 c / 3 ct │
│ (~213 instancias)│  │ RF=3 ISR=2    │  │  - rbd-fast x3              │  │ redundancy=2      │
│ wal_level=logical│  │ topics: idle  │  │  - EC 8+3 (Iceberg)         │  │ desactivado prod  │
└─────────────────┘  └────────────────┘  └─────────────────────────────┘  └───────────────────┘
                                               │
                                       ┌───────▼────────┐
                                       │ Lakekeeper REST│  (Iceberg catalog)
                                       │ 3 réplicas     │
                                       └────────────────┘

Observabilidad: Prometheus Operator + Grafana (sin OTEL/Tempo/Loki verificable).
Secrets: Kubernetes Secrets con CNPG-managed `<svc>-pg-app`. No Vault, no ESO.
GitOps: NO. Helm + Terraform aplicados imperativamente.
Mesh / mTLS: NO. Solo NetworkPolicy.
```

### Lista de componentes (mínima)

- 84 servicios Rust + 1 frontend SvelteKit.
- 71 clusters Postgres CNPG.
- 1 cluster Kafka Strimzi (KRaft).
- 1 cluster Ceph (Rook).
- 1 catálogo Iceberg (Lakekeeper).
- 1 Vespa (opcional).
- 1 Valkey, 1 NATS (solo en compose, no en Helm).
- Prometheus Operator + Grafana.

### Dependencias principales

- Todos los servicios → `edge-gateway-service` (hop entrante).
- Todos los servicios → `identity-federation-service` (validación JWT).
- Servicios con estado → su CNPG cluster.
- Pipeline / dataset → Lakekeeper + Ceph S3.
- Búsqueda → Vespa (cuando habilitado).
- Lineage → Kafka data plane (no activo todavía).

---

## 3. Scorecard por dominio

| Dominio | Estado | /100 | Evidencia | Riesgo principal | Observación |
|---|---|---|---|---|---|
| Ingress / gateway | PARTIAL | 55 | `templates/ingress.yaml` (1 Ingress global) | SPOF lógico de tráfico externo | Falta plan de scale + canary del gateway |
| Identity | PARTIAL | 50 | `services/identity-federation-service/*` | Implementación custom no auditada | Sustituir o cubrir con Keycloak + OPA |
| Metadata / catalog | PRESENT | 75 | `data-asset-catalog-service` + Postgres CNPG | Acoplamiento DB-per-service excesivo | Consolidar en menos clusters |
| Ontology schema | PRESENT | 70 | `ontology-definition-service` + migrations sqlx | Sin write/read split | Introducir read model materializado |
| Ontology write/index | PARTIAL | 55 | `ontology-actions-service`, `ontology-funnel-service` | Falta indexer asíncrono real | Pipeline write→Kafka→indexer está diseñado pero no activo |
| Ontology read/query | PARTIAL | 55 | `ontology-query-service` | Sin cache + sin secondary index | Vespa desactivado por defecto |
| Actions / workflows | PARTIAL | 50 | `workflow-automation-service` (custom) | Sin idempotencia ni durabilidad probada | Migrar a Temporal |
| Storage (object) | PRESENT | 85 | `infra/k8s/rook/cluster.yaml` (5 MON, EC 8+3) | Operación Ceph no trivial | Bien diseñado |
| Storage (relational) | PARTIAL | 60 | 71 clusters CNPG | Sobre-fragmentación, complejidad operativa | Consolidar a ≤8 clusters por dominio |
| Event backbone | PARTIAL | 60 | Kafka HA correcto, pero topics inactivos | Backbone no usado en producción | Activar lineage + CDC |
| Search | PARTIAL | 50 | Vespa subchart desactivado | Búsqueda degradada cuando se desactiva | Hacer obligatorio en prod |
| Security | PARTIAL | 45 | NetworkPolicy + CNPG Secret | Sin mTLS, sin Vault, sin OPA | Riesgo serio para enterprise |
| Observability | PARTIAL | 55 | Prometheus + Grafana | Sin tracing distribuido | OTEL collector ausente |
| Delivery | PARTIAL | 40 | GH Actions limitado | Sin GitOps, imágenes no publicadas para 88 servicios | Gap operacional crítico |
| Disaster Recovery | PARTIAL | 55 | CNPG ScheduledBackup + barman | Restore no probado | Sin Velero, sin PITR runbook ejecutado |

---

## 4. Matriz de SPOF

| Componente | ¿SPOF? | Evidencia | Consecuencia | Mitigación existente | Mitigación recomendada | Severidad |
|---|---|---|---|---|---|---|
| `edge-gateway-service` | Sí (lógico) | Único Ingress, único tipo de pod | Pérdida de entrada externa al saturarse | HPA + 2-3 réplicas | 2 IngressControllers + PDB+HPA agresivo + canary | Alta |
| `identity-federation-service` | Sí (funcional) | Sin alternativa de auth | Bloqueo total de autenticación | 2 réplicas | 3+ réplicas, externalizar a Keycloak HA | Crítica |
| `identity-federation-pg` | Parcial | CNPG 3 réplicas, primary único | RW indisponible durante failover (~30 s) | CNPG failover | Pgbouncer pooler + connection retry en cliente | Media |
| CNPG operator | Sí | Single deployment del operator | Sin reconciliación si cae operator | — | 2 réplicas + leader election (default) | Alta |
| Lakekeeper | No | 3 réplicas configuradas | — | Helm chart oficial | OK | — |
| Strimzi Kafka cluster | No | 3 brokers KRaft, RF=3 | — | min.insync=2 | OK | — |
| Strimzi operator | Sí | Single deployment | Sin reconciliación si cae | — | 2 réplicas + leader election | Media |
| Rook MON quorum | No | 5 MONs anti-affinity host | Tolera 2 fallos | — | OK | — |
| Rook MGR | No | 2 MGRs | Tolera 1 fallo | — | OK | — |
| Vespa configservers | No | 3 configservers anti-affinity | Tolera 1 fallo | — | OK (cuando habilitado) | — |
| Vespa content nodes | No | 3 content, redundancy=2 | Tolera 1 fallo | — | OK | — |
| `workflow-automation-service` | Sí (funcional) | Custom scheduler, leader desconocido | Trabajos duplicados o perdidos | — | Migrar a Temporal o introducir leader election explícita | Alta |
| `pipeline-schedule-service` | Sí (funcional) | Cron scheduler en proceso | Si dos réplicas → doble disparo | replicas=1 (anti-patrón) | Cron como CronJob k8s o Temporal Schedules | Alta |
| Valkey (cache rate limit) | Sí | 1 instancia en compose | Rate limit cae a fail-open o fail-closed | — | Sentinel/Cluster, o usar EnvoyFilter | Media |
| NATS (si se activa) | Sí | 1 instancia en compose | Eventos perdidos | — | NATS cluster ≥3, JetStream con replicas=3 | Media |
| Prometheus (si single) | No demostrado | No hay manifest claro | Pérdida métricas | — | Thanos/Mimir o Prometheus HA pair | Media |
| Grafana | Probable SPOF | No demostrado | Sin dashboards | — | 2 réplicas + DB externa | Baja |
| Helm release único | Sí (operativo) | 1 release con 84 deployments | `helm upgrade` malo afecta a todos | — | Separar en al menos 5 releases por plano | Alta |
| Image registry (`ghcr.io`) | Externo | — | Pull failure si caído | imagePullPolicy IfNotPresent | Mirror local + air-gap secondary | Media |

---

## 5. Matriz servicio por servicio (resumen agrupado)

> Por brevedad agrupo los 85 servicios en categorías. Detalle completo en valores Helm.

| Grupo | Servicios | Stateful/Stateless | Réplicas actuales (default values) | Recomendado | Store | Riesgo HA | Observación |
|---|---|---|---|---|---|---|---|
| Edge | edge-gateway-service | Stateless | 2 | 3 (prod) | Valkey (rate limit) | Alta | Único punto de entrada; saturación crítica |
| Identity | identity-federation-service | Stateful (sesión) | 2 | 3 + Keycloak externo | Postgres | Crítica | Custom broker; auditar JWT/JWKS rotation |
| Catalog & metadata | data-asset-catalog, dataset-versioning, lineage, lineage-deletion, cdc-metadata | Stateless+ext store | 2 | 3 | Postgres + Iceberg | Media | OK con DB HA |
| Ontology | ontology-definition / query / actions / funnel / functions / security / exploratory / timeseries | Stateful (DB-per-svc) | 2 | 3 | Postgres + Vespa | Alta | Sin write/read split; latencia de query no acotada |
| Pipeline | pipeline-authoring / build / schedule | Mixto (schedule = stateful singleton) | 2 (ojo: schedule debería ser 1 con leader election) | 1 + leader election o Temporal | Postgres | Alta | Doble disparo si 2 replicas sin leader |
| Workflow / actions | workflow-automation, approvals, automation-operations | Stateful en DB | 2 | 3 | Postgres | Alta | Idempotencia no garantizada |
| ML | ml-experiments, model-{adapter,catalog,deployment,evaluation,inference-history,lifecycle,serving} | Stateless+modelos en S3 | 2 | 2-3 | Postgres + Ceph | Media | OK |
| AI/agents | agent-runtime, ai-application-generation, ai-evaluation, llm-catalog, prompt-workflow, mcp-orchestration, retrieval-context, knowledge-index, conversation-state | Mixto | 2 | 3 | Postgres + Vespa + Ceph | Media | OK con cache |
| Security/governance | authorization-policy, audit-compliance, security-governance, network-boundary, oauth-integration, cipher, sds, session-governance, checkpoints-purpose, custom-endpoints, telemetry-governance | Stateful (audit) | 2 | 3 | Postgres + Kafka audit | Alta | Sumidero de audit no demostrado |
| Marketplace / nexus | marketplace, marketplace-catalog, nexus, federation-product-exchange, product-distribution, application-curation, application-composition, app-builder, solution-design, widget-registry, tool-registry | Stateless | 2 | 2 | Postgres | Baja | OK |
| Search/query plane | sql-bi-gateway, sql-warehousing, virtual-table, spreadsheet-computation, tabular-analysis, scenario-simulation, report, document-reporting, document-intelligence, geospatial-intelligence, time-series-data | Mixto | 2 | 3 | Postgres + Iceberg + Vespa | Alta | Sin -ro routing; carga al primario |
| Compute modules | compute-modules-control-plane, compute-modules-runtime, notebook-runtime, managed-workspace, developer-console, sdk-generation, code-repository-review, code-security-scanning | Stateless+jobs | 2 | 3 | Postgres + Ceph | Media | Necesitan k8s Jobs propios |
| Connectors / ingestion | connector-management, ingestion-replication, event-streaming, entity-resolution, dataset-quality, object-database, monitoring-rules, execution-observability, workflow-trace, global-branch, tenancy-organizations, model-evaluation, notification-alerting, health-check | Mixto | 2 | 3 | Postgres + Kafka | Media | OK |
| Web | apps/web (SvelteKit) | Stateless SSR | 2 | 3 | — | Baja | OK |

---

## 6. Hallazgos detallados

> 24 hallazgos numerados. Severidad: C=Critical, H=High, M=Medium, L=Low.

### F-001 [C] Imágenes de 88 servicios no se construyen ni publican

- **Evidencia:** `.github/workflows/docker-publish.yml` matrix incluye ~6 servicios. `services/*/Dockerfile` existen 94. `values.yaml` referencia `ghcr.io/open-foundry/<svc>:latest` para los 84.
- **Por qué importa:** El `helm install` no funciona contra un cluster limpio sin un build local exhaustivo. La promesa de plataforma desplegable es ficción operativa.
- **Fallo posible:** `ImagePullBackOff` masivo en producción.
- **Cómo arreglar:** Generar matrix dinámica leyendo `services/*/Dockerfile`, publicar todas las imágenes en cada release con tag inmutable (`sha-<short>`), evitar `:latest` en `values.yaml`.

### F-002 [C] Sin GitOps

- **Evidencia:** No hay `.argocd/`, `gitops/`, ni `Application` CRDs. Despliegue manual con `helm upgrade`.
- **Por qué importa:** Drift inevitable. Cero auditoría de cambio. Rollback ad-hoc.
- **Fallo posible:** Estado del cluster diverge del git. Cambios manuales no documentados. Imposible reproducir un entorno.
- **Cómo arreglar:** Adoptar **ArgoCD** con `ApplicationSet` por entorno; bootstrap mediante un único `Application` raíz (app-of-apps).

### F-003 [C] Identity federation custom sin auditoría externa

- **Evidencia:** `services/identity-federation-service/` con migrations sqlx propias, sin Keycloak ni IdP externo declarado.
- **Por qué importa:** Implementaciones de OAuth2/OIDC propietarias son una fuente común de CVEs. Sin SCIM, MFA gestionado, recuperación de credenciales, JWKS rotation auditada.
- **Fallo posible:** Bypass de autenticación, ataques de replay, JWT no revocables.
- **Cómo arreglar:** Sustituir core por **Keycloak** (Quarkus, HA, RBAC nativo). Mantener `identity-federation-service` solo como capa de federación (mapping, token enrichment) o eliminarlo.

### F-004 [C] Sin mTLS este-oeste

- **Evidencia:** No hay Istio/Linkerd/Cilium TLS transparente. Tráfico HTTP entre servicios.
- **Por qué importa:** Zero-trust es un objetivo declarado. Hoy un pod comprometido puede impersonar a cualquier servicio.
- **Cómo arreglar:** **Linkerd** (más simple) o Istio Ambient. Identidad SPIFFE por ServiceAccount. Política mTLS estricta por namespace.

### F-005 [H] PDB con `minAvailable: 1` bajo HPA `minReplicas: 2`

- **Evidencia:** `templates/poddisruptionbudget.yaml` + `values.yaml` línea 53.
- **Por qué importa:** Drain de un nodo deja 1 pod. Si la métrica HPA tarda, se servirá el doble de tráfico con mitad de capacidad.
- **Cómo arreglar:** `maxUnavailable: 1` y `minReplicas >= 3` para servicios críticos. PDB derivado de `minReplicas`.

### F-006 [H] No hay routing a réplicas Postgres `-ro`

- **Evidencia:** Todas las `DATABASE_URL` apuntan a `<cluster>-pg-app` (que resuelve a `-rw`).
- **Por qué importa:** La capacidad de las réplicas se desperdicia. Carga de lectura cae sobre el primario, multiplicando tiempos de respuesta y agotando conexiones.
- **Cómo arreglar:** Inyectar `DATABASE_READ_URL` apuntando a `<cluster>-ro`. Modificar capa sqlx para distinguir consultas SELECT de mutaciones (patrón "primary on demand" para mutaciones, default a réplica). Usar **PgBouncer** (cnpg-poolers) para reducir conexiones.

### F-007 [H] Workflow engine homemade sin garantías de durabilidad

- **Evidencia:** `services/workflow-automation-service/src/domain/executor.rs`, `services/pipeline-schedule-service/src/main.rs` con ticker en proceso.
- **Por qué importa:** Sin idempotencia, deduplicación ni retries con backoff exponencial demostrados. Si dos réplicas del scheduler se ejecutan simultáneamente: doble disparo. Si una réplica única cae: pérdida de jobs.
- **Cómo arreglar:** Migrar a **Temporal** (HA, idempotencia, retries, signals, queries). Alternativa: Argo Workflows para CIs y jobs DAG.

### F-008 [H] Sobre-fragmentación: 71 clusters Postgres

- **Evidencia:** `infra/k8s/cnpg/clusters/*.yaml` (71 archivos), 213 instancias Postgres.
- **Por qué importa:** Operabilidad imposible para equipo pequeño. 213 procesos a actualizar, monitorizar, respaldar. Coste de RAM mínimo ~55 GB solo de Postgres. Los "límites de bounded context" no requieren un cluster físico distinto.
- **Cómo arreglar:** Consolidar a **8 clusters** lógicos por **plano**:
  1. `pg-identity` (auth, sesión)
  2. `pg-catalog` (data-asset-catalog, dataset-versioning, lineage, cdc-metadata)
  3. `pg-ontology` (ontology-*)
  4. `pg-workflow` (pipeline-*, workflow-*, approvals, automation)
  5. `pg-ml-ai` (ml-*, ai-*, model-*, agent-*)
  6. `pg-marketplace` (marketplace, nexus, app-builder, etc.)
  7. `pg-governance` (authorization-policy, audit, security, telemetry)
  8. `pg-platform` (connectors, ingestion, monitoring, health, tenancy)
  Aislamiento lógico vía **schemas separados** dentro del mismo cluster. CNPG con 3 instancias por cluster = 24 procesos en lugar de 213.

### F-009 [H] Vespa desactivado por defecto sin alternativa de búsqueda

- **Evidencia:** `charts/vespa` `enabled: false` en algunos overlays.
- **Por qué importa:** Servicios `ontology-query`, `knowledge-index`, `retrieval-context` requieren búsqueda full-text/vector. Sin Vespa caen a Postgres pg_trgm, que no es production-grade para corpora >100k docs.
- **Cómo arreglar:** Hacer Vespa obligatorio en `values-prod.yaml`. Para dev, sustituir por **OpenSearch single-node** o **Meilisearch** (perfil demo).

### F-010 [H] Sin OTEL collector ni tracing distribuido

- **Evidencia:** Cargo deps `opentelemetry`, `opentelemetry-otlp` presentes en workspace pero sin collector ni Tempo/Jaeger.
- **Por qué importa:** Imposible diagnosticar latencias multi-hop entre 84 servicios.
- **Cómo arreglar:** Desplegar **OpenTelemetry Collector** (DaemonSet) + **Tempo** (con backend Ceph S3). Instrumentación obligatoria de spans `tower-http` en cada servicio.

### F-011 [H] CPU limits demasiado ajustados → throttling

- **Evidencia:** `values.yaml` default `cpu: "1"` limit con `cpu: 200m` request.
- **Por qué importa:** Servicios Rust con tareas async y compresión gzip pueden picar CPU. Throttling causa P99 latencias erráticas.
- **Cómo arreglar:** Eliminar CPU limit (mantener request). Confiar en HPA + topology spread. Usar `requests` honestos.

### F-012 [H] Migrations en proceso, no en init container

- **Evidencia:** `services/*/main.rs` ejecuta `sqlx::migrate!` antes de bind. Documentado en READMEs.
- **Por qué importa:** Si la migration falla en producción, el pod crashea en loop sin posibilidad de rollback automatizado del schema. Ninguna réplica puede arrancar hasta que la migration funcione.
- **Cómo arreglar:** Mover migrations a **k8s Job** con Helm hook `pre-upgrade`. Validar con `--dry-run` previo. Soportar **migrations expand-and-contract** para zero downtime.

### F-013 [H] Sin Vault / External Secrets

- **Evidencia:** `Secret` k8s con placeholders `change-me`. Sin `ExternalSecret`, sin SealedSecret, sin SOPS.
- **Por qué importa:** Compromiso de un Secret = exposición permanente. Sin rotación auditable. Air-gap difícil.
- **Cómo arreglar:** Desplegar **External Secrets Operator** + **Vault** (o cloud-native: AWS Secrets Manager / GCP Secret Manager). Definir `ClusterSecretStore`. Rotación cada 90 días.

### F-014 [H] Helm release único = blast radius máximo

- **Evidencia:** Un solo Chart `open-foundry` desplegando 84 deployments.
- **Por qué importa:** `helm upgrade` malo o `--atomic` con rollback puede tirar la plataforma entera. Imposible promover servicios independientemente.
- **Cómo arreglar:** Separar en **5 releases** alineados con planos:
  - `open-foundry-platform` (gateway, identity, audit, secrets)
  - `open-foundry-data` (catalog, lineage, ingestion, connectors)
  - `open-foundry-ontology` (ontology-*, search)
  - `open-foundry-workflow` (pipeline-*, workflow-*, approvals)
  - `open-foundry-ai` (ml-*, ai-*, agent-*)

### F-015 [H] Sin canary / blue-green

- **Evidencia:** RollingUpdate default. No hay Argo Rollouts, Flagger.
- **Por qué importa:** Bug introducido = se propaga a 100% del tráfico durante el rollout.
- **Cómo arreglar:** **Argo Rollouts** con análisis basado en Prometheus (latencia P95, error rate, saturation).

### F-016 [M] ServiceAccount única para todos los servicios

- **Evidencia:** `templates/serviceaccount.yaml` crea una sola SA para el chart.
- **Por qué importa:** Imposibilita workload identity por servicio (AWS IAM/GCP WI), ni RBAC granular contra k8s API.
- **Cómo arreglar:** Una SA por servicio. Workload identity vinculada a la SA. RBAC mínimo (probablemente ninguno para la mayoría).

### F-017 [M] readOnlyRootFilesystem = false en todos los servicios

- **Evidencia:** `values.yaml` línea 31.
- **Por qué importa:** Pod comprometido puede escribir binarios en `/`. Pierdes una capa defensiva.
- **Cómo arreglar:** `readOnlyRootFilesystem: true` global con override por servicio que necesita escribir (Vespa, notebook-runtime). Mount `emptyDir` en `/tmp`, `/var/run`.

### F-018 [M] Single Ingress por gateway sin segundo IngressController

- **Evidencia:** `templates/ingress.yaml` un único Ingress.
- **Por qué importa:** Si Traefik (k3s) falla, plataforma offline.
- **Cómo arreglar:** Dos IngressControllers (Traefik + nginx-ingress) o usar **cert-manager + ExternalDNS** sobre LB cloud HA.

### F-019 [M] No image signing / SBOM

- **Evidencia:** Workflows no usan cosign/syft.
- **Por qué importa:** Supply chain. Cualquier compromiso de GHA = inyección de imagen no detectable.
- **Cómo arreglar:** **cosign sign --keyless** + **syft attest** + **Kyverno policy** verificando firmas en admisión.

### F-020 [M] Ausencia de policy-as-code

- **Evidencia:** No hay OPA, OpenFGA, Cedar.
- **Por qué importa:** `authorization-policy-service` existe pero la lógica de policy es propietaria, no declarativa.
- **Cómo arreglar:** **OpenFGA** (mejor fit para ReBAC tipo Foundry) como plano de decisión. `authorization-policy-service` se vuelve adapter.

### F-021 [M] CronJob crítico (`pipeline-schedule-service`) corre como Deployment

- **Evidencia:** `pipeline-schedule-service` es un Deployment con tick interno.
- **Por qué importa:** Si replicas=2 sin leader election → doble dispatch. Si replicas=1 → SPOF.
- **Cómo arreglar:** Modelar como `CronJob` k8s o usar **Temporal Schedules** (HA nativo).

### F-022 [M] Audit trail sin sumidero verificado

- **Evidencia:** `audit-compliance-service` y `libs/audit-trail` existen, pero no hay topic Kafka activo ni sink WORM.
- **Por qué importa:** Cumplimiento (SOC2, ISO 27001) requiere logs de audit inmutables y replicados.
- **Cómo arreglar:** Definir topic Kafka `audit.events` con `cleanup.policy=delete` y retención larga + sink a Iceberg (immutable WORM).

### F-023 [L] Ausencia de chaos testing

- **Evidencia:** No hay Litmus, Chaos Mesh, ni script de Game Day.
- **Por qué importa:** No-SPOF declarado pero no probado.
- **Cómo arreglar:** **Chaos Mesh**. Game Day mensual: kill primary CNPG, drain node, partición Kafka, etc.

### F-024 [L] Documentación de DR no ejecutada

- **Evidencia:** `infra/runbooks/disaster-recovery.md` existe pero no hay log de simulacro.
- **Cómo arreglar:** Drill trimestral. Restore probado en cluster aislado.

---

## 7. Evaluación de resiliencia (escenarios)

| Escenario | Comportamiento esperado actual | Evidencia | Confianza | Brecha | Recomendación |
|---|---|---|---|---|---|
| Caída de 1 pod | Tráfico drenado, otro pod sirve | PDB+Service+probes | Alta | Ninguna | OK |
| Caída de 1 nodo | Pods reprograman; PDB respeta minAvailable=1 | topologySpread (soft) | Media | `whenUnsatisfiable: ScheduleAnyway` permite mala distribución | Cambiar a `DoNotSchedule` para servicios críticos |
| Rolling upgrade | RollingUpdate default; sin canary | Deployment standard | Media | Sin análisis automático | Argo Rollouts |
| Caída de AZ completa | Ceph zone-aware sobrevive; CNPG depende de PDB+spread | Ceph failureDomain=zone | Media | Servicios usan topologyKey=hostname, no zone | Añadir `topology.kubernetes.io/zone` a topologySpread |
| Pérdida temporal de Postgres primary | CNPG failover ~30 s; conexiones rotas | CNPG operator | Alta | Servicios no tienen retry + jitter probado | PgBouncer + retry policy |
| Pérdida temporal de Kafka | Productores buffer + retries; consumidores re-balance | Strimzi RF=3 | Alta | Topics inactivos (no drama hoy) | Verificar idempotency + acks=all en producers |
| OpenSearch / Vespa degradado | Vespa redundancy=2 tolera 1 nodo | Vespa subchart | Alta | Si Vespa desactivado → degradación severa de búsqueda | Hacer obligatorio en prod |
| Pérdida de object storage (Ceph) | EC 8+3 tolera 3 chunks, zone-aware | Rook lint | Alta | Ninguna | OK |
| Expiración de certificados | Sin cert-manager visible | — | Baja | Riesgo de expiración silenciosa | Desplegar cert-manager + alertas Prometheus |
| IdP caído | Plataforma inaccesible (sin fallback) | identity-federation-service | Baja | Cero alternativa | Cache JWT short-lived + degraded mode read-only |
| Partición de red | Servicios degradan; sin circuit breaker explícito | — | Baja | Falta tower middleware | Tower CB + tower-buffer |
| Deploy fallido | Helm rollback manual | — | Media | Sin --atomic | Usar Argo Rollouts con auto-rollback |
| Migración fallida | Pod CrashLoopBackOff bloqueando réplica | sqlx in-process | Baja | Sin pre-flight | Migrations como Job pre-upgrade |
| Pérdida de region | No soportado | — | N/A | No multi-region | Roadmap fase 4 |

---

## 8. Evaluación de seguridad

- **SSO/OIDC:** custom (`identity-federation-service`). **No demostrado** soporte de SAML, MFA gestionado, SCIM, social IdP. **Recomendación:** Keycloak.
- **Machine identity:** ServiceAccount única + workload identity opcional. **No SPIFFE/SPIRE.** **Recomendación:** Linkerd con identidades SPIFFE por SA.
- **mTLS:** **No.** **Recomendación:** Linkerd ambient.
- **Secrets:** k8s Secrets sin Vault. Placeholders en git. **Crítico.** **Recomendación:** ESO + Vault.
- **Network policies:** Bien (Cilium en Kafka, vanilla en chart). **Recomendación:** default-deny global y allow-list explícito.
- **Encryption at rest:** Ceph cifrado opcional. CNPG sin TDE. **Recomendación:** Ceph encryption + LUKS en PVCs.
- **Encryption in transit:** TLS terminado en Ingress. Interno HTTP plano. **Recomendación:** mTLS.
- **Audit trail:** servicio existe, sumidero no verificado. **Recomendación:** Kafka topic + Iceberg WORM.
- **Tenant isolation:** Solo a nivel aplicación (`tenancy-organizations-service`). Sin namespace por tenant ni network isolation. **Recomendación:** Namespace por tenant para tenants enterprise (multi-cluster opcional).
- **Supply chain:** sin signing, sin SBOM, sin Kyverno. **Recomendación:** cosign + syft + Kyverno + Renovate.

**Score seguridad: 48/100.**

---

## 9. Evaluación de operabilidad

- **Métricas:** Prometheus Operator + reglas + dashboards. **Bien.**
- **Logs:** No demostrado pipeline (Loki/Vector/Fluent Bit ausentes). **Gap.**
- **Traces:** Ausente. **Gap crítico.**
- **Alertas:** PrometheusRule presentes. **Bien.**
- **Runbooks:** `infra/runbooks/cnpg.md`, `disaster-recovery.md`. **Existen pero sin evidencia de drill.**
- **Backups:** CNPG ScheduledBackup. **Bien para Postgres.** **Velero ausente** (no PVCs ni recursos k8s respaldados).
- **Restore:** Documentado, no probado. **Gap.**
- **Capacity planning:** Sin documentar SLOs ni capacity model. **Gap.**

**Score operabilidad: 45/100.**

---

## 10. Desalineaciones objetivo vs implementación

| Objetivo declarado | Implementación real | Brecha |
|---|---|---|
| Zero-trust este-oeste | NetworkPolicy L3/L4 únicamente | Falta mTLS + identidad de workload |
| GitOps | helm/terraform manual | Falta Argo CD |
| Storage heterogéneo fit-for-purpose | 95% Postgres + Iceberg + Vespa opcional | Falta Cassandra/Scylla para estado operacional alto throughput; falta cache HA |
| Microservicios + bounded contexts | 84 servicios; muchos son stubs | Sobre-fragmentación; consolidar |
| Ontology read/write split | Mismo Postgres por servicio | No hay materialización ni indexer asíncrono |
| Lineage activo | Headers definidos, pipeline inactivo | Activar topics |
| Multi-cloud / sovereign / air-gap | values overlay existen | Sin probar; sin mirror registry |
| Multi-tenant fuerte | Tenancy a nivel app | Sin namespace por tenant |
| Auditoría inmutable | Servicio existe, sin sink WORM | Iceberg+Kafka |
| Observabilidad completa | Solo métricas | Falta tracing + logs centralizados |

---

## 11. Plan de remediación priorizado

| Prioridad | Acción | Beneficio | Dificultad | Equipo | Dependencias |
|---|---|---|---|---|---|
| P0 | Construir y publicar imágenes de los 84 servicios | Desbloquea `helm install` | Baja | Platform | GH Actions matrix dinámico |
| P0 | Consolidar a 8 clusters CNPG con schemas | -85% complejidad operativa Postgres | Media | DBA + Platform | Migrations + connection strings |
| P0 | Migrations como Job pre-upgrade | Evita CrashLoop por schema | Baja | Backend | Helm hooks |
| P0 | External Secrets Operator + Vault | Security baseline | Media | Security + Platform | Vault deployment |
| P0 | Argo CD con app-of-apps | GitOps + auditabilidad | Media | Platform | Repo gitops/ |
| P1 | Linkerd + mTLS + identidades por SA | Zero-trust real | Media | Security + Platform | SA por servicio |
| P1 | Sustituir identity-federation por Keycloak | Auditabilidad + features estándar | Alta | Backend + Security | Migración usuarios |
| P1 | Migrar workflow a Temporal | HA + idempotencia | Alta | Backend | SDK Rust |
| P1 | OTEL Collector + Tempo | Tracing distribuido | Media | Platform | Instrumentación |
| P1 | Argo Rollouts + análisis Prometheus | Canary auto | Media | Platform | Métricas SLO |
| P2 | Routing a `-ro` Postgres | -50% carga primario | Media | Backend | Capa sqlx |
| P2 | Vespa obligatorio en prod | Búsqueda real | Baja | Platform | — |
| P2 | OpenFGA como PDP | Policy-as-code | Alta | Security + Backend | Migración rules |
| P2 | Image signing (cosign) + Kyverno | Supply chain | Media | Platform + Security | — |
| P3 | Chaos Mesh + Game Days | Validación HA | Media | SRE | — |
| P3 | Multi-region active/passive (control plane) | DR enterprise | Alta | Platform + DBA | Topology |
| P3 | Tenancy por namespace | Aislamiento fuerte | Alta | Platform | Refactor |

---

## 12. ADRs recomendados (corregidos / nuevos)

### ADR-N01 — Consolidar metadatos a 8 clusters Postgres por plano

- **Status:** Proposed.
- **Context:** 71 clusters CNPG inviables operacionalmente.
- **Decision:** 8 clusters CNPG (3 instancias c/u), aislamiento por **schema** + **role**.
- **Consequences:** -85% complejidad operativa; algunos services comparten cluster pero mantienen aislamiento lógico; DR backups granulares por schema.

### ADR-N02 — Adoptar GitOps con Argo CD (app-of-apps)

- **Status:** Proposed.
- **Decision:** Repo `gitops/` con `ApplicationSet` por entorno; sync manual sólo en producción (auto-sync en dev/staging).

### ADR-N03 — Servicio de identidad: Keycloak HA + adapter local

- **Status:** Proposed (sustituye identity-federation custom).
- **Consequences:** Pierdes control fino, ganas auditabilidad, MFA, SCIM, OIDC compliance.

### ADR-N04 — Linkerd como service mesh (zero-trust)

- **Status:** Proposed.
- **Decision:** Linkerd 2 ambient. mTLS strict. Identidad SPIFFE.
- **Trade-off vs Istio:** menor complejidad operativa.

### ADR-N05 — Workflow engine externo: Temporal

- **Status:** Proposed.
- **Decision:** `workflow-automation-service` se vuelve adapter de Temporal Workflows. `pipeline-schedule-service` reemplazado por Temporal Schedules.

### ADR-N06 — Migrations como Job pre-upgrade con expand-and-contract

- **Status:** Proposed.
- **Decision:** Helm hook `pre-upgrade,pre-install` + Job. Servicios no migran in-process.

### ADR-N07 — Secrets gestionados por Vault + ESO

- **Status:** Proposed.
- **Decision:** Vault como source of truth. ExternalSecret CRDs. Rotación 90 d.

### ADR-N08 — Releases Helm separados por plano

- **Status:** Proposed.
- **Decision:** 5 charts: platform, data, ontology, workflow, ai.
- **Consequences:** blast radius reducido; promoción independiente.

### ADR-N09 — Routing read/write a Postgres

- **Status:** Proposed.
- **Decision:** `DATABASE_READ_URL` separado, capa sqlx con pool primario y pool réplica.

### ADR-N10 — Observabilidad: OTEL collector + Tempo + Loki + Mimir

- **Status:** Proposed.
- **Decision:** OTEL Collector DaemonSet → Tempo (traces) + Loki (logs) + Mimir (metrics HA).

### ADR-N11 — Policy-as-code con OpenFGA

- **Status:** Proposed.
- **Decision:** OpenFGA como PDP. authorization-policy-service como adapter. Modelado ReBAC.

### ADR-N12 — Supply chain: cosign + syft + Kyverno

- **Status:** Proposed.
- **Decision:** Imágenes firmadas con keyless OIDC. SBOM atestado. Kyverno admission.

---

## 13. Anexo técnico

- **Archivos inspeccionados (muestra):** `infra/k8s/helm/open-foundry/{Chart.yaml,values.yaml,values-prod.yaml,templates/*}`, `infra/k8s/cnpg/{operator,clusters,templates}`, `infra/k8s/strimzi/*`, `infra/k8s/rook/*`, `infra/k8s/lakekeeper/*`, `infra/k8s/vespa/*`, `infra/observability/*`, `infra/runbooks/*`, `services/edge-gateway-service/*`, `services/identity-federation-service/*`, `services/sql-bi-gateway-service/*`, `services/workflow-automation-service/*`, `services/pipeline-schedule-service/*`, `services/lineage-service/*`, `libs/event-bus-data/*`, `.github/workflows/*`, `compose.yaml`, `infra/docker-compose*.yml`.
- **Suposiciones:** El cluster destino tiene CNI Cilium o equivalente (según mention en network-policies). El operator CNPG y Strimzi ya instalados en el cluster destino (no incluidos en chart open-foundry).
- **Limitaciones:** No se verificó comportamiento runtime; auditoría es estática sobre código y manifests. No se inspeccionó CI history. No se verificaron tags de imágenes publicadas en `ghcr.io`.
- **Evidencia faltante:** Logs reales de un upgrade completo, drill de DR, traces, alertas activas, métricas SLO en producción.

---

# PARTE II — AUTORÍA DE ARQUITECTURA DE REFERENCIA

> Diseño objetivo No-SPOF para una plataforma OSS de "enterprise data operating system".
> Incluye trade-offs, ADRs, escenarios y roadmap.

## 1. Executive architecture statement (15 puntos)

1. La plataforma es un **conjunto cooperante de planos** (control, data, semantic, execution, security, observability), no un monolito ni una nube de microservicios sin estructura.
2. **No-SPOF es propiedad emergente** del diseño + operación, no un atributo de un solo componente; se valida por chaos engineering, no por aspiración.
3. **Storage fit-for-purpose** sobre tres ejes: object storage para datasets (Iceberg + Parquet sobre S3-compatible), Postgres HA para metadatos relacionales acotados, Cassandra/Scylla para estado operacional de alta tasa, Vespa para serving semántico, Kafka como espina dorsal de eventos. Cada uno con su modelo de consistencia.
4. **Stateless-first**: cada microservicio es stateless por defecto; el estado vive en stores especializados; los pocos servicios stateful se modelan como StatefulSet con coordinación explícita (leader election o Raft).
5. **Zero-trust este-oeste** vía service mesh (Linkerd ambient) con identidades SPIFFE y mTLS estricto.
6. **Identity centralizada** en Keycloak HA; políticas en OpenFGA (ReBAC tipo Foundry); audit trail inmutable en Iceberg.
7. **GitOps como única fuente de verdad operacional** mediante Argo CD app-of-apps + ApplicationSet por entorno y por plano.
8. **Progressive delivery** con Argo Rollouts + análisis automático sobre métricas SLO.
9. **Schema evolution** mediante expand-and-contract; nada de migraciones in-process.
10. **Ontology backend con write/read split**: writes a un cluster transaccional + indexación asíncrona vía Kafka → Vespa + materialización en Iceberg para análisis batch.
11. **Workflow engine externo** (Temporal) para todo lo que requiera durabilidad, retries, signals y schedules.
12. **Multi-tenancy fuerte**: namespace por tenant enterprise, RBAC + ABAC, network policies, aislamiento de quotas. Tenants pequeños comparten namespace con políticas OPA/OpenFGA.
13. **Observabilidad OpenTelemetry-first**: traces, métricas y logs por OTEL Collector con backends HA (Tempo/Mimir/Loki).
14. **Despliegue write-once**: 1 manifest set funciona en cluster dev (k3s, 6-10 GB RAM), HA prod (multi-AZ), air-gap (mirror) y multicloud (Crossplane opcional para infra).
15. **Recuperación medible**: tier-based RTO/RPO; restore probado trimestralmente; runbooks ejecutables.

## 2. Design principles (20)

1. No-SPOF por diseño: ningún componente crítico con réplica única.
2. Least privilege siempre; SA por servicio; secret de uso único.
3. Immutability: imágenes inmutables, tags por SHA, datasets Iceberg.
4. Explicit contracts: gRPC + protobuf entre planos; OpenAPI en bordes.
5. Event-driven where useful, sync where necessary; no abusar de eventos.
6. Stateless-first.
7. Storage fit-for-purpose; nada de "Postgres para todo".
8. Progressive delivery: canary auto + análisis SLO + rollback.
9. Idempotency obligatoria en todo handler que muta estado.
10. Backpressure explícita (tower-buffer, semaphore, queue size).
11. Circuit breakers + retries con jitter (no thundering herd).
12. Schemas versionados; expand-and-contract; backward compat ≥1 versión.
13. Read/write split donde aplique.
14. Aislamiento por namespace para tenants enterprise; quota-as-policy.
15. mTLS por defecto; cero tráfico interno sin identidad.
16. Secrets nunca en git; vault + rotación; cifrado en reposo.
17. Tracing antes que logging; métricas son el mínimo.
18. Operations-as-code: GitOps + Helm/Kustomize; manualidad minimizada.
19. Disaster recovery probado, no documentado.
20. Replicación cross-AZ default; cross-region por tier crítico.

## 3. System context

- **Actores:**
  - **End users** (analistas, data engineers, ML engineers, app builders) → web SPA (SvelteKit) + APIs REST/gRPC + notebook.
  - **Service accounts** (CI/CD pipelines, agents, integradores externos) → APIs autenticadas con OIDC client credentials.
  - **Operators** (SRE, platform team) → Argo CD UI, Grafana, Backstage (opcional), kubectl restringido, runbooks.
- **Trust boundaries:**
  - Cluster boundary (Ingress + WAF).
  - Namespace boundary (NetworkPolicy + RBAC + tenant).
  - Service boundary (mTLS + SPIFFE).
  - Data boundary (Iceberg ACL + OpenFGA + audit).
- **External systems:**
  - IdPs externos (Okta/Entra ID/Google) federados a Keycloak.
  - Cloud APIs (S3/GCS/Azure) si no on-prem; Crossplane para provisioning.
  - Notification sinks (email, Slack, PagerDuty).
  - Lineage consumers (downstream BI, data observability).

## 4. Capability map

| Dominio | Capacidades |
|---|---|
| Ingestion | Connectors batch, Debezium CDC, Kafka Connect, file uploads |
| Catalog | Dataset registry, schemas, lineage, versioning, tags, ownership |
| Compute | Spark batch, Flink streaming, Trino interactive, notebook runtime |
| Semantic | Ontology schema, indexer, query, actions, search, graph traversal |
| Workflow | Pipelines DAG, schedules, signals, triggers, retries, approvals |
| Security | AuthN, AuthZ (RBAC+ABAC+ReBAC), policy-as-code, audit, secrets |
| Apps & AI | App builder, custom endpoints, AI agents, RAG, evaluation |
| Platform control | Cluster bootstrap, addon mgmt, GitOps, secret mgmt, cert mgmt |
| Observability | Traces, metrics, logs, SLOs, alerting, runbook execution |

## 5. Logical architecture

```
                ┌────────────────────────────────────────────────────────────┐
                │  Edge: WAF + IngressController HA + Linkerd gateway         │
                └────────────────────────────────────────────────────────────┘
                                            │
                ┌────────────────────────────┼────────────────────────────┐
                │                            │                            │
        ┌───────▼────────┐         ┌─────────▼─────────┐         ┌────────▼───────┐
        │ Control plane  │         │ Workspace plane   │         │ Public APIs    │
        │ Argo CD,       │         │ web SPA, notebook │         │ gateway-svc    │
        │ Crossplane,    │         │ workspace mgmt    │         │ rate limit,    │
        │ cert-mgr, ESO  │         │                   │         │ auth gating    │
        └───────┬────────┘         └─────────┬─────────┘         └────────┬───────┘
                │                            │                            │
                │             ┌──────────────┴──────────────┐             │
                │             │  Identity & Policy plane    │             │
                │             │  Keycloak HA + OpenFGA      │             │
                │             └──────────────┬──────────────┘             │
                │                            │                            │
                ▼                            ▼                            ▼
        ┌──────────────────────────────────────────────────────────────────┐
        │ Domain plane (microservices, agrupados por bounded context)       │
        │  catalog | ontology | workflow | ml/ai | marketplace | governance │
        └──────────────────────────────────────────────────────────────────┘
                │                      │                       │
        ┌───────▼──────────┐  ┌────────▼────────┐  ┌───────────▼─────────┐
        │ Metadata plane   │  │ Semantic serving │  │ Operational state   │
        │ Postgres HA (8)  │  │ Vespa (BM25+ANN) │  │ Scylla/Cassandra    │
        │ schema-per-svc   │  │ + indexer pipe   │  │ (high-throughput)   │
        └──────────────────┘  └──────────────────┘  └─────────────────────┘
                                       │
                                ┌──────▼──────────┐
                                │ Event backbone  │
                                │ Kafka / Redpanda│
                                │ + Schema Reg    │
                                └──────┬──────────┘
                                       │
                ┌──────────────────────┴──────────────────────┐
                │ Data plane                                   │
                │ Iceberg + Parquet sobre S3 compatible (Ceph) │
                │ Trino/Spark/Flink consumers                  │
                └──────────────────────────────────────────────┘
                                       │
                ┌──────────────────────┴──────────────────────┐
                │ Workflow / orchestration                     │
                │ Temporal cluster HA                          │
                └──────────────────────────────────────────────┘
                                       │
                ┌──────────────────────┴──────────────────────┐
                │ Observability plane                          │
                │ OTEL Collector → Tempo / Mimir / Loki        │
                │ Prometheus Operator + Grafana                │
                └──────────────────────────────────────────────┘
```

## 6. Reference implementation (OSS concreta)

| Componente | OSS recomendado | Alternativa | Razón |
|---|---|---|---|
| Container orchestrator | Kubernetes 1.31+ | — | Estándar |
| GitOps | Argo CD | Flux | UI superior, app-of-apps |
| IaC infra cloud | Crossplane (opcional) | Terraform | Reconciliación nativa k8s |
| Service mesh | Linkerd 2.16 ambient | Istio Ambient | Operación simple |
| Ingress | nginx-ingress + cert-manager | Traefik | Estándar enterprise |
| Identity | Keycloak HA | Authentik | Madurez + SAML/OIDC |
| Policy | OpenFGA | OPA / Cedar | ReBAC fit Foundry |
| Secrets | Vault + External Secrets Operator | SOPS | Auditabilidad + rotación |
| Cert management | cert-manager + Let's Encrypt / step-ca | — | Estándar |
| Postgres HA | CloudNativePG | Patroni | Operator-first |
| Wide-column | ScyllaDB Operator | Cassandra | Performance + ops |
| Object storage | MinIO (cloud) o Rook Ceph (on-prem) | — | S3 API |
| Catalog Iceberg | Lakekeeper | Polaris / Nessie | OSS Apache-2.0 |
| Stream broker | Strimzi Kafka KRaft | Redpanda | Adopción amplia |
| Schema registry | Apicurio | Confluent CE | OSS puro |
| Search | Vespa | OpenSearch | Hybrid BM25+ANN, low latency |
| Workflow | Temporal | Argo Workflows / Dagster | Idempotencia + signals |
| Batch compute | Spark on K8s | — | Estándar |
| Stream compute | Flink (Strimzi-managed) | Spark Streaming | Event-time correcto |
| Interactive query | Trino | DuckDB en notebook | Federación |
| Lineage | Marquez (OpenLineage) | DataHub | Estándar abierto |
| Observability | OpenTelemetry Collector + Tempo + Mimir + Loki + Grafana + Prometheus Operator | DataDog | OSS Coherent |
| Chaos | Chaos Mesh | Litmus | Madurez |
| Backup k8s | Velero + Restic | Kasten K10 | OSS |
| Backup Postgres | CNPG barman-cloud | pgBackRest | Nativo |
| Container registry | Harbor | ghcr | Local + signing |
| Image signing | Cosign / Sigstore | Notary | OIDC keyless |
| SBOM | Syft + Grype | Trivy | Cobertura |
| Admission policy | Kyverno | Gatekeeper | YAML-friendly |
| Notebook | JupyterHub on K8s | Hex/Marimo | Estándar |
| Internal portal | Backstage (opcional) | — | Discovery |

## 7. Service decomposition (consolidación recomendada)

> Reducir 84 servicios a **~25-30** servicios cohesivos. Lista propuesta:

| Servicio | Responsabilidad | API | Estado | Storage | HA pattern | Failure modes |
|---|---|---|---|---|---|---|
| `gateway` | Edge proxy, JWT, rate-limit | REST | Stateless | Valkey HA | 3 réplicas + HPA | Saturación |
| `identity` | OIDC broker delgado sobre Keycloak | REST/gRPC | Stateless | — | 3 réplicas | Cache miss |
| `policy-decision` | Adapter sobre OpenFGA | gRPC | Stateless | OpenFGA store (Postgres) | 3 réplicas | Cold cache |
| `audit-sink` | Recibe eventos audit y escribe Iceberg WORM | Kafka consumer | Stateless | Kafka + Iceberg | 3 réplicas + consumer group | Backlog |
| `metadata-registry` | Schemas, datasets, ownership | REST/gRPC | Stateless | Postgres `pg-catalog` | 3 réplicas | DB primary failover |
| `lineage` | OpenLineage consumer + read API | REST + Kafka | Stateless | Postgres + Marquez | 3 réplicas | Backlog |
| `dataset-versioning` | Branches sobre Iceberg | REST | Stateless | Lakekeeper + Iceberg | 3 réplicas | Conflict |
| `connector-mgmt` | Conectores batch + CDC | REST | Stateless | Postgres `pg-platform` | 3 réplicas | External API |
| `ingestion-runner` | Lanzador de jobs batch (Spark/Flink) | k8s Jobs/Operator | Stateless | k8s API | 2 réplicas (leader-elected) | Job failure |
| `ontology-schema` | Tipos + relaciones + versiones | REST | Stateless | Postgres `pg-ontology` | 3 réplicas | Schema conflict |
| `ontology-indexer` | Consume Kafka write events → Vespa + Iceberg | Kafka consumer | Stateless | — | N consumers | Backlog |
| `ontology-query` | Read API (búsqueda + agregación) | REST/gRPC | Stateless | Vespa + Postgres `-ro` + cache | 3-N réplicas | Vespa partition |
| `ontology-actions` | Escrituras semánticas + Saga + emisión Kafka | REST + Kafka producer | Stateless | Postgres `pg-ontology` + Kafka | 3 réplicas | Conflict, backlog |
| `workflow-control` | Adapter sobre Temporal (start/list/cancel) | REST/gRPC | Stateless | Temporal | 2 réplicas | Temporal cluster |
| `workflow-workers` | Workers Temporal Rust SDK | Temporal task queue | Stateless | — | N workers | Worker pool |
| `approvals` | Workflows de aprobación | REST | Stateless | Postgres + Temporal | 3 réplicas | — |
| `model-registry` | ML models, versiones, artefactos | REST | Stateless | Postgres + S3 | 3 réplicas | — |
| `model-serving` | Inference HTTP/gRPC | REST/gRPC | Stateless (modelos en S3) | S3 | N réplicas + GPU | OOM |
| `agent-runtime` | Ejecución de agentes LLM + tools | REST + Temporal | Stateless | Postgres + S3 | 3 réplicas | LLM rate limit |
| `app-builder` | Definición de apps low-code | REST | Stateless | Postgres + S3 | 3 réplicas | — |
| `notebook-runtime` | Lanza pods Jupyter por usuario | k8s Jobs | Stateless | k8s API + S3 | 2 réplicas | Resource quota |
| `query-gateway` | Trino router federado | REST/Flight SQL | Stateless | — | 3 réplicas | Trino cluster |
| `notification` | Email, Slack, PagerDuty | Kafka consumer | Stateless | — | N consumers | Sink down |
| `tenancy` | Tenants, workspaces, quotas | REST | Stateless | Postgres `pg-platform` | 3 réplicas | — |
| `web` | SvelteKit SSR | HTTP | Stateless | — | 3 réplicas | — |

## 8. Storage architecture

### Matriz Workload → Store recomendado

| Workload | Store | Por qué | Lo que NO debe ir |
|---|---|---|---|
| Datasets grandes inmutables | Iceberg + Parquet sobre S3 (Ceph/MinIO) | ACID tablas + branching + time travel | Estado mutable de baja latencia |
| Metadata relacional acotada (<1 TB por dominio) | Postgres HA (CNPG) | Consistencia fuerte, transacciones | Logs de eventos, blobs, búsqueda full-text |
| Estado operacional alta tasa (sesiones, contadores, índices invertidos pequeños) | ScyllaDB / Cassandra | Multi-DC, escalado horizontal | Joins complejos, consistencia fuerte |
| Eventos / streams | Kafka (Strimzi) o Redpanda | Backbone durable | Estado long-tail (compactar o sink a Iceberg) |
| Búsqueda semántica + texto | Vespa | BM25 + ANN + tensor + ranking | OLTP |
| Cache | Valkey HA / Dragonfly | Ephemeral key-value | Source of truth |
| Workflow state | Temporal (Postgres backend) | Durabilidad, signals, retries | Datos de negocio |
| Audit trail inmutable | Kafka topic compactado/append + sink Iceberg | WORM auditable | Operaciones online |
| Graph (opcional) | JanusGraph / Neo4j | Si grafo es first-class | Metadata genérica |
| Vector store | Vespa (preferido) o Qdrant | RAG | Sin justificación → no añadir |

### Modelos de consistencia

| Subsistema | Modelo | Implicación |
|---|---|---|
| Postgres CNPG | Sync replication (1 réplica), strong consistency | RW puede bloquear si quedan <2 nodos |
| Scylla | Tunable (LOCAL_QUORUM recomendado) | Read-your-writes garantizado |
| Iceberg | Snapshot isolation | Branching para experimentos |
| Kafka | At-least-once por defecto + idempotent producer + transactional opcional | Consumidores deben ser idempotentes |
| Vespa | Eventual sobre escrituras async | Lag medible (stale-by) |
| Valkey | Best-effort | Fallback acceptable |

## 9. No-SPOF strategy (por subsistema)

| Subsistema | Estrategia |
|---|---|
| Ingress | 2+ replicas IngressController + ExternalDNS sobre LB cloud HA o keepalived on-prem |
| Control plane k8s | Managed (EKS/GKE/AKS) o k3s HA con etcd embedded ≥3 nodos / kube-vip |
| Service mesh | Linkerd HA (controller 3 réplicas, identity 3 réplicas) |
| Identity | Keycloak HA modo `kc.sh start --features=multi-site` con 3 réplicas + DB CNPG |
| Metadata | CNPG ≥3 réplicas, sync, failover automático <30 s |
| Workflow engine | Temporal HA (frontend 3, history 3, matching 3, worker 3) sobre Cassandra/Scylla 3-DC |
| Event bus | Kafka 3 brokers KRaft, RF=3, ISR=2, mirror MM2 multi-region opcional |
| Object storage | Ceph 5 MON/2 MGR/EC 8+3/zone-aware o S3 cloud regional |
| Search | Vespa 3 configservers + 2 stateless containers + 3 content (redundancy=2) |
| Catalog Iceberg | Lakekeeper 3 réplicas + Postgres backend HA |
| Ontology query | 3+ réplicas detrás de Linkerd + cache local + circuit breaker |
| Lineage | Stateless consumers Kafka + idempotent merge a Postgres |
| Observability | Tempo/Mimir/Loki en HA con backend S3 + Grafana 2 réplicas |
| CI/CD | GitHub Actions o Tekton; Argo CD HA (controller 2, repo-server 2, server 2, redis HA) |
| Secrets/certs | Vault HA (Raft 3 nodos), cert-manager + Let's Encrypt staging fallback |
| Backups | Velero + CNPG ScheduledBackup → object storage en otra región |
| Topología regional | DR multi-región con replicación asíncrona del data plane (Iceberg → cross-region S3, Kafka MM2) |

## 10. Deployment topology

### Single cluster dev
- 1 nodo (k3s, Rancher Desktop, kind).
- Réplicas=1 forzadas. Stores ligeros (Postgres bundleado, Valkey, NATS, MinIO).
- ~6-10 GB RAM.

### HA production cluster (single region, multi-AZ)
- 3 AZ × ≥3 nodos cada uno (control plane managed o k3s HA).
- topologySpread por zone para todos los servicios críticos.
- Ceph zone-aware, CNPG con instances en distintas zonas.
- Linkerd ambient + Keycloak HA + Vault HA + Argo CD HA.

### Multi-region active/passive
- Region A: full stack productivo.
- Region B: stack en standby; sólo data plane recibiendo replicación async (S3 cross-region replication, Kafka MM2 mirroring, Postgres archive ship).
- Failover manual con runbook ejecutable; RTO ~30 min, RPO ~5 min según tier.

### Multi-region active/active
- Solo justificable para servicios stateless globales (gateway, web) y para datasets read-only globales.
- Stateful (Postgres, Kafka, Temporal) → active/passive por región (consistencia > disponibilidad).
- Identity en active/active vía Keycloak multi-site (Infinispan cross-DC).

### Air-gapped / sovereign
- Mirror de imágenes en Harbor local + signing reverificable.
- Argo CD apuntando a git mirror interno.
- Sin egress público (NetworkPolicy strict).
- Vault on-prem.

## 11. Reliability model

### SLOs por subsistema (ejemplo)

| Subsistema | Latency P95 | Availability | Errors |
|---|---|---|---|
| gateway | 100 ms | 99.95% | <0.1% 5xx |
| identity | 150 ms | 99.99% | <0.01% |
| ontology-query | 200 ms (p95) / 500 ms (p99) | 99.9% | <0.5% |
| ontology-actions | 300 ms | 99.9% | <0.5% |
| workflow control | 500 ms | 99.9% | <0.5% |
| metadata-registry | 100 ms | 99.95% | <0.1% |
| event ingest (Kafka) | 50 ms | 99.99% | <0.01% |

### RTO / RPO por tier

| Tier | RTO | RPO | Estrategia |
|---|---|---|---|
| Tier 0 (identity, gateway, audit) | 5 min | 0 (sync replication) | Multi-AZ + Keycloak HA + audit dual-write |
| Tier 1 (ontology, workflow, metadata) | 15 min | <1 min | CNPG sync + Temporal HA + Vespa async |
| Tier 2 (analytics, ML serving) | 1 h | <15 min | Iceberg snapshot, model rollback |
| Tier 3 (notebook, sandbox) | 4 h | <1 h | Best-effort |

### Patrones obligatorios
- Health: liveness, readiness, startup probes diferenciadas.
- Graceful shutdown (SIGTERM → drain → close DB pools → exit).
- preStop hook con sleep 5s para drenar Linkerd/Service.
- Backpressure: tower-buffer + tower-load-shed.
- Retries con jitter: backoff exponencial 100ms→5s, max 3 attempts.
- Idempotency keys en handlers POST.
- Deduplication en consumidores Kafka (Idempotent producer + transactional consumer).
- Quorum: Vault Raft 3, Ceph MON 5, Kafka KRaft 3.
- Anti-entropy: Cassandra/Scylla repair semanal, Ceph scrub diario.
- Reconciliation: Argo CD app-of-apps cada 3 min.

## 12. Security architecture

- **SSO/OIDC/SAML:** Keycloak con realms por tenant; brokers a Okta/Entra/Google.
- **Machine identity:** SPIFFE via Linkerd; ServiceAccount por servicio.
- **mTLS:** Linkerd strict; cert rotation automática 24 h.
- **Network policy:** default-deny por namespace; allow-lists explícitos por servicio.
- **Encryption at rest:** Ceph dm-crypt, CNPG `tablespaces encrypted` + LUKS, Kafka TLS.
- **Encryption in transit:** TLS en Ingress (cert-manager), mTLS este-oeste (Linkerd), TLS Kafka.
- **Secrets:** Vault Raft HA + ESO; rotación 90 d; dynamic credentials para Postgres.
- **Policy enforcement:** OpenFGA como PDP; PEP en gateway y servicios críticos.
- **Audit trail:** todo CREATE/UPDATE/DELETE → Kafka topic `audit.events` (RF=3, retention=∞) → sink Iceberg WORM.
- **Tenant isolation:** namespace-per-tenant para enterprise; ResourceQuota + LimitRange + NetworkPolicy.
- **Row/column/object-level authz:** evaluado en `ontology-query` mediante plan rewriting con OpenFGA hints + masking columns.
- **Break-glass:** Vault root token sealed, custodia 3-of-5 (Shamir).
- **Air-gap:** Harbor mirror + Argo CD apunta a git interno + NetworkPolicy egress denegado a internet.

## 13. Ontology and semantics architecture

### Componentes
- `ontology-schema` (registry de tipos, relaciones, acciones, versiones).
- `ontology-actions` (write plane: edits operacionales con validación, autorización, emisión de eventos).
- `ontology-indexer` (consume Kafka, materializa en Vespa + Iceberg).
- `ontology-query` (read plane: search-around, agregaciones, traversal grafo).
- `ontology-graph` (opcional: JanusGraph si grafo es first-class).

### Write path
```
Cliente → gateway → ontology-actions
       → valida schema (ontology-schema cache)
       → autoriza (policy-decision / OpenFGA)
       → escribe Postgres pg-ontology (transacción)
       → emite event ontology.write.v1 a Kafka (transactional outbox)
       → audit-sink consume → Iceberg WORM
       → ontology-indexer consume → Vespa upsert + Iceberg materialization
```

### Read path
```
Cliente → gateway → ontology-query
       → check policy (OpenFGA) → derive filters
       → query Vespa (BM25 + ANN + filtros) — fast path
       → fallback Postgres -ro (consistency-required reads)
       → response con masking aplicado
```

### Schema evolution
- Schemas versionados con compat backward (≥1 versión).
- Migrations expand-and-contract.
- Reindexing online: `ontology-indexer` arranca con `--reindex --version=N+1`, escribe en namespace Vespa nuevo, swap atómico.
- Conflict resolution: optimistic concurrency con `version` column + retry.

### Low-latency serving
- Vespa con caching tiers (in-memory + on-disk).
- Cache local (moka) en `ontology-query` con TTL corto (30s) e invalidación por evento.

## 14. Data lifecycle architecture

```
Source (DB, file, API)
   │
   ▼
Ingestion (Airbyte / Debezium / Kafka Connect / custom)
   │
   ▼
Staging (Iceberg branch `_staging`)
   │
   ▼
Validation (Great Expectations / dataset-quality)
   │
   ▼
Canonical (Iceberg main branch + tag)
   │
   ├─► Semantic serving (ontology-indexer → Vespa)
   ├─► Analytics (Trino / Spark / Flink)
   └─► Archive (Iceberg snapshot expire 90d → Glacier-tier)
                        │
                        ▼
                Retention & Purge (legal hold / GDPR)
```

## 15. Platform operations

- **GitOps:** Argo CD app-of-apps. Repo `gitops/` con `apps/`, `clusters/`, `infrastructure/`. ApplicationSet por entorno.
- **Progressive delivery:** Argo Rollouts canary 5%→25%→50%→100% con análisis Prometheus (latency, error rate, saturation).
- **Blue/green:** sólo para servicios con state-transfer caro (rare).
- **Node rotation:** drain → cordon → delete; PDB protege; DaemonSet rolling.
- **Cluster upgrade:** managed (EKS/GKE/AKS) o kubeadm + N-1 strategy.
- **Schema migrations:** Job pre-upgrade Helm hook; expand-and-contract.
- **Data migrations:** Spark job + Iceberg branching (validar en branch antes de merge).
- **Chaos:** Chaos Mesh experimentos mensuales (pod-kill, network-loss, cpu-stress).
- **Capacity:** Karpenter (cloud) o cluster-autoscaler; quotas por tenant.

## 16. ADRs (12)

### ADR-001 — Heterogeneous storage instead of single database
- **Status:** Accepted.
- **Context:** Una sola DB no escala para todos los workloads.
- **Decision:** Postgres (metadata acotada), Iceberg (datasets), Scylla (estado operacional alta tasa), Kafka (eventos), Vespa (semantic serving).
- **Consequences:** Más operativa, mejor performance/coste; necesita expertise multi-store.

### ADR-002 — Object storage + Iceberg para datasets inmutables
- **Decision:** Iceberg sobre S3-compatible (Ceph on-prem, S3 cloud).
- **Consequences:** ACID + branching + time-travel; requiere catalog (Lakekeeper).

### ADR-003 — Scylla/Cassandra para estado operacional de alta tasa
- **Decision:** Scylla para sesiones, contadores, eventos en vivo, índices invertidos pequeños.
- **Consequences:** Eventual consistency; modelado por queries.

### ADR-004 — Postgres HA solo para metadatos relacionales acotados
- **Decision:** ≤8 clusters CNPG agrupados por plano lógico; <1 TB cada uno.
- **Consequences:** Operabilidad razonable; aislamiento por schema.

### ADR-005 — Separar ontology write plane y query plane
- **Decision:** Writes en Postgres + emite Kafka; reads sirven de Vespa/cache.
- **Consequences:** Lag medible (segundos); reads escalan independiente.

### ADR-006 — GitOps con Argo CD
- **Decision:** Argo CD app-of-apps; ApplicationSet por entorno.
- **Consequences:** Source-of-truth git; promoción auditable.

### ADR-007 — Zero-trust este-oeste con Linkerd ambient
- **Decision:** mTLS strict; identidades SPIFFE por SA.
- **Consequences:** Operación adicional de mesh; trade-off worth it.

### ADR-008 — Active/passive multi-region para stateful
- **Decision:** Replicación async cross-region; failover manual con runbook.
- **Consequences:** RPO ~minutos; consistencia preservada.

### ADR-009 — Kafka como event backbone
- **Decision:** Strimzi KRaft; RF=3 ISR=2; topics versionados.
- **Consequences:** Backbone único; cuidado con backlog.

### ADR-010 — Policy-as-code con OpenFGA
- **Decision:** Modelo ReBAC; cliente en cada PEP.
- **Consequences:** Policies versionadas; auditables.

### ADR-011 — Node ephemerality como forcing function
- **Decision:** Nodos efímeros (rotación semanal); todo stateless o StatefulSet bien diseñado.
- **Consequences:** Forzosa correctitud HA; mejor higiene.

### ADR-012 — OpenTelemetry-first observability
- **Decision:** Todo emite spans/metrics/logs vía OTLP; backends Tempo/Mimir/Loki.
- **Consequences:** Estandarización; vendor-neutral.

## 17. Trade-offs y alternativas

### Postgres HA vs CockroachDB vs YugabyteDB
| Criterio | Postgres CNPG | CockroachDB | YugabyteDB |
|---|---|---|---|
| Compatibilidad | 100% Postgres | Postgres-like | Postgres + Cassandra-like |
| Multi-region active/active | No (active/passive) | Sí (built-in) | Sí |
| Operabilidad | Alta | Media | Media |
| Coste licencia | Free | Enterprise costoso | Free |
| **Recomendación** | **Postgres CNPG** para mayoría; CockroachDB sólo si multi-region active/active es requisito hard | | |

### Cassandra/Scylla vs only-Postgres
- Postgres no escala más allá de ~10k writes/s sostenidos en single primary sin sharding manual.
- Scylla escala horizontalmente con linealidad real.
- **Recomendación:** Scylla para sesiones, contadores, eventos en vivo. Postgres para todo lo transaccional acotado.

### Temporal vs Dagster vs Argo Workflows
| | Temporal | Dagster | Argo Workflows |
|---|---|---|---|
| Modelo | Durable execution | DAG declarativo | DAG YAML |
| Idempotency | Built-in | Manual | Manual |
| Signals/queries | Sí | No | No |
| Estado | Cassandra/Postgres | Postgres | k8s CRDs |
| Use case | Workflows largos, retries, signals | Pipelines de datos típicos | CI/CD, ML pipelines |
- **Recomendación:** **Temporal** para workflow engine general; Argo Workflows opcional para batch ML.

### OpenSearch vs Vespa
- OpenSearch: BM25 maduro, vector OK, buena UX devs.
- Vespa: BM25 + ANN nativo + ranking ML + tensor + low-latency multi-stage. Menos comunidad.
- **Recomendación:** **Vespa** si la búsqueda es first-class (ontology, retrieval). OpenSearch si necesitas Kibana-like UX.

### Iceberg vs Delta Lake vs Hudi
| | Iceberg | Delta Lake | Hudi |
|---|---|---|---|
| OSS gobernanza | Apache | Linux Foundation (dominado por Databricks) | Apache |
| Lectores | Trino, Spark, Flink, DuckDB, ClickHouse | Spark first-class, otros vía conector | Spark, Flink |
| Branching | Sí | No (tags v3) | No |
| **Recomendación** | **Iceberg** | | |

### Istio vs Linkerd
- Istio: features ricos, complejidad operativa alta.
- Linkerd: subset suficiente, ops trivial, ambient en estable.
- **Recomendación:** **Linkerd** salvo necesidad explícita de Istio Gateway/EnvoyFilter.

### Keycloak + OpenFGA vs custom auth
- Custom = ahorrar 1 deployment a cambio de meses de hardening propio.
- **Recomendación:** **Keycloak + OpenFGA** sin discusión.

### Trino vs Spark SQL serving
- Trino: low-latency interactive (<10 s).
- Spark SQL: ETL batch.
- **Recomendación:** Trino para serving (BI, sql-bi-gateway), Spark para ETL.

## 18. Failure scenarios (20+)

| # | Escenario | Impacto | Detección | Mitigación | Recuperación |
|---|---|---|---|---|---|
| 1 | Caída worker node | Pods evacuados | k8s node NotReady alert | PDB + topologySpread | Reschedule auto |
| 2 | Caída AZ completa | 1/3 capacidad perdida | Multi-AZ alert | Ceph zone-aware, CNPG zone-aware | Auto failover |
| 3 | Partición de red entre AZ | Split brain riesgo | Alertmanager | Quorum CNPG/Kafka/Vault | Resolución manual si quorum perdido |
| 4 | Pérdida temporal de Kafka brokers | Backlog productores | JMX metrics | RF=3 ISR=2; productor retry | Auto |
| 5 | Cluster OpenSearch/Vespa degradado | Lecturas degradadas | Vespa metrics | redundancy=2 | Auto + reindex parcial |
| 6 | Postgres primary failover | RW pausa ~30 s | CNPG metrics | Connection retry + jitter | Auto |
| 7 | Corrupción metadatos | Bug aplicación | Audit + reconciliation | Backups + WAL PITR | Restore PITR |
| 8 | Backlog masivo en indexing | Lag minutos→horas | Lag metric | Autoscale consumers | Catch-up |
| 9 | Schema migration fallida | Pod CrashLoop | Helm hook fail | Pre-flight + dry-run | Rollback Helm |
| 10 | Pérdida de un MinIO/Ceph node | Replicación auto | Ceph health | Replication 3 / EC 8+3 | Reconstrucción auto |
| 11 | Expiración certificados | TLS handshake fail | cert-manager metrics | Renovación automática | Force renew |
| 12 | IdP externo no disponible | Sin login nuevos | Keycloak metrics | Cache JWT corta + degraded read-only | Re-auth cuando vuelve |
| 13 | Drift GitOps | Cluster ≠ git | Argo CD diff | Auto-sync (dev/staging), alerta prod | Manual sync |
| 14 | Pérdida de Vault | Secrets inaccesibles | Vault health | Raft 3 nodos + auto-unseal | Restore + unseal Shamir |
| 15 | Saturación gateway | 5xx | SLO burn | HPA + rate limit + circuit breaker downstream | Scale + investigar |
| 16 | Temporal cluster down | Workflows pausados | Temporal metrics | HA frontend/history/matching | Auto |
| 17 | Backup falla | Sin backup nuevo | Velero/CNPG alert | Alerta inmediata | Re-trigger |
| 18 | Restore corrupto | DR inválido | Drill trimestral | Validación checksum | Backup más antiguo |
| 19 | Imagen comprometida | Posible RCE | Cosign verify fail | Kyverno admission deny | Rollback + investigar |
| 20 | Tenant noisy neighbor | Latencia otros tenants | Per-tenant metrics | ResourceQuota + LimitRange | Throttle |
| 21 | LLM API rate-limited | Agentes bloqueados | Agent metrics | Backpressure + queue + multi-provider | Failover provider |
| 22 | Iceberg conflict en commit | Write reject | Spark/Flink retry | Optimistic retry + branching | — |
| 23 | OpenFGA store down | Authz fail-closed | OpenFGA health | Fail-closed (deny) + alert | Restart store |
| 24 | Argo CD down | Sin sync | Health check | HA Argo (controller 2, repo-server 2) | Auto |
| 25 | DNS interno fail | Resolución rota | CoreDNS metrics | NodeLocal DNSCache + replicas ≥3 | Auto |

## 19. Roadmap evolutivo (4 fases)

### Fase 1 — MVP (1-2 sprints)
- Consolidar Postgres a 8 clusters.
- CI/CD para imágenes (todos los servicios) + cosign.
- Argo CD instalado + app-of-apps inicial.
- Migrations como Job pre-upgrade.
- ESO + Vault dev.
- OTEL Collector + Tempo (single replica acceptable dev).
- Reducir servicios a ~30 (eliminar stubs).

### Fase 2 — Production-ready (1-2 meses)
- Linkerd ambient + mTLS.
- Keycloak HA + OpenFGA.
- Temporal HA.
- Vespa obligatorio.
- Argo Rollouts canary.
- Velero backups.
- Chaos Mesh + Game Day mensual.

### Fase 3 — Enterprise hardened (1-2 trimestres)
- Multi-AZ HA real probada.
- SLO dashboards y burn-rate alerts.
- Tenancy por namespace.
- Audit Iceberg WORM.
- Image signing obligatorio Kyverno.
- Backstage developer portal.
- Restore drill trimestral.

### Fase 4 — Multi-region / sovereign / air-gapped
- Active/passive multi-region (Iceberg cross-region replication, Kafka MM2).
- Harbor mirror para air-gap.
- Crossplane para infra cloud opcional.
- BYOK encryption (KMS por tenant).
- FedRAMP/SOC2 evidence pack.

## 20. ASCII diagrams adicionales

### Despliegue HA en Kubernetes (single region multi-AZ)

```
                 ┌──────────────────────────────────────────────┐
                 │             Cloud LB / kube-vip               │
                 └─────────────────┬────────────────────────────┘
                                   │
                       ┌───────────┼───────────┐
                       │           │           │
                ┌──────▼─────┐ ┌───▼────┐ ┌────▼──────┐
                │ AZ-a (3 nodos) │ AZ-b (3) │ AZ-c (3) │
                │  - ingress     │  - ingress │  - ingress │
                │  - linkerd     │  - linkerd │  - linkerd │
                │  - apps stateless│  - apps   │  - apps    │
                │  - cnpg-1 (rw) │  - cnpg-2 (s)│ - cnpg-3 (s)│
                │  - kafka-0     │  - kafka-1 │  - kafka-2  │
                │  - vespa-cs/c/ct│ - vespa    │  - vespa    │
                │  - ceph mon/mgr│  - ceph mon│  - ceph mon │
                └────────────────┘└────────────┘└────────────┘
```

### Topología DR multi-region

```
   Region A (active)                    Region B (passive)
 ┌──────────────────────┐    async      ┌──────────────────────┐
 │ k8s + apps + stores  │──────────────▶│ k8s + apps idle      │
 │ Postgres primary     │  WAL ship     │ Postgres standby     │
 │ Kafka cluster        │  MM2 mirror   │ Kafka cluster passive│
 │ Iceberg / S3         │  S3 CRR       │ Iceberg / S3         │
 │ Vault primary        │  perf rep     │ Vault performance    │
 │ Argo CD              │  manifest sync│ Argo CD              │
 └──────────┬───────────┘               └─────────┬────────────┘
            │       failover runbook              │
            └─────────────────────────────────────┘
                       Decisión humana
                  (RTO ~30 min, RPO ~5 min)
```

### Write path semántico

```
client ─▶ gateway ─▶ ontology-actions
                       │
                 ┌─────┼─────────────────────────────────┐
                 ▼     ▼                                 ▼
            Postgres  Kafka                        OpenFGA check
              (tx)    (transactional outbox)
                            │
                ┌───────────┼─────────────────┐
                ▼           ▼                 ▼
           audit-sink   ontology-indexer   notification
                │           │
                ▼           ▼
           Iceberg WORM   Vespa upsert + Iceberg snapshot
```

### Read path semántico

```
client ─▶ gateway ─▶ ontology-query
                       │
                 ┌─────┼─────────────┐
                 ▼     ▼             ▼
              OpenFGA  Vespa       cache (moka)
              (filters) (BM25+ANN+rank)
                       │
                 fallback (consistency-required)
                       ▼
                Postgres -ro
                       │
                 ▼
                 mask + serialize
```

## 21. Final recommendation

### Arquitectura objetivo (resumen ejecutivo)

- **30 servicios** cohesivos (consolidación desde 84) sobre Kubernetes HA multi-AZ.
- **8 clusters Postgres CNPG** agrupados por plano + Scylla 3 nodos para estado operacional alta tasa + Iceberg sobre Ceph/MinIO + Vespa para búsqueda + Kafka KRaft para eventos + Temporal para workflow + Keycloak para identidad + OpenFGA para policy + Vault para secrets.
- **Linkerd ambient** + mTLS estricto + SPIFFE.
- **Argo CD** app-of-apps + Argo Rollouts.
- **OpenTelemetry-first** observabilidad (Tempo + Mimir + Loki + Grafana).
- **Cosign + Syft + Kyverno** para supply chain.
- Despliegue **write-once**: dev (k3s), prod multi-AZ, air-gap (Harbor mirror), multicloud (Crossplane opcional).

### Stack recomendado (top 12)

Kubernetes · Argo CD · Linkerd · Keycloak · OpenFGA · Vault · CloudNativePG · Strimzi Kafka · Lakekeeper + Iceberg + Ceph · Vespa · Temporal · OpenTelemetry stack.

### Qué evitar (anti-patrones para esta plataforma)

1. **Database-per-microservice extrema** (>10 clusters por equipo small-medium): operacionalmente insostenible.
2. **Workflow engine homemade**: Temporal existe, no reinventes durabilidad.
3. **Identity custom**: Keycloak existe, no reinventes OIDC.
4. **Single Helm release para 80+ servicios**: blast radius enorme.
5. **Postgres como cache, queue, search y blob store** simultáneo.
6. **Migrations in-process al startup**: rollback imposible.
7. **CPU limits muy ajustados** sobre cargas burst: throttling oculta SLO violations.
8. **NetworkPolicy sin default-deny**: zero-trust nominal.
9. **Replicas=1 con anti-affinity required**: pods no scheduleables.
10. **Cron en Deployment**: doble disparo o SPOF.
11. **Pure latest tags en producción**: imposible reproducir estado.
12. **GitOps opcional**: drift garantizado.

### Top 10 riesgos (de mayor a menor)

1. Identity custom sin auditoría (potencial CVE).
2. Sobrefragmentación Postgres (operabilidad).
3. Helm release único (blast radius).
4. Workflow engine custom (durabilidad).
5. Sin GitOps (drift).
6. Sin OTEL (diagnóstico imposible a escala).
7. Sin Vault (rotación inexistente).
8. Sin canary (despliegue agresivo).
9. Vespa opcional (búsqueda degradada).
10. Sin chaos drills (HA no probada).

### Top 10 next steps accionables

1. Matrix CI dinámica para construir y firmar las 84 imágenes.
2. Consolidar a 8 clusters Postgres CNPG + plan migración con expand-and-contract.
3. Instalar Argo CD + bootstrap app-of-apps.
4. Mover migrations a Job pre-upgrade Helm hook.
5. ESO + Vault dev → eliminar Secrets `change-me` del repo.
6. Linkerd ambient + Kyverno admission verificando mTLS strict.
7. Sustituir identity-federation-service por Keycloak HA.
8. Migrar workflow-automation a Temporal.
9. OTEL Collector + Tempo + Loki desplegados con backend S3.
10. Definir SLOs por servicio + dashboards de burn-rate + Argo Rollouts con análisis.

---

**Fin del documento.**
