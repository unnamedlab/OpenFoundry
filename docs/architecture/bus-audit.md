# Auditoría de uso del bus de eventos (`event-bus-data` vs `event-bus-control`)

- **Fecha:** 2026-04-29
- **Alcance:** `services/*` del monorepo OpenFoundry
- **Insumos:**
  - [`libs/event-bus-data`](../../libs/event-bus-data/Cargo.toml) — Apache Kafka (data plane)
  - [`libs/event-bus-control`](../../libs/event-bus-control/) — NATS JetStream (control plane)
  - [ADR-0011 — Control vs Data bus contract](./adr/ADR-0011-control-vs-data-bus-contract.md)
  - [`docs/architecture/runtime-topology.md` §"Control Plane vs Data Plane"](./runtime-topology.md)
  - [`tools/bus-lint/check_bus.py`](../../tools/bus-lint/check_bus.py) (lint de la Tarea 4)
  - [`/.github/bus-allowlist.yaml`](../../.github/bus-allowlist.yaml)

> Este informe es **input para PRs separados**. No se modifica código de servicios
> como parte de esta auditoría (cf. restricción de la tarea).

---

## 1. Resumen ejecutivo

La auditoría busca servicios que utilicen `libs/event-bus-data` (Kafka) para
**semántica de control** —notificaciones, invalidaciones de caché, señales o
triggers de workflow, y comandos cortos no replayables— y que, por tanto,
deberían migrarse a `libs/event-bus-control` (NATS JetStream).

**Hallazgo principal:** **0 servicios** del directorio `services/*` declaran
hoy una dependencia Cargo sobre `event-bus-data`. La separación control vs
data, definida en ADR-0011, ya está respetada **antes** de cualquier
migración. La auditoría no encuentra violaciones de semántica que requieran
mover topics de Kafka a NATS.

Evidencia (todas las rutas son absolutas dentro del repo):

```text
$ grep -rE 'event[-_]bus[-_]data' /home/runner/work/OpenFoundry/OpenFoundry/services/*/Cargo.toml
# (sin resultados)

$ python3 /home/runner/work/OpenFoundry/OpenFoundry/tools/bus-lint/check_bus.py
Bus contract lint OK: 94 service(s) checked, 34 with declared bus dependencies.
```

Los 34 servicios con dependencia declarada usan **exclusivamente**
`event-bus-control` (NATS JetStream), que es el plano correcto para la
semántica que emiten (ver tabla §3). Los 60 servicios restantes no
dependen de ninguno de los dos buses.

**Recomendación global:** mantener el estado actual. La Tarea 4 (lint de
contrato) ya garantiza que cualquier futura adición de `event-bus-data`
deba pasar por una entrada explícita en `.github/bus-allowlist.yaml`,
revisable en PR. La sección §5 enumera las violaciones que el lint
**debería** marcar si, hipotéticamente, alguno de estos servicios añadiera
Kafka para semántica de control.

---

## 2. Metodología

1. **Detección de dependencia Cargo** sobre `event-bus-data` en cada
   `services/*/Cargo.toml` (formas detectadas por
   [`tools/bus-lint/check_bus.py`](../../tools/bus-lint/check_bus.py):
   tabla `[dependencies.event-bus-data]`, valor inline, y `.workspace = true`).
2. **Detección de uso directo** de `rdkafka` o `event_bus_data::*` en el
   código fuente Rust (`services/*/src/**/*.rs`).
3. **Cribado de menciones literales** a `kafka` en código y migraciones SQL,
   distinguiendo entre:
   - uso del bus interno `event-bus-data`, **vs**
   - configuración de un **conector externo** Kafka del cliente (catálogo,
     ingestion, virtual-table) — fuera del alcance de ADR-0011.
4. **Clasificación semántica** de cada subject NATS observado contra las
   cuatro categorías de control de la tarea: notificación, invalidación
   de caché, trigger/señal de workflow, comando corto no replayable.

Comandos exactos en §6.

---

## 3. Tabla principal — servicio → topic actual → semántica → recomendación

> Convención de columnas:
>
> - **Topic actual** referencia a un *subject* NATS (`of.<dominio>.<acción>`)
>   o a una constante en
>   [`libs/event-bus-control/src/contracts.rs`](../../libs/event-bus-control/src/contracts.rs)
>   /
>   [`libs/event-bus-control/src/topics.rs`](../../libs/event-bus-control/src/topics.rs).
> - **Topic Kafka actual** está vacío (`—`) en todas las filas: ningún servicio
>   depende de `event-bus-data`.

