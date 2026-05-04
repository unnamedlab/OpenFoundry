# Plan de migración a patrón Foundry-pattern de orquestación

> **Estado**: PROPUESTO. Reemplaza la apuesta por Temporal de [ADR-0021](adr/ADR-0021-temporal-on-cassandra-go-workers.md).
>
> **Objetivo**: Sustituir el orquestador Temporal centralizado por orquestación distribuida estilo Palantir Foundry: Spark Operator para batches, Kafka consumers para eventos, state machines en Postgres para flujos con estado, todo coordinado vía outbox transaccional + Debezium.
>
> **Motivación a escala TB-PB / miles de usuarios**: eliminar SPOF "del orquestador", reducir coste operacional (no dimensionar Temporal+Cassandra-dedicada), eliminar el footgun del determinismo en código de negocio, alinear con plan declarado (ADR-0022 outbox + Debezium ya es el patrón Automate).
>
> **Duración estimada**: 6-10 semanas con 1 ingeniero senior dedicado, o 3-5 semanas con 2 ingenieros en paralelo.
>
> **Riesgo**: Medio. El refactor toca código en producción-bound pero ningún workflow está aún en uso real (estamos en pre-producción según el migration-plan-cassandra-foundry-parity §0). Esta es la ventana barata para corregir.

---

## 0. Resumen del cambio

### Antes (estado actual)
- 5 Go workers en [`workers-go/`](../../workers-go/) consumiendo Temporal SDK
- Persistencia Temporal sobre Cassandra (default + visibility)
- Servicios Rust con `temporal-client` Rust ([`libs/temporal-client/`](../../libs/temporal-client/)): `pipeline-schedule-service`, `approvals-service`, `automation-operations-service`
- Cluster Temporal: 4 services (frontend/history/matching/worker) + Cassandra dedicada
- Determinismo y replay como contrato del SDK

### Después (estado objetivo)
- **Pipeline workloads**: SparkApplication CRs sobre Spark Operator (k8s-native)
- **Reindex / fan-out de eventos**: Kafka consumer puro (Rust o Go con `event-bus-data`)
- **Workflow-automation**: patrón Automate (condition consumer → effect dispatcher) sobre Kafka + Postgres state machine
- **Automation-ops**: saga choreography vía outbox Postgres + Debezium → Kafka
- **Approvals**: state machine en Postgres (`pg-policy.identity_federation` o nuevo schema) + cron K8s + notification queue (NATS / Kafka)
- **Cero dependencia de Temporal**: cluster eliminado, libs eliminadas, workers Go eliminados o reescritos

### Principios que guían cada tarea
1. **Cada workflow vive donde el dato vive**: no hay "estado del orquestador" separado.
2. **Outbox + Debezium + Kafka es el único bus** de coordinación cross-servicio. NATS solo para hot-path control plane.
3. **Idempotencia obligatoria** en todos los consumers/handlers. Cada evento tiene `event_id` (UUID v5 sobre keys deterministas).
4. **Saga choreography, no orchestration**. Cada step publica su outcome; el siguiente step se dispara consumiendo. No hay un nodo central que dirija.
5. **Compensaciones explícitas** como events. Si paso 5 falla, paso 5 publica `step5.failed` que dispara compensaciones de 4, 3, 2, 1.
6. **Time-based triggers** = K8s CronJob que publica events, no in-process tick loops.
7. **Observabilidad por evento**: cada step persiste audit en `audit_compliance` schema (Postgres) y emite Prometheus counters.

### Archivos / módulos afectados

| Tipo | Eliminar | Refactorizar | Nuevo |
| --- | --- | --- | --- |
| Code Rust | `libs/temporal-client/` | `services/{pipeline-schedule,approvals,automation-operations}-service/src/domain/temporal_*.rs` | `libs/state-machine/`, `libs/saga/`, `libs/event-scheduler/` |
| Code Go | `workers-go/{workflow-automation,pipeline,approvals,automation-ops,reindex}/` | (ninguno; los reescribimos como Kafka consumers Rust o eliminamos) | (opcional) `workers-go/` Kafka consumers en Go puro |
| Infra | `infra/helm/infra/temporal/` | `infra/helm/infra/spark-jobs/` (añadir SparkApplication para pipelines), `infra/helm/infra/cassandra-cluster/` (drop temporal_* keyspaces) | KafkaTopic CRs, schema migrations Postgres |
| Docs | (none) | `ADR-0021`, migration plan §0 §2 §14 | `ADR-0027` Foundry-pattern orchestration |

---

## 1. Mapeo workflow → patrón Foundry

| Worker actual | Workflows Temporal | Patrón Foundry equivalente |
| --- | --- | --- |
| `pipeline-worker` | `PipelineRun` (pipeline build/execute) | **SparkApplication CR**. `pipeline-build-service` crea el CR, K8s + Spark Operator orquestan. |
| `reindex-worker` | `OntologyReindex` (paginar Cassandra → publicar Kafka) | **Kafka producer + consumer**. Trigger = event en `ontology.changes.v1`; consumer pagina y publica a `ontology.reindex.v1`. |
| `workflow-automation-worker` | `AutomationRun` (ejecutar action contra ontology-actions-service) | **Automate-pattern**: `automate.condition.<id>` event → consumer → `ontology-actions-service.execute()` HTTP call → `automate.outcome.<id>` event. |
| `automation-ops-worker` | `AutomationOpsTask` (saga con compensaciones) | **Saga choreography vía outbox**. Cada step publica outcome via outbox; siguiente step lo consume. Falla → publica `step.failed` → consumers de compensación reaccionan. |
| `approvals-worker` | `ApprovalRequest` (espera humana N días) | **State machine Postgres** + CronJob k8s para timeouts + Kafka topic `approval.events` para notify. |

---

## 2. Fases y tareas

Cada tarea es un **prompt autocontenido** listo para entregar a un agente. Sigue convenciones:
- "Context": estado al iniciar la tarea
- "Goal": objetivo concreto verificable
- "Steps": pasos ordenados con paths exactos
- "Verification": comandos/criterios para validar
- "Failure modes": fallos comunes y mitigación

---

## FASE 0 — Decisión, ADRs, inventario

### Tarea 0.1 — Crear branch dedicado y ADR-0027

```text
Context: Repo en /Users/torrefacto/Documents/Repositorios/OpenFoundry. ADR-0021 declara
Temporal sobre Cassandra como decisión arquitectural. Migración a patrón Foundry requiere
formalmente superseder esa decisión.

Goal: branch git nuevo `migration/foundry-pattern-orchestration` y ADR-0027 que documenta
el cambio. ADR-0021 marcado como Superseded.

Steps:
1. `git checkout -b migration/foundry-pattern-orchestration`.
2. Crear `docs/architecture/adr/ADR-0027-foundry-pattern-orchestration.md` con:
   - Status: Accepted
   - Date: today
   - Supersedes: ADR-0021
   - Context: Temporal evaluado, pros/cons a escala PB documentados (link a este plan).
   - Decision: refactor a patrón Foundry — Spark/Kafka/Postgres state machines, sin
     orquestador centralizado.
   - Consequences: positivas (operacional, económico, alineación), negativas (refactor
     cost, durable execution manual via outbox+idempotencia).
   - Implementation: link a este plan.
3. Editar `docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md`:
   - Status: Superseded
   - Add header line: "**Superseded by [ADR-0027](ADR-0027-foundry-pattern-orchestration.md)
     on YYYY-MM-DD.**"
4. `git add . && git commit -m "docs(adr): supersede ADR-0021 with ADR-0027 (Foundry pattern)"`

Verification:
- `git log --oneline -1` muestra el commit.
- `head -3 docs/architecture/adr/ADR-0021-*.md` muestra el "Superseded by" banner.
- `head -10 docs/architecture/adr/ADR-0027-*.md` muestra Status Accepted.

Failure modes: ninguno; es solo documentación.
```

### Tarea 0.2 — Inventario completo de superficie Temporal

