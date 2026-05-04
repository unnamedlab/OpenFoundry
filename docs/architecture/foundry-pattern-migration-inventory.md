# Foundry-pattern migration inventory — Temporal surface

- Fecha: 2026-05-04
- Método: unión de `grep`/`find` sobre `temporal|Temporal|TEMPORAL|workers-go|go-workers|TEMPORAL_|temporal_persistence|temporal_visibility`, más expansión explícita de `workers-go/**` e `infra/helm/infra/temporal/**`.
- Paths accionables inventariados: **159**.
- Falsos positivos léxicos revisados y excluidos: **16**.
- Inspección adicional: `.github/workflows/ci.yml`, `.github/workflows/docker-publish.yml` y `.github/workflows/release.yml` no contienen referencias actuales a Temporal/workers-go; no se incluyen en la tabla.

## Resumen de categorías objetivo

- `libs/temporal-client/` → DELETE (Tarea 8.1).
- `workers-go/` → DELETE por dominio (Tareas 3.6, 4.3, 5.4, 6.5, 7.5) y cleanup global (Tarea 9.3).
- Servicios Rust con adaptadores/deps Temporal → REFACTOR/DELETE (Tareas 3.5, 5.3, 6.3, 7.3, 8.2, 8.3).
- `infra/helm/infra/temporal/` y wiring de Helm/runtime/Compose → DELETE/REFACTOR (Tareas 9.1, 9.2, 9.3, 9.4).
- Documentación/ADRs/runbooks → DOC-UPDATE (Tareas 0.3, 10.1, 10.2).

## Tarea 3.x — Pipeline

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `services/pipeline-schedule-service/Cargo.toml` | dependency | DELETE | Tarea 8.3 | quitar dep `temporal-client` del servicio pipeline |
| `services/pipeline-schedule-service/src/domain/dispatcher.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/src/domain/event_listener.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/src/domain/mod.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/src/domain/schedule.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/src/domain/temporal_schedule.rs` | import | DELETE | Tarea 8.2 | eliminar adaptador/handler `temporal_*` del servicio pipeline |
| `services/pipeline-schedule-service/src/handlers/mod.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/src/handlers/schedules_v2.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/src/handlers/temporal_schedule.rs` | import | DELETE | Tarea 8.2 | eliminar adaptador/handler `temporal_*` del servicio pipeline |
| `services/pipeline-schedule-service/src/lib.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/src/main.rs` | import | REFACTOR | Tarea 3.5 | migrar scheduler a CronJob + Kafka / SparkApplication |
| `services/pipeline-schedule-service/tests/temporal_schedule_idempotency.rs` | test | DELETE | Tarea 3.7 | retirar tests e2e/idempotencia dependientes de Temporal |
| `services/pipeline-schedule-service/tests/temporal_schedule_load.rs` | test | DELETE | Tarea 3.7 | retirar tests e2e/idempotencia dependientes de Temporal |
| `workers-go/pipeline/Dockerfile` | manifest | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/README.md` | doc | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/activities/activities.go` | import | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/activities/activities_test.go` | test | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/go.mod` | dependency | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/go.sum` | dependency | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/internal/contract/contract.go` | import | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/main.go` | import | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |
| `workers-go/pipeline/workflows/pipeline_run.go` | import | DELETE | Tarea 3.6 | eliminar worker pipeline en Go/Temporal |

## Tarea 4.x — Reindex

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `services/ontology-indexer/README.md` | doc | REFACTOR | Tarea 4.4 | ajustar downstream consumer a nuevo productor reindex sin Temporal |
| `services/ontology-indexer/src/lib.rs` | import | REFACTOR | Tarea 4.4 | ajustar downstream consumer a nuevo productor reindex sin Temporal |
| `workers-go/reindex/README.md` | doc | DELETE | Tarea 4.3 | eliminar worker reindex en Go/Temporal |
| `workers-go/reindex/activities/activities.go` | import | DELETE | Tarea 4.3 | eliminar worker reindex en Go/Temporal |
| `workers-go/reindex/go.mod` | dependency | DELETE | Tarea 4.3 | eliminar worker reindex en Go/Temporal |
| `workers-go/reindex/internal/contract/contract.go` | import | DELETE | Tarea 4.3 | eliminar worker reindex en Go/Temporal |
| `workers-go/reindex/main.go` | import | DELETE | Tarea 4.3 | eliminar worker reindex en Go/Temporal |
| `workers-go/reindex/workflows/reindex.go` | import | DELETE | Tarea 4.3 | eliminar worker reindex en Go/Temporal |

## Tarea 5.x — Workflow automation

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `services/workflow-automation-service/Cargo.toml` | dependency | DELETE | Tarea 8.3 | quitar dep `temporal-client` del servicio workflow-automation |
| `services/workflow-automation-service/src/domain/mod.rs` | import | REFACTOR | Tarea 5.3 | migrar orchestration a state machine + Kafka |
| `services/workflow-automation-service/src/domain/temporal_adapter.rs` | import | DELETE | Tarea 8.2 | eliminar adaptador `temporal_*` del servicio workflow-automation |
| `services/workflow-automation-service/src/handlers/approvals.rs` | import | REFACTOR | Tarea 5.3 | migrar orchestration a state machine + Kafka |
| `services/workflow-automation-service/src/handlers/execute.rs` | import | REFACTOR | Tarea 5.3 | migrar orchestration a state machine + Kafka |
| `services/workflow-automation-service/src/main.rs` | import | REFACTOR | Tarea 5.3 | migrar orchestration a state machine + Kafka |
| `services/workflow-automation-service/tests/temporal_e2e.rs` | test | DELETE | Tarea 5.3 | retirar test end-to-end dependiente de Temporal |
| `workers-go/workflow-automation/Dockerfile` | manifest | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/README.md` | doc | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/activities/activities.go` | import | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/activities/activities_test.go` | test | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/go.mod` | dependency | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/go.sum` | dependency | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/internal/contract/contract.go` | import | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/main.go` | import | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |
| `workers-go/workflow-automation/workflows/automation_run.go` | import | DELETE | Tarea 5.4 | eliminar worker workflow-automation en Go/Temporal |