| Servicio                              | Topic Kafka actual | Subject NATS / contrato actual                                   | Semántica detectada                                                  | Recomendación                                                |
| ------------------------------------- | ------------------ | ---------------------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------ |
| app-builder-service                   | —                  | `of.workflows.*` (`event_bus_control::contracts`)                | Trigger de build/composición (control)                               | **Mantener en NATS**                                         |
| application-curation-service          | —                  | `of.ontology.*`                                                  | Señal de curación / aprobación (control)                             | **Mantener en NATS**                                         |
| audit-compliance-service              | —                  | `of.audit.gateway`, `of.audit.auth`, `of.audit.datasets`, `of.audit.workflows`, `of.audit.notifications` (`services/audit-compliance-service/src/domain/collector.rs:7-11`) | Recolección de eventos cortos de auditoría (control, fan-out)        | **Mantener en NATS**                                         |
| connector-management-service          | —                  | `of.datasets.*` (control); literal `"kafka"` en código se refiere al **conector externo** del cliente (`services/connector-management-service/src/connectors/kafka.rs`) | Notificación de cambio de conector (control)                         | **Mantener en NATS** (Kafka externo no aplica a ADR-0011)    |
| data-asset-catalog-service            | —                  | `DATASET_QUALITY_REFRESH_REQUESTED_SUBJECT` (`services/data-asset-catalog-service/src/handlers/upload.rs:11,302`) | Invalidación / refresco tras upload (control, comando corto)         | **Mantener en NATS**                                         |
| dataset-quality-service               | —                  | `DatasetQualityRefreshRequested` (`services/dataset-quality-service/src/domain/quality/profiler.rs:7`) | Trigger de re-perfilado (control, comando corto no replayable)       | **Mantener en NATS**                                         |
| entity-resolution-service             | —                  | `of.ontology.*`                                                  | Señal de re-resolución (control)                                     | **Mantener en NATS**                                         |
| event-streaming-service               | —                  | `of.*` (control); el backend Kafka del propio servicio es **stub** (`services/event-streaming-service/src/backends/kafka.rs:3-4,35`) y se expone como producto a clientes, no como bus interno | Producto de streaming externo (data) + control de su ciclo de vida   | **Mantener en NATS**; el stub Kafka no usa `event-bus-data`  |
| federation-product-exchange-service   | —                  | `of.notifications.*`                                             | Notificación de federación (control)                                 | **Mantener en NATS**                                         |
| geospatial-intelligence-service       | —                  | `of.ontology.*`                                                  | Señal de re-cálculo de capas (control)                               | **Mantener en NATS**                                         |
| global-branch-service                 | —                  | `of.workflows.*`                                                 | Notificación de cambio de rama (control)                             | **Mantener en NATS**                                         |
| identity-federation-service           | —                  | `of.auth`                                                        | Notificación de identidad / sesión (control)                         | **Mantener en NATS**                                         |
| ingestion-replication-service         | —                  | `of.datasets.*` (control); literal `"kafka"` se refiere al **conector externo** (`services/ingestion-replication-service/src/connectors/kafka.rs`) | Señal de ciclo de ingestión (control)                                | **Mantener en NATS**                                         |
| lineage-service                       | —                  | `of.audit`, `of.datasets`                                        | Señales cortas de linaje (control); el firehose OpenLineage masivo iría a `event-bus-data` cuando se materialice | **Mantener en NATS** para señales; **futuro** topic Kafka `of.lineage.events` permanecería en `event-bus-data` (data) |
| marketplace-catalog-service           | —                  | `of.notifications.*`                                             | Notificación de catálogo (control)                                   | **Mantener en NATS**                                         |
| marketplace-service                   | —                  | `of.notifications.*`; mención `'kafka'` en `services/marketplace-service/migrations/20260422220500_marketplace_foundation.sql:61,68` describe **adapters de cliente**, no el bus interno | Notificación de marketplace (control)                                | **Mantener en NATS**                                         |
| nexus-service                         | —                  | `of.workflows.*`                                                 | Coordinación / trigger (control)                                     | **Mantener en NATS**                                         |
| notebook-runtime-service              | —                  | `of.workflows.*`                                                 | Señal de ciclo de notebook (control, comando corto)                  | **Mantener en NATS**                                         |
| notification-alerting-service         | —                  | `NOTIFICATION_SUBJECT = "of.notifications.updated"` (`services/notification-alerting-service/src/main.rs:13`, `src/handlers/ws.rs:9,106`) | Notificación pura, fan-out a WebSocket (control, no replayable)      | **Mantener en NATS**                                         |
| object-database-service               | —                  | `of.ontology.*`                                                  | Invalidación de caché de objetos (control)                           | **Mantener en NATS**                                         |
| ontology-actions-service              | —                  | `of.ontology`                                                    | Trigger de acción ontológica (control)                               | **Mantener en NATS**                                         |
| ontology-definition-service           | —                  | `of.ontology`                                                    | Notificación de cambio de esquema (control)                          | **Mantener en NATS**                                         |
| ontology-functions-service            | —                  | `of.ontology`                                                    | Trigger de ejecución de función (control, comando corto)             | **Mantener en NATS**                                         |
| ontology-funnel-service               | —                  | `of.ontology`                                                    | Señal de re-cálculo de funnel (control)                              | **Mantener en NATS**                                         |
| ontology-query-service                | —                  | `of.queries`                                                     | Invalidación de plan de consulta (control)                           | **Mantener en NATS**                                         |
| ontology-security-service             | —                  | `of.ontology`, `of.auth`                                         | Invalidación de políticas (control)                                  | **Mantener en NATS**                                         |
| pipeline-authoring-service            | —                  | `of.pipelines`                                                   | Señal de cambio de pipeline (control)                                | **Mantener en NATS**                                         |
| pipeline-build-service                | —                  | `of.pipelines`                                                   | Trigger de build (control, comando corto no replayable)              | **Mantener en NATS**                                         |
| pipeline-schedule-service             | —                  | `WORKFLOW_TRIGGER_REQUESTED_SUBJECT` (`services/pipeline-schedule-service/src/domain/workflow.rs:12`) | Trigger de workflow agendado (control, comando corto)                | **Mantener en NATS**                                         |
| product-distribution-service          | —                  | `of.notifications.*`                                             | Notificación de distribución (control)                               | **Mantener en NATS**                                         |
| report-service                        | —                  | `of.notifications.*`                                             | Notificación de reporte (control)                                    | **Mantener en NATS**                                         |
| sql-bi-gateway-service                | —                  | `of.queries`                                                     | Invalidación de caché de consultas (control)                         | **Mantener en NATS**                                         |
| virtual-table-service                 | —                  | `of.datasets.*` (control); cadena `"kafka"` en `services/virtual-table-service/src/models/connection.rs:39` enumera **tipos de conexión** del usuario | Señal de cambio de tabla virtual (control)                           | **Mantener en NATS**                                         |
| workflow-automation-service           | —                  | `WORKFLOW_TRIGGER_REQUESTED` (`services/workflow-automation-service/src/models/execution.rs:2`, `src/domain/workflow_run_requested.rs:1`) | Trigger / señal de workflow (control, comando corto)                 | **Mantener en NATS**                                         |