```text
Context: Necesitamos saber EXACTAMENTE qué se va a tocar antes de refactorizar. El refactor
es ~60 tareas; un inventario claro evita olvidar ficheros.

Goal: documento `docs/architecture/foundry-pattern-migration-inventory.md` con TODAS las
referencias a Temporal en el repo: imports, deps, manifests, docs, tests, CI.

Steps:
1. Generar el inventario con `grep`/`find`. Para cada hit, anotar:
   - Tipo: import / dependency / manifest / doc / test / config
   - Path
   - Acción prevista: DELETE / REFACTOR / DOC-UPDATE
   Comandos:
     grep -rln 'temporal\|Temporal\|TEMPORAL' \
       --include='*.rs' --include='*.go' --include='*.toml' --include='*.mod' \
       --include='*.yaml' --include='*.yml' --include='*.md' --include='*.gotmpl' \
       --include='Justfile' --include='justfile' \
       . 2>/dev/null | grep -v 'docs_original_palantir_foundry\|target/\|.tgz' | sort -u
2. Categorizar por:
   - libs/temporal-client/ (DELETE entera)
   - workers-go/ (DELETE entera, o REFACTOR a Kafka consumers Go puros)
   - services/*/Cargo.toml deps (DELETE temporal-client dep)
   - services/*/src/**/temporal_*.rs (DELETE archivos)
   - infra/helm/infra/temporal/ (DELETE entera)
   - infra/helm/operators/* (no hay específico)
   - .github/workflows/ (revisar y limpiar)
   - justfile (limpiar recipes go-test, go-worker, etc.)
   - Cassandra keyspaces (drop temporal_*)
   - docs (varios update)
3. Documentar en formato tabla markdown:
     | Path | Tipo | Acción | Tarea responsable |
     | ---- | ---- | ------ | ----------------- |
     | libs/temporal-client/Cargo.toml | rust-deps | DELETE | Tarea 8.1 |
     ...
4. Total esperado de paths: ~80-150. Si sale <50, has buscado mal; vuelve a grep.

Verification:
- `wc -l docs/architecture/foundry-pattern-migration-inventory.md` ≥ 150.
- Cross-reference: cada Tarea 3-9 del plan tiene paths del inventario asignados.

Failure modes:
- Olvidar tests `_temporal_*.rs` o `*temporal*_test.go`. Buscar también con `find . -name '*temporal*'`.
- Olvidar CI workflows: `.github/workflows/go-workers.yml` está obvio, pero también `.github/workflows/ci.yml` puede tener cache de go modules de `workers-go/`.
```

### Tarea 0.3 — Actualizar plan de migración Cassandra-Foundry-parity

```text
Context: docs/architecture/migration-plan-cassandra-foundry-parity.md menciona Temporal
explícitamente en §0, §2, §14. Esos puntos quedan obsoletos.

Goal: editar el plan existente para reflejar nueva dirección, manteniendo las decisiones
sobre Cassandra/Iceberg/Vespa/Postgres-consolidation que siguen vigentes.

Steps:
1. Leer migration-plan-cassandra-foundry-parity.md completo.
2. Editar §0 "Resumen del cambio arquitectónico":
   - "Temporal HA (frontend×3, history×3, ...)" → ELIMINAR. Reemplazar por "Orquestación
     distribuida estilo Foundry: Spark Operator + Kafka consumers + Postgres state
     machines + outbox/Debezium (ver ADR-0027)".
   - "Workers de negocio Temporal en Go" → "Workers de negocio en Rust (Kafka consumers)
     o Go (cuando justifique)".
3. Editar §2 "Inventario de lo que se elimina o reemplaza":
   - "scheduler casero" entries: revisar; muchos de esos eran a borrar EN FAVOR de Temporal.
     Ahora se REESCRIBEN como state machines + cron + Kafka consumers (no se borran).
4. Editar §14 "Estructura de carpetas resultante":
   - `services/workflow-automation-worker/` → eliminar como worker Temporal, mantener como
     servicio Rust con consumers Kafka.
   - `libs/workflow-kernel/` → eliminar (era para Temporal helpers); reemplazar por
     `libs/state-machine/` + `libs/saga/`.
   - `infra/k8s/temporal/` → eliminar mención; reemplazar por SparkApplication CRs.
5. Añadir §20 "Migration to Foundry-pattern (ADR-0027)":
   - Status: in progress
   - Plan: link a este migration-plan-foundry-pattern-orchestration.md
   - Tareas pendientes: count
6. Commit: `docs(plan): update Cassandra-Foundry-parity plan to reflect Foundry-pattern orchestration`

Verification:
- `grep -c 'Temporal' docs/architecture/migration-plan-cassandra-foundry-parity.md` ≤ 5
  (residual references como "ADR-0021 (superseded)").
- `grep -c 'Foundry-pattern\|ADR-0027' docs/architecture/migration-plan-cassandra-foundry-parity.md` ≥ 3.

Failure modes:
- No tocar las secciones sobre Cassandra modeling, Vespa, Iceberg DR — esas siguen 100% válidas.
```

### Tarea 0.4 — ADR-0038: contrato outbox + idempotencia

```text
Context: Patrón Foundry depende fuertemente de outbox transaccional + Debezium + idempotencia
en consumers. ADR-0022 ya cubre outbox; necesitamos un ADR específico que enuncie el contrato
de event_id determinista + retry safety.

Goal: ADR-0038 que documenta convenciones de eventos: schema, event_id determinista,
idempotency key, dead-letter handling, retry policies.

Steps:
1. Crear `docs/architecture/adr/ADR-0038-event-contract-and-idempotency.md`.
2. Contenido mínimo:
   - Status: Accepted
   - Context: tras ADR-0037 todo workflow es event-driven; necesitamos contrato uniforme.
   - Decision:
     - Schema: cada evento usa Avro/JSON-Schema en Apicurio Registry.
     - Estructura común: `event_id`, `event_type`, `aggregate_id`, `aggregate_type`,
       `occurred_at`, `correlation_id`, `causation_id`, `payload`.
     - `event_id` = UUID v5 sobre `(aggregate_type, aggregate_id, version, event_type)` →
       eventos duplicados producidos por outbox→Debezium retry son detectables.
     - Consumers MUST ser idempotentes: tabla `processed_events(event_id PK, processed_at)`
       en Postgres del consumer (o `cassandra.processed_events` con TTL para alto volumen).
     - DLQ topic: `__dlq.<original-topic>` con max retries 5, backoff exponencial.
     - Retry policy: in-app retries finitas (3-5) → DLQ → on-call review.
   - Consequences: positivas (cada consumer es seguro de retries), negativas (storage
     overhead de processed_events).
3. Commit: `docs(adr): add ADR-0038 event contract and idempotency`

Verification: `head -50 docs/architecture/adr/ADR-0038-event-contract-and-idempotency.md`
muestra el documento completo.

Failure modes: ninguno; documentación.
```

---

## FASE 1 — Librerías comunes Rust

### Tarea 1.1 — `libs/state-machine`: helper para state machines en Postgres