## Tarea 6.x — Automation ops

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `services/automation-operations-service/Cargo.toml` | dependency | DELETE | Tarea 8.3 | quitar dep `temporal-client` del servicio automation-ops |
| `services/automation-operations-service/migrations/README.md` | doc | DOC-UPDATE | Tarea 6.2 | actualizar diseño de saga schema sin Temporal |
| `services/automation-operations-service/src/domain/mod.rs` | import | REFACTOR | Tarea 6.3 | migrar pasos a saga choreography |
| `services/automation-operations-service/src/domain/temporal_adapter.rs` | import | DELETE | Tarea 8.2 | eliminar adaptador `temporal_*` del servicio automation-ops |
| `services/automation-operations-service/src/handlers.rs` | import | REFACTOR | Tarea 6.3 | migrar pasos a saga choreography |
| `services/automation-operations-service/src/lib.rs` | import | REFACTOR | Tarea 6.3 | migrar pasos a saga choreography |
| `services/automation-operations-service/src/main.rs` | import | REFACTOR | Tarea 6.3 | migrar pasos a saga choreography |
| `workers-go/automation-ops/Dockerfile` | manifest | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/README.md` | doc | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/activities/activities.go` | import | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/activities/activities_test.go` | test | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/go.mod` | dependency | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/go.sum` | dependency | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/internal/contract/contract.go` | import | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/main.go` | import | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/workflows/automation_ops_task.go` | import | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |
| `workers-go/automation-ops/workflows/automation_ops_task_test.go` | test | DELETE | Tarea 6.5 | eliminar worker automation-ops en Go/Temporal |

## Tarea 7.x — Approvals

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `services/approvals-service/Cargo.toml` | dependency | DELETE | Tarea 8.3 | quitar dep `temporal-client` del servicio approvals |
| `services/approvals-service/src/domain/mod.rs` | import | REFACTOR | Tarea 7.3 | migrar approvals a state machine + timeout sweep |
| `services/approvals-service/src/domain/runtime.rs` | import | REFACTOR | Tarea 7.3 | migrar approvals a state machine + timeout sweep |
| `services/approvals-service/src/domain/temporal_adapter.rs` | import | DELETE | Tarea 8.2 | eliminar adaptador `temporal_*` del servicio approvals |
| `services/approvals-service/src/handlers/approvals.rs` | import | REFACTOR | Tarea 7.3 | migrar approvals a state machine + timeout sweep |
| `services/approvals-service/src/lib.rs` | import | REFACTOR | Tarea 7.3 | migrar approvals a state machine + timeout sweep |
| `services/approvals-service/src/main.rs` | import | REFACTOR | Tarea 7.3 | migrar approvals a state machine + timeout sweep |
| `workers-go/approvals/Dockerfile` | manifest | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/README.md` | doc | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/activities/activities.go` | import | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/activities/activities_test.go` | test | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/go.mod` | dependency | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/go.sum` | dependency | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/internal/contract/contract.go` | import | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/main.go` | import | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/workflows/approval_request.go` | import | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |
| `workers-go/approvals/workflows/approval_request_test.go` | test | DELETE | Tarea 7.5 | eliminar worker approvals en Go/Temporal |