**Total:** 34 servicios — todos correctamente alineados con el plano de
control. **0 migraciones de Kafka → NATS necesarias.**

### 3.1. Servicios sin dependencia de bus (no listados arriba)

Los servicios restantes en `services/*` (p. ej. `agent-runtime-service`,
`ai-application-generation-service`, `cipher-service`,
`edge-gateway-service`, etc.) no declaran hoy
dependencia sobre `event-bus-control` ni `event-bus-data`. Quedan fuera del
alcance de esta auditoría. Si en el futuro publican o consumen eventos,
deberán añadirse simultáneamente a `Cargo.toml` y a
`.github/bus-allowlist.yaml` para satisfacer el lint de la Tarea 4.

### 3.2. Falsos positivos cribados

Las siguientes menciones a "kafka" en `services/*` **no** son uso de
`event-bus-data` y se han descartado:

| Ruta absoluta                                                                                                 | Por qué no es violación                                                |
| ------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `/home/runner/work/OpenFoundry/OpenFoundry/services/connector-management-service/src/connectors/kafka.rs`     | Conector Kafka externo del cliente (data product), expuesto vía `event_bus_control::connectors` (control de su ciclo de vida) |
| `/home/runner/work/OpenFoundry/OpenFoundry/services/ingestion-replication-service/src/connectors/kafka.rs`    | Idem — conector externo, no bus interno                                |
| `/home/runner/work/OpenFoundry/OpenFoundry/services/virtual-table-service/src/connectors/kafka.rs`            | Idem                                                                   |
| `/home/runner/work/OpenFoundry/OpenFoundry/services/virtual-table-service/src/models/connection.rs:39`        | Cadena `"kafka"` en enumeración de tipos de conexión configurables     |
| `/home/runner/work/OpenFoundry/OpenFoundry/services/event-streaming-service/src/backends/kafka.rs`            | Stub explícito (PR 2 deferred); no añade dependencia sobre `event-bus-data` |
| `/home/runner/work/OpenFoundry/OpenFoundry/services/event-streaming-service/src/main.rs:48`                   | Mensaje de log indicando que el backend Kafka está deshabilitado       |
| `/home/runner/work/OpenFoundry/OpenFoundry/services/marketplace-service/migrations/20260422220500_marketplace_foundation.sql:61,68` | Texto descriptivo de "adapters" en metadatos de catálogo               |