```text
Context: Refactor de approvals + workflow-automation requiere state machines persistidos en
Postgres. Necesitamos helper común para no repetir transición/retry/timeout logic en cada
servicio.

Goal: nuevo crate `libs/state-machine/` con trait `StateMachine`, helpers de transición
atómica vía sqlx, retry con backoff, timeout via expires_at column.

Steps:
1. Crear `libs/state-machine/` con `Cargo.toml` heredando workspace deps. Añadir:
     [dependencies]
     async-trait = { workspace = true }
     chrono     = { workspace = true }
     serde      = { workspace = true }
     serde_json = { workspace = true }
     sqlx       = { workspace = true, features = ["postgres", "runtime-tokio-rustls", "chrono", "uuid"] }
     thiserror  = { workspace = true }
     tokio      = { workspace = true }
     tracing    = { workspace = true }
     uuid       = { workspace = true }
2. Definir API en `src/lib.rs`:
     pub trait StateMachine: Sized {
       type State: Copy + Eq;
       type Event;
       fn transition(self, event: Self::Event) -> Result<Self, TransitionError>;
       fn current_state(&self) -> Self::State;
       fn aggregate_id(&self) -> Uuid;
     }

     pub struct PgStore<T: StateMachine> { pool: PgPool, table: &'static str, ... }
     impl<T> PgStore<T> {
       pub async fn load(&self, id: Uuid) -> Result<T>;
       pub async fn apply(&self, machine: T, event: T::Event) -> Result<T>;  // atomic
       pub async fn timeout_sweep(&self, now: DateTime<Utc>) -> Vec<T>;       // cron
     }
3. SQL pattern: cada state machine type tiene tabla con columnas estándar:
     id           UUID PRIMARY KEY,
     state        TEXT NOT NULL,
     state_data   JSONB NOT NULL,
     version      BIGINT NOT NULL DEFAULT 1,  -- optimistic concurrency
     expires_at   TIMESTAMPTZ,                 -- for timeout sweeps
     created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
     updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
   Transición atómica = UPDATE WHERE id=? AND version=? RETURNING ... .
4. Tests: `tests/state_machine_test.rs` con testcontainers Postgres. Cubrir:
   - happy path transición
   - concurrent update detection (optimistic lock)
   - timeout_sweep returns expired rows
   - invalid transition errors
5. Documentar API en `libs/state-machine/README.md` con ejemplo de uso (`ApprovalState`).
6. Añadir crate a workspace `Cargo.toml`.

Verification:
- `cargo test -p state-machine` pasa.
- `cargo doc -p state-machine --no-deps` genera docs sin warnings.

Failure modes:
- Optimistic lock race: si 2 procesos intentan transicionar simultáneamente, uno falla con
  `TransitionError::Stale`. Tests deben cubrirlo.
- expires_at indexing: añadir índice parcial `WHERE expires_at IS NOT NULL` en migrations.
```

### Tarea 1.2 — `libs/saga`: helper para saga choreography

```text
Context: automation-ops y workflow-automation usan saga (steps + compensations). Necesitamos
helper para emitir step events + compensaciones via outbox transaccional.

Goal: nuevo crate `libs/saga/` con trait `SagaStep`, registry de compensaciones, helper
para publicar step.completed/failed events con compensation chain via outbox.

Steps:
1. Crear `libs/saga/` con Cargo.toml workspace deps + sqlx + uuid + chrono + serde.
2. API en `src/lib.rs`:
     pub trait SagaStep {
       type Input: Serialize;
       type Output: Serialize;
       fn step_name() -> &'static str;
       async fn execute(input: Self::Input) -> Result<Self::Output, SagaError>;
       async fn compensate(input: Self::Input) -> Result<(), SagaError>;
     }

     pub struct SagaRunner<'a> { tx: &'a mut Transaction<'_, Postgres>, saga_id: Uuid, ... }
     impl SagaRunner {
       pub async fn execute_step<S: SagaStep>(...) -> Result<S::Output>;
       // On error, publishes <saga.step.failed> + chain of compensation events
     }

     // Outbox helper integrated:
     pub async fn enqueue_outbox_event<E: Serialize>(
       tx: &mut Transaction<'_, Postgres>,
       topic: &str, key: &[u8], payload: &E
     ) -> Result<Uuid>;
3. Schema asumido: `outbox.events` ya existe en pg-policy bootstrap (verificar; si no,
   crearlo en Tarea 2.1).
   Schema:
     CREATE TABLE outbox.events (
       id UUID PRIMARY KEY,
       topic TEXT NOT NULL,
       key BYTEA NOT NULL,
       payload JSONB NOT NULL,
       headers JSONB DEFAULT '{}',
       enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now()
     );
4. Tests: `tests/saga_test.rs` con testcontainers Postgres + mock Kafka.
   - Saga happy path: 3 steps, todos OK
   - Step 2 falla: compensación step 1 ejecuta
   - Idempotencia: re-ejecutar saga con mismo saga_id no duplica steps
5. README con ejemplo: saga "create order" → reserve inventory → charge card → ship.

Verification:
- `cargo test -p saga` pasa.
- Ejemplo del README compila como integration test.

Failure modes:
- Outbox table debe existir antes de los tests; usar testcontainers init scripts.
- Saga state debe persistirse: tabla `saga_state(saga_id, current_step, completed_steps[], failed_step)`.
```

### Tarea 1.3 — `libs/event-scheduler`: cron-based event emission

```text
Context: Patrón Foundry necesita disparadores time-based (Foundry Schedule). Reemplaza
los tick loops in-process con K8s CronJobs que publican events a Kafka.

Goal: nuevo crate `libs/event-scheduler/` con tooling para construir un binario Rust que
lea config de schedules de Postgres + tabla `schedules.definitions` y publique events
correspondientes a Kafka. Un único pod CronJob ejecuta cada minuto.

Steps:
1. Crear `libs/event-scheduler/` con deps: sqlx, rdkafka (vía event-bus-data), tokio,
   chrono, cron (crate `cron = "0.12"`).
2. API:
     pub struct Scheduler { pg: PgPool, kafka: KafkaProducer, ... }
     impl Scheduler {
       pub async fn tick(&self, now: DateTime<Utc>) -> Result<usize> {
         // 1. SELECT * FROM schedules.definitions WHERE next_run_at <= now AND enabled
         // 2. Para cada match: emit event to kafka topic, UPDATE next_run_at via cron expr
         // 3. Return number of fires
       }
     }
3. Schema Postgres `schedules.definitions`:
     CREATE TABLE schedules.definitions (
       id UUID PRIMARY KEY,
       name TEXT UNIQUE NOT NULL,
       cron_expr TEXT NOT NULL,
       enabled BOOLEAN NOT NULL DEFAULT true,
       topic TEXT NOT NULL,
       payload_template JSONB NOT NULL,
       next_run_at TIMESTAMPTZ NOT NULL,
       last_run_at TIMESTAMPTZ
     );
4. Binario `schedules-tick` en `bin/schedules-tick.rs`: lee env, conecta, llama tick() una
   vez, sale.
5. Tests con testcontainers: 3 schedules con cron diferentes, tick() emite los correctos.

Verification:
- `cargo test -p event-scheduler` pasa.
- Binary `cargo run --bin schedules-tick` (con env apuntando a Postgres dev) emite events
  correctos verificable con `kafkacat`.

Failure modes:
- Race condition si CronJob k8s solapa runs (puede pasar si tick > 60s). Solución: SELECT
  FOR UPDATE SKIP LOCKED por schedule.
- Cron expression parsing: usar crate `cron` con timezone awareness.
```

### Tarea 1.4 — `libs/idempotency`: deduplication helper

```text
Context: ADR-0038 exige idempotencia en todos los consumers. Helper común evita reimplementar.

Goal: crate `libs/idempotency/` con trait `IdempotencyStore` + impls Postgres y Cassandra.

Steps:
1. Crear `libs/idempotency/` con sqlx + scylla deps.
2. API:
     #[async_trait]
     pub trait IdempotencyStore {
       async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome>;
       // Outcome::AlreadyProcessed | Outcome::FirstSeen
     }

     pub struct PgIdempotencyStore { pool: PgPool, table: &'static str }
     pub struct CassandraIdempotencyStore { session: Arc<Session>, ks_table: &'static str }
3. Schema Postgres:
     CREATE TABLE <schema>.processed_events (
       event_id UUID PRIMARY KEY,
       processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
     );
   Schema Cassandra:
     CREATE TABLE <ks>.processed_events (
       event_id UUID PRIMARY KEY,
       processed_at TIMESTAMP
     ) WITH default_time_to_live = 2592000;  -- 30 days TTL
4. Wrapper helper:
     pub async fn idempotent<F, Fut, T>(store: &dyn IdempotencyStore, event_id: Uuid, f: F) -> Result<T>
       where F: FnOnce() -> Fut, Fut: Future<Output = Result<T>>;
5. Tests: replay event → segunda llamada returns AlreadyProcessed.

Verification: `cargo test -p idempotency` pasa con testcontainers Postgres + Cassandra.

Failure modes:
- TTL en Cassandra debe ser ≥ retención del Kafka topic; si TTL < retention, eventos
  expirados podrían reprocesarse al reload tras outage.
```