## Tarea 8.x — Shared code / deps / tests

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `Cargo.toml` | dependency | DELETE | Tarea 8.3 | quitar miembro/dependency `libs/temporal-client` del workspace |
| `justfile` | config | REFACTOR | Tarea 9.3 | eliminar recipes `test-temporal`, `go-build`, `go-test`, `go-tidy`, `go-worker` |
| `libs/temporal-client/Cargo.toml` | dependency | DELETE | Tarea 8.1 | borrar crate/fachada Temporal compartida |
| `libs/temporal-client/src/lib.rs` | import | DELETE | Tarea 8.1 | borrar crate/fachada Temporal compartida |
| `libs/testing/Cargo.toml` | dependency | DELETE | Tarea 8.1 | retirar harness `it-temporal` y launcher de workers-go |
| `libs/testing/src/go_workers.rs` | test | DELETE | Tarea 8.1 | retirar harness `it-temporal` y launcher de workers-go |
| `libs/testing/src/lib.rs` | test | DELETE | Tarea 8.1 | retirar harness `it-temporal` y launcher de workers-go |
| `libs/testing/src/temporal.rs` | test | DELETE | Tarea 8.1 | retirar harness `it-temporal` y launcher de workers-go |

## Tarea 9.x — Infra, Helm, Cassandra, CI y runtime

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `.github/workflows/go-workers.yml` | config | DELETE | Tarea 9.4 | workflow dedicado a build/test/publish de workers-go |
| `infra/README.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |
| `infra/compose/docker-compose.yml` | manifest | REFACTOR | Tarea 9.1 | eliminar servicios/local stack de Temporal UI + auto-setup |
| `infra/helm/README.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |
| `infra/helm/apps/of-apps-ops/templates/services.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-apps-ops/values-dev.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-apps-ops/values-prod.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-apps-ops/values-staging.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-apps-ops/values.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-data-engine/templates/services.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-data-engine/values-dev.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-data-engine/values-prod.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-data-engine/values-staging.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-data-engine/values.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-platform/README.md` | doc | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-platform/templates/services.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-platform/templates/temporal-workers.yaml` | manifest | DELETE | Tarea 9.3 | borrar despliegue Helm de workers-go |
| `infra/helm/apps/of-platform/values-dev.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-platform/values-prod.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-platform/values-staging.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/apps/of-platform/values.yaml` | manifest | REFACTOR | Tarea 9.3 | quitar env/values/despliegues asociados a Temporal y workers-go |
| `infra/helm/helmfile.yaml.gotmpl` | config | REFACTOR | Tarea 9.1 | eliminar repo/release/flags de Temporal del helmfile |
| `infra/helm/infra/cassandra-cluster/README.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |
| `infra/helm/infra/cassandra-cluster/templates/keyspaces-job.yaml` | manifest | REFACTOR | Tarea 9.2 | retirar referencias a keyspaces `temporal_*` |
| `infra/helm/infra/kafka-cluster/templates/kafka-acls-domain-v1.yaml` | manifest | DOC-UPDATE | Tarea 10.2 | limpiar comentarios/documentación que aún citan Temporal activities |
| `infra/helm/infra/temporal/Chart.lock` | dependency | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/Chart.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/README.md` | doc | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/charts/temporal-1.2.0.tgz` | dependency | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/files/servicemonitor.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/templates/cassandra-keyspaces-job.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/templates/namespace.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/templates/raw-files.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/templates/ui-ingress.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/values-dev.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/values-prod.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/temporal/values.yaml` | manifest | DELETE | Tarea 9.1 | borrar chart/release Temporal completo |
| `infra/helm/infra/vespa/upstream-chart/README.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar doc del reindex workflow hacia nuevo coordinador |
| `infra/runbooks/cassandra.md` | doc | REFACTOR | Tarea 9.2 | retirar referencias a keyspaces `temporal_*` |
| `infra/runbooks/dr-failover.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |
| `infra/runbooks/dr-game-day.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |
| `infra/runbooks/temporal.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |
| `infra/test-tools/chaos/README.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |
| `infra/test-tools/chaos/temporal-history-kill.yaml` | manifest | DELETE | Tarea 9.1 | retirar escenario de chaos específico de Temporal |
| `workers-go/README.md` | doc | DELETE | Tarea 9.3 | eliminar workspace Go temporal completo |
| `workers-go/go.work` | config | DELETE | Tarea 9.3 | eliminar workspace Go temporal completo |
| `workers-go/go.work.sum` | config | DELETE | Tarea 9.3 | eliminar workspace Go temporal completo |

## Documentación transversal (Tareas 0.3 / 10.x)

