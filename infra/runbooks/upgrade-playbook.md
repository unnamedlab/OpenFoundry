# Upgrade Playbook

Fecha: 25 de abril de 2026

## Objetivo

Ejecutar upgrades repetibles de OpenFoundry con validación previa, ventana de mantenimiento y rollback explícito.

## Preflight

- Validar Terraform/Helm del entorno
- Confirmar compatibilidad de migraciones
- Generar backup lógico de PostgreSQL
- Generar backup de buckets críticos
- Revisar gates de promotion en fleets sensibles

## Estrategia recomendada

1. `canary` en una deployment cell
2. Validación de métricas y smoke checks
3. Promoción a `stable`
4. Rollout al resto de cells dentro de maintenance window

## Rollback

- Revertir imagen o chart version
- Restaurar DB solo si hubo cambio destructivo o corrupción de datos
- Rehabilitar reconciliadores una vez establecida la versión anterior

## Evidencias mínimas

- Commit o tag desplegado
- Hora de inicio y fin
- Resultado de smoke checks
- Estado de gates
- Versiones previas y nuevas

## KRaft upgrade preflight

Para upgrades del cluster Kafka (operador Strimzi, `spec.kafka.version`,
`spec.kafka.metadataVersion`, o cambios estructurales del `KafkaNodePool`)
aplica además la **política específica de KRaft** documentada en
[ADR-0013](../../docs/architecture/adr/ADR-0013-kafka-kraft-no-spof-policy.md).
Resumen operacional:

1. **Gates obligatorios antes de fusionar el PR de upgrade:**
   - `python3 tools/kafka-lint/check_kraft.py` clean contra el manifest
     resultante (Layer A).
   - Última hora sin disparos de `KafkaUnderMinIsrPartitions` ni
     `KafkaActiveControllerCountAbnormal` en producción (Layer B —
     `infra/observability/prometheus-rules/kafka.yaml`).
   - `kafka-topics.sh ... --under-replicated-partitions` vacío.
   - Última ejecución verde del workflow *Chaos Smoke (Data Plane no-SPOF)*
     hace ≤ 7 días (Layer C — incluye `kill-active-kafka-controller.sh`).
2. **Orden de aplicación** (no acumular cambios en un solo PR):
   1. **Operador Strimzi** primero (CRDs + controller). Sin tocar
      `spec.kafka.version` en el mismo PR.
   2. **`spec.kafka.version`**, una versión minor a la vez, siguiendo la
      matriz de upgrade de Strimzi.
   3. **`spec.kafka.metadataVersion`** sólo cuando el cluster lleve
      estable al menos un ciclo completo de chaos-smoke en la nueva
      `kafka.version`. Este bump **no es reversible**: bloquea el
      formato on-disk del quorum.
3. **Criterios de aborto / rollback inmediato:**
   - Cualquiera de las dos alertas KRaft anteriores se dispara durante
     el rollout.
   - El CR `Kafka/openfoundry` no llega a `Ready` en 30 minutos tras
     `kubectl apply`.
   - Pérdida de quorum: `sum(ActiveControllerCount)` se queda en `0`
     más de 5 minutos.
4. **Prohibido en el mismo PR de upgrade:** mover
   `min.insync.replicas`, `default.replication.factor`,
   `unclean.leader.election.enable`, o `KafkaNodePool.roles` junto con
   un cambio de versión. Cada uno es su propio PR (gated por Layer A).

El paso a paso operativo y los comandos están en `infra/runbooks/kafka.md`
§2.1.