---

## FASE 2 — Validar que outbox + Debezium + Kafka funcionan extremo a extremo

### Tarea 2.1 — Verificar/crear schema `outbox.events` en pg-policy

```text
Context: ADR-0022 declara outbox transaccional en pg-policy. Hay que verificar que el
schema y tabla existen en el bootstrap-sql, y crearlo si no.

Goal: bootstrap-sql de pg-policy crea schema `outbox` con tabla `events` y rol
`debezium_cdc` con SELECT en outbox.

Steps:
1. Revisar
   `infra/helm/infra/postgres-clusters/templates/clusters/pg-policy-bootstrap-sql.yaml`.
2. Si no existe `CREATE SCHEMA outbox` ni `CREATE TABLE outbox.events`, añadir bloque:
     CREATE SCHEMA IF NOT EXISTS outbox;
     CREATE TABLE IF NOT EXISTS outbox.events (
       id UUID PRIMARY KEY,
       topic TEXT NOT NULL,
       key BYTEA NOT NULL,
       payload JSONB NOT NULL,
       headers JSONB DEFAULT '{}',
       enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
       aggregate_type TEXT NOT NULL,
       aggregate_id TEXT NOT NULL
     );
     CREATE INDEX ON outbox.events (enqueued_at);
     -- Each svc_<bc> role gets INSERT; debezium_cdc gets SELECT (already in policy).
     GRANT USAGE ON SCHEMA outbox TO PUBLIC;
     GRANT INSERT ON outbox.events TO PUBLIC;  -- restrict via row-level if needed
3. Ajustar Cluster CR para `wal_level: logical` en dev también (necesario para Debezium).
   Editar `infra/helm/infra/postgres-clusters/values-dev.yaml`:
     pgPolicy:
       walLevel: logical   # was replica
       postgresql:
         maxReplicationSlots: "8"
         maxWalSenders: "10"
4. Re-aplicar: `helmfile -e dev apply --selector name=postgres-clusters --skip-deps`.
   CNPG hará rolling restart del pod.
5. Verificar: psql como svc_audit_compliance → INSERT INTO outbox.events VALUES (...).

Verification:
- `kubectl exec pg-policy-1 -- psql -U postgres -d app -c '\dt outbox.*'` muestra outbox.events.
- `kubectl exec pg-policy-1 -- psql -U postgres -d app -c 'SHOW wal_level'` returns "logical".

Failure modes:
- wal_level change requires Postgres restart; CNPG handles. Si tarda, `kubectl rollout
  status statefulset/pg-policy-1`.
```

### Tarea 2.2 — Habilitar Debezium connector outbox-pg-policy

```text
Context: `infra/helm/infra/debezium/templates/kafka-connector-outbox-pg-policy.yaml` ya
existe. Hay que asegurar que el chart se aplica, KafkaConnect cluster funciona, y el
connector está running.

Goal: Debezium leyendo de pg-policy.outbox.events → publicando a topics Kafka según
EventRouter SMT.

Steps:
1. Habilitar debezium en helmfile dev: cambiar
   `{{- $debeziumInstalled := not $isDev -}}` → `{{- $debeziumInstalled := true -}}`.
2. Revisar chart `infra/helm/infra/debezium/`:
   - kafka-connect.yaml: KafkaConnect CR (Strimzi). 1 worker en dev.
   - kafka-connector-outbox-pg-policy.yaml: KafkaConnector CR with EventRouter SMT.
   - kafka-user-debezium-connect.yaml: KafkaUser CR.
   - prometheus-rules.yaml: gated (observability disabled in dev).
3. Crear secret con credenciales de pg-policy debezium_cdc role:
     DEBEZIUM_PASS=$(grep "^debezium_cdc_pass=" infra/helm/.dev-secrets/.dev-secrets.env | cut -d= -f2)
     kubectl -n kafka create secret generic debezium-pg-policy-creds \
       --from-literal=username=debezium_cdc \
       --from-literal=password=$DEBEZIUM_PASS
4. Apply: `helmfile -e dev apply --selector name=debezium --skip-deps`.
5. Esperar KafkaConnect ready:
     kubectl -n kafka wait --for=condition=Ready kafkaconnect/openfoundry-connect --timeout=600s
6. Esperar KafkaConnector connected:
     kubectl -n kafka get kafkaconnector
7. Smoke test:
   - INSERT INTO pg-policy.outbox.events with topic='audit.events.v1'
   - kafkacat consume topic 'audit.events.v1' → ver el mensaje

Verification:
- `kubectl -n kafka get kafkaconnector outbox-pg-policy -o jsonpath='{.status.connectorStatus.connector.state}'` = "RUNNING".
- kafkacat consume del topic muestra mensajes inyectados via outbox.

Failure modes:
- Replication slot: si Debezium se reinicia frecuentemente, slots WAL se acumulan.
  Monitorear con `SELECT * FROM pg_replication_slots` en pg-policy.
- Connector "FAILED": logs en `kubectl -n kafka logs -l strimzi.io/cluster=openfoundry-connect`.
- TLS: dev no usa TLS, prod sí. Verificar que `tls.enabled: false` en values dev.
```

### Tarea 2.3 — Crear KafkaTopic CRs para events del nuevo patrón

```text
Context: Nuevo patrón Foundry necesita topics específicos. Lista mínima:
- automate.condition.v1, automate.outcome.v1
- approval.events.v1
- ontology.changes.v1, ontology.reindex.v1
- saga.step.v1
- pipeline.builds.v1
- audit.events.v1
- __dlq.* per topic
+ outbox routing: pg-policy CDC publica a un topic `pg-policy.outbox.<aggregate_type>` que
  se enruta a topic final via SMT.

Goal: KafkaTopic CRs declarados en `infra/helm/infra/kafka-cluster/templates/kafka-topics.yaml`
para los topics arriba con replicas/partitions/retention apropiados.

Steps:
1. Editar `infra/helm/infra/kafka-cluster/templates/kafka-topics.yaml` (currently gated off
   in dev). Para cada topic:
     apiVersion: kafka.strimzi.io/v1beta2
     kind: KafkaTopic
     metadata:
       name: automate.condition.v1
       namespace: kafka
       labels:
         strimzi.io/cluster: openfoundry
     spec:
       partitions: 6
       replicas: 1     # dev; prod=3 via values overlay
       config:
         retention.ms: 604800000   # 7 days
         compression.type: zstd
2. Lista completa (15-20 topics). Estructura el archivo con un loop helm template
   alimentado por values.yaml `topics:` list.
3. Habilitar `topics: { enabled: true }` y `acls: { enabled: true }` en values-dev.yaml.
4. Apply: `helmfile -e dev apply --selector name=kafka-cluster --skip-deps`.
5. Verificar: `kubectl -n kafka get kafkatopic` muestra todos.

Verification:
- Todas las KafkaTopics en estado `Ready=True`.
- `kubectl -n kafka exec openfoundry-kafka-0 -- bin/kafka-topics.sh --bootstrap-server :9092 --list`
  muestra los topics.

Failure modes:
- Partitions count cambia → requiere drop topic. Empezar con número conservador (6 dev, 12 prod).
- Retention: 7 días dev OK; prod 30 días para audit.events, 14 days para outcomes.
```

### Tarea 2.4 — End-to-end smoke test outbox → Debezium → Kafka → consumer