| Path | Tipo | Acción | Tarea responsable | Nota |
| ---- | ---- | ------ | ----------------- | ---- |
| `ARCHITECTURE.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0012-data-plane-slos.md` | doc | DOC-UPDATE | Tarea 10.2 | sustituir métricas/latencia Temporal por métricas del patrón Foundry |
| `docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md` | doc | DOC-UPDATE | Tarea 10.1 | añadir log final de migración / verificar superseded banner |
| `docs/architecture/adr/ADR-0024-postgres-consolidation.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0025-eliminate-custom-scheduler.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0026-identity-custom-retained.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0028-search-backend-abstraction.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0030-service-consolidation-30-targets.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0032-chaos-mesh-resilience-suite.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md` | doc | DOC-UPDATE | Tarea 10.2 | ajustar referencias/documentación complementaria del nuevo ADR |
| `docs/architecture/closure-global-stub-allowlist.md` | doc | DOC-UPDATE | Tarea 10.2 | retirar excepciones ligadas a `libs/temporal-client` |
| `docs/architecture/data-model-cassandra.md` | doc | DOC-UPDATE | Tarea 10.2 | limpiar referencias a reindex/workflows Temporal y keyspaces |
| `docs/architecture/lakehouse-evidence/2026-05-03/summary.md` | doc | DOC-UPDATE | Tarea 10.2 | ajustar evidencia que aún referencia workers-go/reindex |
| `docs/architecture/legacy-migrations/approvals-service/README.md` | doc | DOC-UPDATE | Tarea 7.3 | actualizar guía legacy -> state machine sin Temporal |
| `docs/architecture/legacy-migrations/automation-operations-service/README.md` | doc | DOC-UPDATE | Tarea 6.3 | actualizar guía legacy -> saga choreography |
| `docs/architecture/legacy-migrations/workflow-automation-service/README.md` | doc | DOC-UPDATE | Tarea 5.3 | actualizar guía legacy -> Kafka/state machine sin Temporal |
| `docs/architecture/migration-directory-classification.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación histórica/arquitectural que aún referencia Temporal |
| `docs/architecture/migration-plan-cassandra-foundry-parity.md` | doc | DOC-UPDATE | Tarea 0.3 | alinear planes de migración con inventario y ADR superseding |
| `docs/architecture/migration-plan-foundry-pattern-orchestration.md` | doc | DOC-UPDATE | Tarea 0.3 | alinear planes de migración con inventario y ADR superseding |
| `docs/architecture/service-consolidation-map.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar mapa de consolidación que aún asume backend Temporal |
| `docs/architecture/slo-evidence/2026-05-03/summary.md` | doc | DOC-UPDATE | Tarea 10.2 | sustituir métricas/latencia Temporal por métricas del patrón Foundry |
| `docs/getting-started/dev-stack.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar stack local sin Temporal |
| `libs/cassandra-kernel/README.md` | doc | DOC-UPDATE | Tarea 10.2 | actualizar documentación operacional/infra tras retirada de Temporal |

## Falsos positivos revisados y excluidos

- `PoC/01-vision-y-caso-de-uso.md` — temporal = cronología de la demo, no Temporal.io.
- `benchmarks/ontology/README.md` — temporal = orden temporal de IDs/fixtures, no workflow engine.
- `benchmarks/ontology/runbooks/hot-partitions.md` — bucket temporal de datos, no Temporal.io.
- `checklist.md` — series temporales/geotemporales, no workflow engine.
- `guia-migracion-services-a-microservicios.md` — compatibilidad temporal, no Temporal.io.
- `libs/ontology-kernel/src/domain/time_series.rs` — campo tipo temporal de series temporales.
- `microservicios-derivados-desde-foundry-docs.md` — ventanas/datos temporales, no Temporal.io.
- `prompts-migracion-hasta-85-microservicios.md` — compatibilidad temporal / analytics temporal, no Temporal.io.
- `services/dataset-versioning-service/src/handlers/foundry.rs` — orden temporal, no Temporal.io.
- `services/event-streaming-service/src/runtime/flink/sql.rs` — temporal interval join de Flink, no Temporal.io.
- `services/monitoring-rules-service/src/evaluator.rs` — GeotemporalObservations enum, no Temporal.io.
- `services/monitoring-rules-service/src/streaming_monitors.rs` — geotemporal = dominio analítico, no Temporal.io.
- `services/time-series-data-service/Cargo.toml` — temporal workloads = time-series.
- `vendor/arrow-arith/src/lib.rs` — módulo temporal de Arrow (fechas/horas), no Temporal.io.
- `vendor/arrow-arith/src/numeric.rs` — temporal_conversions de Arrow, no Temporal.io.
- `vendor/arrow-arith/src/temporal.rs` — temporal kernels de Arrow, no Temporal.io.

## Comprobaciones

- Total de rows accionables en tablas: **159**.
- Cobertura de Tareas 3–9: presente en secciones 3.x, 4.x, 5.x, 6.x, 7.x, 8.x y 9.x.
- Verificación manual pendiente tras guardar el documento: `wc -l docs/architecture/foundry-pattern-migration-inventory.md`.