---

## 4. Topics Kafka activos en `event-bus-data`

Hoy: **ninguno consumido o producido por servicios del monorepo**.

`libs/event-bus-data` está implementada y testeable
(`libs/event-bus-data/src/{publisher,subscriber}.rs`,
`libs/event-bus-data/Cargo.toml:17-22` con feature `it`), pero ningún
servicio la usa. Este es el estado documentado por
[`docs/architecture/runtime-topology.md` §"Control Plane vs Data Plane"](./runtime-topology.md):
los firehoses CDC / lineage / OpenLineage están descritos como destino
**futuro** del data plane, no como tráfico actual.

---

## 5. Violaciones que el lint de la Tarea 4 debería marcar tras la migración

Como no hay migraciones que ejecutar (estado actual = estado objetivo), esta
sección documenta los patrones que `tools/bus-lint/check_bus.py` debe
seguir bloqueando para preservar el invariante de ADR-0011. Cada ítem
corresponde a una violación que un PR futuro podría intentar introducir
y que el lint **debería** rechazar:

1. **Adición silenciosa de `event-bus-data` para semántica de control.**
   Cualquiera de los 34 servicios listados en §3 que añada
   `[dependencies.event-bus-data]` en su `Cargo.toml` **sin** añadir
   `- data` bajo su entrada en `.github/bus-allowlist.yaml` debe fallar
   con el mensaje `depends on bus(es) ['data'] not declared`.
   Aplica especialmente a:
   - `notification-alerting-service` (notificaciones puras → control)
   - `dataset-quality-service`, `data-asset-catalog-service`
     (refresh/invalidación → control)
   - `pipeline-schedule-service`, `pipeline-build-service`,
     `workflow-automation-service`, `nexus-service`,
     `app-builder-service`, `notebook-runtime-service`
     (triggers/comandos cortos → control)
   - `ontology-query-service`, `sql-bi-gateway-service`,
     `object-database-service`, `ontology-security-service`
     (invalidación de caché → control)
2. **Entrada `target` declarando `data` para un servicio con semántica de
   control.** Aunque la sección `target:` de `.github/bus-allowlist.yaml`
   es informativa (ver §1 del YAML), revisores deben rechazar PRs que
   muevan a `target` cualquier servicio de la lista anterior con bus
   `data`.
3. **Drift de `current` vs Cargo.toml.** Si un servicio elimina su
   dependencia de `event-bus-control` pero olvida quitar la entrada del
   allowlist, el lint debe marcar `stale entry`.
4. **Servicio nuevo con dependencia de bus pero sin entrada.** Cualquier
   crate nuevo bajo `services/` que dependa de `event-bus-control` o
   `event-bus-data` sin aparecer en el allowlist debe fallar con
   `not listed in .github/bus-allowlist.yaml`.
5. **Uso de `rdkafka` directo (sin pasar por `event-bus-data`).** Esta
   regla **no** está cubierta hoy por `check_bus.py` (sólo audita Cargo
   deps). Recomendación para una iteración futura del lint: extender
   el chequeo para detectar `use rdkafka::*` en `services/*/src/**/*.rs`
   y exigir que el servicio esté declarado con bus `data`.

---

## 6. Cómo reproducir esta auditoría

```bash
# 1. Detectar dependencia Cargo sobre event-bus-data
grep -rE 'event[-_]bus[-_]data' \
  /home/runner/work/OpenFoundry/OpenFoundry/services/*/Cargo.toml

# 2. Detectar uso directo de rdkafka o de la API event_bus_data
grep -rnE 'rdkafka|event_bus_data::|use event_bus_data' \
  /home/runner/work/OpenFoundry/OpenFoundry/services

# 3. Re-ejecutar el lint de la Tarea 4 (debe seguir verde)
python3 /home/runner/work/OpenFoundry/OpenFoundry/tools/bus-lint/check_bus.py
```

---

## 7. Conclusión

La separación control/data definida en
[ADR-0011](./adr/ADR-0011-control-vs-data-bus-contract.md) está **plenamente
respetada** en el estado actual del monorepo. No se requieren PRs de
migración Kafka → NATS. El allowlist se actualiza añadiendo secciones
documentales `current` y `target` (idénticas, en ausencia de migraciones)
para dejar el estado objetivo explícito y preparar el terreno por si en
el futuro algún servicio añade `event-bus-data` para semántica de datos
legítima (CDC, lineage, OpenLineage firehose).