```text
Context: Validar el backbone completo antes de empezar el refactor de workers.

Goal: Test integración que desde un servicio Rust (cualquiera con DB en pg-policy) hace
INSERT en outbox.events DENTRO de una transacción, y un consumer Kafka recibe el evento
en el topic correcto.

Steps:
1. Escribir test en `services/audit-compliance-service/tests/outbox_e2e.rs` (o servicio
   equivalente con DB en pg-policy):
     - tx.begin()
     - INSERT INTO audit_compliance.events (...)
     - INSERT INTO outbox.events (id, topic, key, payload, ...)
     - tx.commit()
     - kafka_consumer.poll(topic='audit.events.v1', timeout=10s)
     - assert event_id matches
2. Test usa testcontainers Postgres con bootstrap-sql aplicado + Strimzi/Kafka real (lima).
3. Si el test pasa: outbox+Debezium+Kafka backbone está validado para el resto del refactor.

Verification:
- `cargo test -p audit-compliance-service --features it-debezium outbox_e2e -- --ignored`
  pasa en lima.

Failure modes:
- Latencia outbox→Kafka: Debezium puede tardar 5-30s la primera vez. Test timeout = 60s.
- Si el connector está paused, `kafkaconnector` lo muestra. Resume con kubectl annotate.
```

---

## FASE 3 — Refactor `pipeline-worker` → SparkApplication

### Tarea 3.1 — Inventario funcional de pipeline-worker

```text
Context: workers-go/pipeline/workflows/pipeline_run.go orquesta el build y ejecución de
pipelines. Antes de migrar a Spark, hay que entender qué hace exactamente.

Goal: documento `docs/architecture/refactor/pipeline-worker-inventory.md` con:
- Lista de workflows definidos (PipelineRun, ...)
- Lista de activities llamadas (qué servicios Rust)
- Inputs/outputs (de pipeline-build-service, pipeline-schedule-service)
- Tipos de trabajo: build de DAG, ejecución, scheduling

Steps:
1. Leer todo workers-go/pipeline/.
2. Mapear cada activity Go a HTTP endpoint Rust llamado.
3. Mapear cada workflow a "qué Spark transformation equivalente sería".
4. Identificar cuál parte es:
   - Spark batch puro → SparkApplication CR
   - Coordinación entre Spark jobs → DAG via SparkApplication dependsOn
   - User-triggered → API en pipeline-build-service que crea SparkApp CR
   - Scheduled → CronJob k8s que crea SparkApp CR

Verification: documento en `docs/architecture/refactor/` con tabla de migración.

Failure modes:
- Si pipeline_run.go usa Temporal-specific features (signals, queries) que no mapean a
  Spark, documentar e investigar workaround. Probable: Workshop UI para queries vivos.
```

**Status:** done — see [`docs/architecture/refactor/pipeline-worker-inventory.md`](refactor/pipeline-worker-inventory.md)
for the full inventory (workflows, activities, HTTP endpoints, per-node
transform dispatch, trigger surfaces, and the Temporal → Foundry-pattern
migration table). Headline finding: `PipelineRun` uses no signals,
queries, child workflows, timers, or `ContinueAsNew`, so no
"Workshop-UI-for-live-queries" workaround is needed.

### Tarea 3.2 — Diseñar SparkApplication CRs templating

```text
Context: pipeline-build-service necesita crear SparkApplication CRs dinámicamente cuando
un usuario invoca "run pipeline". Cada pipeline = 1 SparkApplication.

Goal: helm chart helper en infra/helm/infra/spark-jobs/ con SparkApplication template
parametrizable (input dataset, output dataset, transform code, resources). pipeline-build-
service llama K8s API para crear el CR.

Steps:
1. Revisar `infra/helm/infra/spark-jobs/templates/` actual: `iceberg-rewrite-data-files.yaml`,
   `iceberg-expire-snapshots.yaml`, `metrics-aggregation-service-daily.yaml`. Estos son
   maintenance jobs.
2. Añadir `infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml` con un
   SparkApplication que use variables (no helm, sino k8s downward/env):
     apiVersion: sparkoperator.k8s.io/v1beta2
     kind: SparkApplication
     metadata:
       name: pipeline-run-${pipeline_id}-${run_id}
       namespace: openfoundry
     spec:
       type: Scala  # or Python, depending on pipeline-build-service code
       mode: cluster
       image: openfoundry/pipeline-runner:0.1.0  # we'll build this image in 3.3
       mainClass: com.openfoundry.pipeline.PipelineRunner
       arguments:
         - "--pipeline-id"
         - "${pipeline_id}"
         - "--input-dataset"
         - "${input_dataset_rid}"
         - "--output-dataset"
         - "${output_dataset_rid}"
       sparkConf:
         "spark.sql.catalog.iceberg": "org.apache.iceberg.spark.SparkCatalog"
         "spark.sql.catalog.iceberg.type": "rest"
         "spark.sql.catalog.iceberg.uri": "http://lakekeeper.lakekeeper.svc:8181"
       driver:
         cores: 1
         memory: "1g"
       executor:
         cores: 1
         instances: 2
         memory: "2g"
3. pipeline-build-service tendrá un SparkApplication generator que rellena las variables
   antes de POST al k8s API.
4. Documentar el template + variables en `infra/helm/infra/spark-jobs/templates/README.md`.

Verification:
- `kubectl apply --dry-run=client -f` (con vars rellenas manualmente) renderiza válidamente.

Failure modes:
- Spark image debe tener Iceberg + Lakekeeper REST client + tu lib de transformaciones.
  Tarea 3.3 lo construye.
```

### Tarea 3.3 — Construir imagen `pipeline-runner` (Spark + Iceberg + transforms)

```text
Context: La imagen pipeline-runner ejecuta el código Spark. Debe contener Spark 3.5+,
Iceberg connector, Lakekeeper REST catalog client, y código de transforms (Scala/Python).

Goal: imagen `localhost:5001/pipeline-runner:0.1.0` en local registry, ejecutable como
SparkApplication.

Steps:
1. Crear `services/pipeline-runner/` (nuevo):
     services/pipeline-runner/
       Dockerfile
       build.sbt           # if Scala
       src/main/scala/com/openfoundry/pipeline/PipelineRunner.scala
       (or pyproject.toml + main.py if Python)
2. Dockerfile basado en `apache/spark:3.5.4-scala2.12-java17-python3-ubuntu` o similar.
   COPY iceberg-spark-runtime jar a /opt/spark/jars/.
   COPY el código de transforms.
3. PipelineRunner main reads CLI args (--pipeline-id, --input-dataset, --output-dataset),
   loads transform definition from pipeline-build-service via HTTP, executes Spark SQL,
   writes output to Iceberg.
4. Build & push: `docker buildx build --platform linux/arm64 \
   -t 192.168.105.3:30501/pipeline-runner:0.1.0 --push services/pipeline-runner/`.

Verification:
- `kubectl -n openfoundry run pipeline-test --rm --image=localhost:5001/pipeline-runner:0.1.0 -- spark-submit --version`
  muestra Spark 3.5.x.
- SparkApplication con esa imagen y un transform mínimo (read 1 row, write 1 row) completa.

Failure modes:
- Tamaño imagen: Spark base es ~600MB. Multi-stage build para reducir.
- Iceberg version compatibility: Spark 3.5 + Iceberg 1.5+; pin específico.
```

### Tarea 3.4 — Refactor `pipeline-build-service` para crear SparkApplication CRs

```text
Context: El servicio Rust pipeline-build-service hoy llama Temporal SDK. Debe pasar a
crear SparkApplication CRs via Kubernetes API.

Goal: pipeline-build-service usa k8s_openapi + kube-rs para POST SparkApplication CR
cuando recibe `POST /api/v1/pipeline/builds/run`.

Steps:
1. Cargo.toml: añadir kube = { workspace = true }, k8s-openapi.
2. Eliminar `temporal-client` de deps.
3. Eliminar archivos `services/pipeline-build-service/src/domain/temporal_*.rs` si existen.
4. Nuevo módulo `services/pipeline-build-service/src/spark.rs`:
     pub async fn submit_pipeline_run(client: kube::Client, input: PipelineRunInput)
       -> Result<String /* spark_app_name */> {
       // build SparkApplication from template
       // POST via kube::Api::create
     }
5. Handler `POST /api/v1/pipeline/builds/run` → llama submit_pipeline_run + persiste
   (pipeline_run_id, spark_app_name, status='submitted') en pg-runtime-config.pipeline_authoring.
6. Watch endpoint: `GET /api/v1/pipeline/builds/:run_id/status` → query SparkApplication
   status via kube::Api::get.
7. Tests integración con kube fake client.
8. RBAC: deploy-time, of-platform chart needs to grant pipeline-build-service SA permission
   to create/get/watch SparkApplications in openfoundry namespace.

Verification:
- `cargo test -p pipeline-build-service` pasa.
- `curl -X POST .../api/v1/pipeline/builds/run -d '{"pipeline_id":"..."}'`  →
  `kubectl get sparkapplication` muestra el CR creado.

Failure modes:
- Permisos RBAC: pipeline-build-service SA debe tener Role con verbs
  ['create','get','list','watch'] sobre sparkapplications.
- Service quota: namespace puede tener LimitRange que rechaza pods con >X memoria. Ajustar.
```

### Tarea 3.5 — Refactor `pipeline-schedule-service` reemplazando temporal_schedule

```text
Context: pipeline-schedule-service tiene dominio Temporal Schedule, varios handlers,
y tests con temporal-client. Reemplazar por CronJob k8s + libs/event-scheduler.

Goal: pipeline-schedule-service usa libs/event-scheduler para gestionar schedules en
postgres, y K8s CronJob `pipeline-scheduler-tick` ejecuta el binario `schedules-tick`
cada minuto.

Steps:
1. Eliminar archivos:
   - services/pipeline-schedule-service/src/domain/temporal_schedule.rs
   - services/pipeline-schedule-service/src/handlers/temporal_schedule.rs
   - services/pipeline-schedule-service/tests/temporal_schedule_*.rs
2. Eliminar dep `temporal-client` de Cargo.toml.
3. Añadir dep `event-scheduler` (Tarea 1.3).
4. Reescribir src/main.rs: ahora es solo HTTP API (sin tick loop).
5. Refactor handlers/schedules_v2.rs para guardar/leer schedules en
   pg-runtime-config.pipeline_authoring.schedule_definitions (tabla del libs/event-scheduler).
6. Crear `infra/helm/apps/of-data-engine/templates/cronjob-pipeline-scheduler.yaml`:
     apiVersion: batch/v1
     kind: CronJob
     metadata:
       name: pipeline-scheduler-tick
     spec:
       schedule: "* * * * *"
       jobTemplate:
         spec:
           template:
             spec:
               containers:
                 - name: tick
                   image: localhost:5001/pipeline-schedule-service:0.1.0
                   command: ["/usr/local/bin/schedules-tick"]
                   env: { ... }
7. El binario `schedules-tick` (de libs/event-scheduler) lee schedule_definitions y
   publica a topic `pipeline.scheduled.v1`. pipeline-build-service consume ese topic
   y dispara SparkApplication.

Verification:
- `cargo test -p pipeline-schedule-service` pasa sin temporal.
- `kubectl get cronjob pipeline-scheduler-tick` exists.
- Crear schedule via API, esperar 1-2 minutos, ver SparkApplication creado.

Failure modes:
- CronJob suspend: Helm hooks deben gestionar correctamente. Documentar.
- DST / timezone: cron en `Europe/Madrid` requiere CronJob field `timeZone` (k8s 1.27+).
```

### Tarea 3.6 — Eliminar `workers-go/pipeline/`

```text
Context: Tras refactor Spark + scheduler, el worker Go ya no se usa.

Goal: directorio eliminado, justfile limpio, CI workflow actualizado, plan migration plan §2 actualizado.

Steps:
1. `git rm -rf workers-go/pipeline`
2. Remove de `workers-go/go.work` la entry pipeline.
3. Edit `justfile` recipe `go-worker` para no listar pipeline.
4. Edit `.github/workflows/go-workers.yml` matrix: remove pipeline.
5. Update `migration-plan-cassandra-foundry-parity.md` §2: marcar como done.
6. Commit: `refactor(pipeline): replace Temporal worker with Spark Operator + scheduler`.

Verification:
- `helmfile -e dev lint` pasa.
- `go work sync` en workers-go/ no protesta.
- CI green.

Failure modes: ninguno; cleanup.
```

### Tarea 3.7 — Tests integración pipeline end-to-end

```text
Context: Validar que el flujo completo funciona: API call → SparkApplication → Iceberg
write.

Goal: test integración en `services/pipeline-build-service/tests/spark_e2e.rs` que:
1. POST /run con un pipeline mínimo
2. Espera el SparkApp en estado COMPLETED (timeout 5min)
3. Lee Iceberg table output y valida row count

Steps:
1. Test usa cluster lima real (no testcontainers — Spark Operator necesita k8s real).
2. Marcar `#[ignore]` por defecto, ejecutar con `cargo test --ignored` en CI staging.
3. Smoke pipeline: lee 10 rows de un dataset fixture en MinIO, escribe a otra tabla
   Iceberg, valida.

Verification: test pasa en CI staging tras Phase 9 helmfile dev apply completo.

Failure modes:
- Spark exec time variable: timeout generoso (10min) y assertions específicas.
```

---

## FASE 4 — Refactor `reindex-worker` → Kafka consumer puro

### Tarea 4.1 — Inventario reindex-worker

```text
Context: reindex-worker pagina Cassandra y publica a Kafka. Es ya casi un consumer puro,
solo envuelto en Temporal por ADR-0021.

Goal: documento que mapea las activities Go (ScanCassandra, PublishBatch) a un consumer
Rust o Go simple.

Steps:
1. Leer workers-go/reindex/workflows/reindex.go + activities.
2. Documentar inputs (TenantID, TypeID, ResumeToken) y outputs (count, status).
3. Mapear a:
   - Trigger: event en `ontology.reindex.requested.v1`
   - Consumer: pagina Cassandra con cursor en Postgres (resume-safe), publica batches a
     `ontology.reindex.v1`.
4. Identificar dónde el cursor se persiste hoy (ResumeToken in workflow state) →
   reemplazar por tabla `pg-runtime-config.reindex_jobs`.

Verification: documento `docs/architecture/refactor/reindex-worker-inventory.md`.

Failure modes: ninguno; planning.
```

### Tarea 4.2 — Implementar `services/reindex-coordinator-service` Rust

```text
Context: El reindex actual es un workflow Temporal en Go. Migrar a un servicio Rust con
consumer Kafka que paginate Cassandra.

Goal: nuevo (o renombrado) servicio Rust `reindex-coordinator-service` que:
- Consume topic `ontology.reindex.requested.v1`
- Inicia un job paginando Cassandra
- Persiste estado en pg-runtime-config.reindex_jobs (resumeable)
- Publica batches a `ontology.reindex.v1`
- Marca job DONE/FAILED y publica `ontology.reindex.completed.v1`

Steps:
1. Crear `services/reindex-coordinator-service/` (o renombrar uno existente):
     Cargo.toml: deps cassandra-kernel, event-bus-data, sqlx, libs/idempotency.
     src/main.rs: HTTP server (control plane) + Kafka consumer loop.
     src/state.rs: state machine para job status (queued, running, completed, failed).
     src/scan.rs: paginación Cassandra usando token() based scan + lib cassandra-kernel.
2. Schema Postgres: pg-runtime-config.reindex_jobs(id, tenant_id, type_id, status,
   resume_token, started_at, completed_at).
3. Idempotencia: cada batch tiene event_id deterministic (UUID v5 sobre
   tenant_id||type_id||token).
4. Tests integración con Cassandra + Kafka real.

Verification:
- `kafkacat -P -t ontology.reindex.requested.v1` con un payload válido →
  `kafkacat -C -t ontology.reindex.v1` recibe batches.

Failure modes:
- Cassandra throughput: pagination debe respetar rate limit. Configurable via env.
- Restart safety: si el servicio crashea mid-scan, el resume_token persistido permite
  retomar.
```

### Tarea 4.3 — Eliminar `workers-go/reindex/`

```text
Mismo patrón que Tarea 3.6:
- git rm -rf workers-go/reindex
- update go.work
- update justfile, CI matrix
- update plan
- commit
```

### Tarea 4.4 — Verificar consumer downstream (Vespa indexer)

```text
Context: ontology-indexer consume `ontology.reindex.v1` y actualiza Vespa. Validar que
los nuevos batches del nuevo reindex-coordinator-service son compatibles con el formato
que ontology-indexer espera.

Goal: schema de eventos en topic verificado vía Apicurio, ontology-indexer consume y
actualiza Vespa correctamente.

Steps:
1. Definir/registrar JSON-Schema en Apicurio para `ontology.reindex.v1` event.
2. ontology-indexer carga schema en startup y valida.
3. Smoke test end-to-end.

Verification: tras una requested event, Vespa tiene los documentos esperados.
```

---

## FASE 5 — Refactor `workflow-automation-worker` → Automate-pattern

### Tarea 5.1 — Inventario workflow-automation

```text
Context: Es el más complejo: orquesta ontology actions con retries, espera entre pasos,
posible escalado. Equivale a Foundry "Automate" puro.

Goal: documento mapping cada AutomationRun input → patrón Automate equivalente:
- condition (cron + ontology change event)
- effect (HTTP call to ontology-actions-service)
- retry policy
- audit trail

Steps:
1. Leer workers-go/workflow-automation/workflows/automation_run.go completo.
2. Listar todos los tipos de "trigger payload" que el workflow maneja.
3. Mapear cada tipo a:
   - Source event topic (qué event lo dispara)
   - Effect call (qué endpoint llama)
   - State machine si es multi-step
4. Documentar estructura nueva del workflow-automation-service (era worker, será servicio
   con consumer + dispatcher).
```

### Tarea 5.2 — Diseñar tabla state machine `automation_runs`

```text
Context: Algunos automation runs son single-step (cond → effect). Otros pueden ser
multi-step (cond → effect1 → effect2). Necesitamos tabla genérica.

Goal: schema en pg-runtime-config.workflow_automation.automation_runs usando
libs/state-machine.

Steps:
1. Definir state enum: Queued, Running, Suspended, Completed, Failed, Compensating.
2. Schema:
     CREATE TABLE workflow_automation.automation_runs (
       id UUID PRIMARY KEY,
       tenant_id UUID NOT NULL,
       definition_id UUID NOT NULL,
       state TEXT NOT NULL,
       state_data JSONB NOT NULL,
       version BIGINT NOT NULL DEFAULT 1,
       expires_at TIMESTAMPTZ,
       correlation_id UUID NOT NULL,
       created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
       updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
     );
3. Migración sqlx en services/workflow-automation-service/migrations/.
```

### Tarea 5.3 — Refactor workflow-automation-service consumer + dispatcher

```text
Context: Servicio Rust nuevo (o adaptado) consume condition events, dispatcha effects.

Goal: servicio con 3 componentes:
- HTTP API (definir automations)
- Condition consumer (Kafka consumer de `automate.condition.v1`)
- Effect dispatcher (HTTP client a ontology-actions-service)
- Outcome publisher (publica a `automate.outcome.v1`)

Steps:
1. src/main.rs orquesta tokio tasks: HTTP server + Kafka consumer.
2. Consumer flow:
   - Receive condition event
   - idempotent check (libs/idempotency)
   - Load AutomationDefinition
   - Create automation_runs row (state=Running)
   - Call effect endpoint con timeout y retries
   - On success: state=Completed, publish outcome
   - On failure (retryable): re-enqueue with backoff (publish to delayed topic, or
     update expires_at and let scheduler re-emit)
   - On failure (non-retryable): state=Failed, publish outcome.failed
3. Tests integración + chaos test (effect endpoint 500s, eventually 200, verify retries).
```

### Tarea 5.4 — Eliminar workers-go/workflow-automation/

```text
Mismo patrón Tarea 3.6.
```

---

## FASE 6 — Refactor `automation-ops-worker` → Saga choreography

### Tarea 6.1 — Inventario automation-ops-worker

```text
Goal: catalog de los AutomationOpsTask tipos (cleanup, retention, etc.) y sus
compensaciones.
```

### Tarea 6.2 — Diseñar saga schema y events

```text
Goal: schema events `saga.step.requested.v1`, `saga.step.completed.v1`,
`saga.step.failed.v1`, `saga.compensate.v1`. Tabla `saga_state` en pg-policy.audit_compliance
para audit + dedup.
```

### Tarea 6.3 — Implementar saga steps usando libs/saga

```text
Goal: cada step = consumer Kafka que ejecuta su trabajo + emite outcome via outbox.
```

### Tarea 6.4 — Tests saga + chaos test

```text
Goal: validar happy path + paso N falla → compensaciones de N-1, N-2 se ejecutan en orden inverso.
```

### Tarea 6.5 — Eliminar workers-go/automation-ops/

---

## FASE 7 — Refactor `approvals-worker` → State machine + cron

### Tarea 7.1 — Inventario approvals

```text
Goal: tipos de approval (single approver, multi-approver, threshold-based, time-based escalation).
```

### Tarea 7.2 — Schema `approval_requests` state machine

```text
Goal: tabla en pg-policy.audit_compliance con states (Pending, Approved, Rejected, Expired,
Escalated). Indexes en (state, expires_at).
```

### Tarea 7.3 — Refactor approvals-service usando libs/state-machine

```text
Goal: handlers HTTP (create, approve, reject) + Kafka consumer para events de "manager
decided" + CronJob para timeouts/escalations.
```

### Tarea 7.4 — CronJob para timeout sweep

```text
Goal: K8s CronJob que cada 5 min ejecuta libs/state-machine::timeout_sweep y publica
`approval.expired.v1` events.
```

### Tarea 7.5 — Eliminar workers-go/approvals/

---

## FASE 8 — Service-side cleanup (Rust)

### Tarea 8.1 — Eliminar `libs/temporal-client/`

```text
Context: Tras Tareas 3-7, ningún servicio importa temporal-client.

Goal: crate eliminado del workspace, todos los imports rotos detectados y arreglados.

Steps:
1. `git rm -rf libs/temporal-client/`
2. Remove from workspace Cargo.toml members.
3. `cargo build --workspace` — debe fallar si algo aún importa.
4. Para cada error de compile: identificar el servicio, agendar fix en su tarea de fase 3-7.
5. Commit cuando `cargo build --workspace` pasa: "refactor: remove libs/temporal-client".
```

### Tarea 8.2 — Eliminar todos los archivos `temporal_*.rs` y `*temporal*` en servicios

```text
Goal: ningún archivo Rust con `temporal` en path o contenido.

Steps:
1. find services -name '*temporal*' -delete
2. grep -rln 'temporal' services/ | xargs sed -i '' '/temporal/d' # cuidado, revisar
3. cargo build pasa.
4. Commit por servicio: "refactor(<svc>): remove Temporal adapter".
```

### Tarea 8.3 — Cleanup deps en `Cargo.toml` workspace

```text
Goal: eliminar de workspace Cargo.toml:
- temporal-client = ...
- (cualquier dep que solo usaba temporal SDK)

Verify: cargo deny check pasa, cargo audit no tiene findings nuevos.
```

---

## FASE 9 — Infrastructure cleanup

### Tarea 9.1 — Eliminar `infra/helm/infra/temporal/` y release del helmfile

```text
Steps:
1. git rm -rf infra/helm/infra/temporal/
2. Edit infra/helm/helmfile.yaml.gotmpl: remove temporal release block, remove
   $temporalInstalled var, remove temporal repo entry.
3. helmfile -e dev lint pasa.
4. helmfile -e prod lint pasa.
5. Update infra/helm/README.md: quitar temporal de la lista.
6. Commit: "infra: remove Temporal helm release per ADR-0027".
```

### Tarea 9.2 — Drop Cassandra keyspaces `temporal_persistence` y `temporal_visibility`

```text
Steps:
1. kubectl exec on Cassandra pod with cqlsh:
     DROP KEYSPACE IF EXISTS temporal_persistence;
     DROP KEYSPACE IF EXISTS temporal_visibility;
2. Verificar con DESC KEYSPACES.
3. Document en runbook.

Failure modes: no aplica si el cluster Cassandra está vacío en dev.
```

### Tarea 9.3 — Eliminar `workers-go/` entera o reescribir como Kafka consumers Go

```text
Decisión: workers-go vacío tras Tareas 3-7 (todas eliminadas). Si decides mantener Go
para consumers de alto throughput, refactor cada uno a un worker Go puro con
github.com/twmb/franz-go o similar (sin Temporal SDK).

Para esta migración: ELIMINAR. Servicios Rust con event-bus-data cubren todos los casos.

Steps:
1. git rm -rf workers-go/
2. git rm -f .github/workflows/go-workers.yml
3. Edit justfile: remove go-* recipes.
4. Verify CI green.
```

### Tarea 9.4 — Update CI workflows

```text
Goal: .github/workflows/ sin referencias a temporal o workers-go.

Steps:
1. Edit .github/workflows/ci.yml: remove `workers-go` paths from triggers and matrix.
2. Edit .github/workflows/docker-publish.yml: ensure pipeline-runner image is in matrix.
3. Add new workflow .github/workflows/integration-foundry-pattern.yml that runs the
   end-to-end Spark + outbox + saga tests.
4. Local lint: act -j ci.
```

---

## FASE 10 — Documentación

### Tarea 10.1 — Update ADR-0021 con Superseded banner final

```text
Already done in Tarea 0.1, here we just verify and add a "Migration log" section listing
what changed.
```

### Tarea 10.2 — Update READMEs por carpeta tocada

```text
Goal: ningún README mencione Temporal salvo en context histórico.

Comando: grep -rln Temporal services/ libs/ infra/ workers-go/ docs/ | grep -v
docs_original_palantir | grep README | xargs editar.
```

### Tarea 10.3 — Documentar el patrón Foundry en `docs/architecture/foundry-pattern-orchestration.md`

```text
Goal: doc canónico explicando:
- Mapping de los 5 patrones (pipeline / reindex / automate / saga / approval)
- Convenciones (event_id, idempotency, outbox, retry)
- Cómo añadir un nuevo workflow
- Comparación con Foundry Builds/Automate/Functions
```

---

## FASE 11 — Verificación end-to-end y cierre

### Tarea 11.1 — Test integración full flow

```text
Goal: test que ejercita todos los patrones:
1. Crear ontology object (Action via ontology-actions-service)
2. Outbox event publica `ontology.changes.v1`
3. workflow-automation-service consume y dispatcha effect
4. effect llama otro action
5. saga ejecuta 3 steps
6. paso 3 falla → compensaciones revierten 2, 1
7. approval request creado, manager aprueba
8. pipeline scheduled tick crea SparkApplication
9. SparkApplication completa, escribe Iceberg

Verify: tras 5 minutos, todos los topics, tablas, datasets esperados están en estado
correcto.
```

### Tarea 11.2 — Performance benchmark

```text
Goal: medir throughput sostenido:
- 100 automations/segundo durante 1 minuto: P99 latency, error rate
- 10 sagas concurrentes: latency end-to-end
- 1 SparkApp con 1M rows input: time to complete

Verify: comparable a baseline Temporal (debería ser similar o mejor).
```

### Tarea 11.3 — Chaos test: fallar componentes

```text
Goal: validar que SPOFs eliminados:
- Kill kafka broker → consumers reanudan tras reelección
- Kill Postgres primary → CNPG failover, services reanudan
- Kill Spark driver → SparkApp restart by Spark Operator
- Kill ontology-actions-service → Automate consumers retry, eventually deliver

Verify: cada caso recuperación automática <60s.
```

### Tarea 11.4 — Closing audit + grep gates

```text
Goal: verificar que NO hay residuos Temporal en el repo.

Comandos:
- find . -name '*temporal*' | grep -v 'docs_original_palantir' → empty
- grep -rln 'temporal\|Temporal' --include='*.rs' --include='*.go' --include='*.toml' \
  --include='*.yaml' --include='*.gotmpl' . | grep -v 'docs_original_palantir\|ADR-0021\|ADR-0027\|migration-plan' → empty
- helm list -A | grep temporal → empty
- kubectl get crd | grep temporal → empty (si Temporal CRDs were installed)

Verify: todos vacíos.
```

### Tarea 11.5 — Merge a main + tag

```text
Steps:
1. PR de la branch migration/foundry-pattern-orchestration a main.
2. Re-run CI.
3. Squash merge or merge commit (decide by team policy).
4. Tag release: `v0.2.0-foundry-pattern`.
5. Update CHANGELOG.md.
```

---

## 3. Riesgos y mitigaciones

| Riesgo | Probabilidad | Impacto | Mitigación |
| --- | --- | --- | --- |
| Workflows en vuelo durante migración | Baja (pre-prod) | Alto si ocurre | Migración pre-prod; cero workflows live al hacer cutover |
| Pérdida de durable execution semántica para approvals long-running | Media | Medio | libs/state-machine + tests cubren timeout / escalation |
| Saga bugs (paso fallido sin compensación correcta) | Alta | Alto | Tests chaos + audit log de cada step en pg-policy.audit_compliance |
| Rendimiento Spark inferior a Temporal para batches | Baja | Bajo | Spark IS faster para batch; benchmark valida |
| Deuda técnica residual (algún archivo temporal_* olvidado) | Alta | Bajo | Tarea 11.4 grep gates |
| Equipo aprende 5 patrones nuevos | Alta | Medio | Doc canónico Tarea 10.3 + brown bag sessions |

---

## 4. Métricas de éxito

- ✅ `grep -r 'temporal' --include='*.rs' --include='*.go' . | grep -v docs_original` returns 0
- ✅ `helm list -A` no incluye temporal
- ✅ Cassandra solo tiene keyspaces de aplicación (no temporal_*)
- ✅ helmfile -e prod lint pasa
- ✅ Test integración Tarea 11.1 pasa
- ✅ Benchmark Tarea 11.2 muestra throughput ≥ baseline o explicable
- ✅ Chaos test Tarea 11.3 pasa todos los escenarios
- ✅ ADR-0027 accepted, ADR-0021 superseded, plan cassandra-foundry-parity actualizado

---

## 5. Cómo empezar mañana

1. **Día 1**: Tarea 0.1 (branch + ADR-0027) + Tarea 0.2 (inventario). Esto te da 1 día para
   ver el alcance real.
2. **Día 2-3**: Tareas 0.3 + 0.4 (docs) + Tarea 1.1 (libs/state-machine).
3. **Semana 2**: Resto de Fase 1 + Fase 2 (libs + outbox+Debezium validado).
4. **Semana 3-4**: Fase 3 (pipeline → Spark) — el primero, el más alto-ROI.
5. **Semana 5**: Fase 4 (reindex) — el más simple.
6. **Semana 6-7**: Fases 5 + 6 (workflow-automation + automation-ops).
7. **Semana 8**: Fase 7 (approvals) + Fase 8 (cleanup Rust).
8. **Semana 9**: Fase 9 (infra cleanup) + Fase 10 (docs).
9. **Semana 10**: Fase 11 (verification + merge).

**Hitos demo intermedios**:
- Fin semana 4: pipeline funciona vía Spark (demo)
- Fin semana 7: automation + saga + approval funcionando (demo)
- Fin semana 10: full integration test verde (demo final)

---

**Fin del plan.**
