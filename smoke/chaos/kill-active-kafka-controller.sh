#!/usr/bin/env bash
#
# Mata el pod que actualmente está actuando como **active KRaft controller**
# del cluster Strimzi y verifica que:
#
#   1. Otro pod del mismo `KafkaNodePool` toma el liderazgo del quorum.
#   2. El cluster vuelve a reportar `Ready` y todos los pods Ready.
#   3. El conteo de active controllers vuelve a ser exactamente 1
#      (no quedan situaciones de 0 = sin controller, ni >1 = split-brain).
#
# Por qué este escenario es distinto de `kill-one-kafka-broker.sh`:
#
#   * `kill-one-kafka-broker.sh` selecciona el primer pod Running que
#     matchea el label selector. En modo combinado controller+broker
#     (ver `infra/k8s/strimzi/kafka-cluster.yaml`) ese pod *podría* ser
#     el active controller — o no. Como el orden de pods devuelto por
#     la API depende de tiempos, no es determinista.
#   * Aquí seleccionamos *deliberadamente* al líder del quorum KRaft, que
#     es el único punto donde se serializan los cambios de metadata
#     (creación de topics, elección de leader de partición, etc.).
#     Es el escenario que hace que la garantía no-SPOF del controller
#     pase de "asumida" a "medida" — alineado con la propiedad que
#     `tools/kafka-lint/check_kraft.py` impone sobre el manifest y que
#     la alerta `KafkaActiveControllerCountAbnormal`
#     (`infra/observability/prometheus-rules/kafka.yaml`) detecta en
#     producción.
#
# Justificación de selectores y comandos:
#
#   * Namespace `kafka`, cluster `openfoundry`, pool `kafka` → coinciden
#     con el manifest de Strimzi. Configurable por entorno para CI multi-
#     cluster (`KAFKA_NAMESPACE`, `KAFKA_CLUSTER`, `KAFKA_POOL`).
#   * Strimzi nombra los pods del `KafkaNodePool` como
#     `<cluster>-<pool>-<broker.id>` (ej. `openfoundry-kafka-0`).
#   * `bin/kafka-metadata-quorum.sh --bootstrap-server localhost:9092 \
#         describe --status` imprime una línea `LeaderId:\t<id>` que
#     identifica al líder del quorum KRaft (active controller).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${KAFKA_NAMESPACE:-kafka}"
CLUSTER="${KAFKA_CLUSTER:-openfoundry}"
POOL="${KAFKA_POOL:-kafka}"
SELECTOR="strimzi.io/cluster=$CLUSTER,strimzi.io/pool-name=$POOL"

require_kubectl

# Helper: ejecuta kafka-metadata-quorum.sh dentro de un pod de Kafka cualquiera
# (el output del quorum es global al cluster, no a un pod concreto). Si la
# llamada falla — p.ej. el pod elegido fue justo el que matamos — se
# reintenta con el siguiente pod Ready.
quorum_leader_id() {
  local probe_pod
  if [[ "$CHAOS_DRY_RUN" == "1" ]]; then
    # Devolver un id determinista en dry-run para que el script sea
    # ejecutable sin un cluster real.
    printf '0'
    return 0
  fi
  for probe_pod in $(kubectl -n "$NAMESPACE" get pods \
      -l "$SELECTOR" \
      --field-selector=status.phase=Running \
      -o jsonpath='{.items[*].metadata.name}'); do
    local out
    out="$(kubectl -n "$NAMESPACE" exec "$probe_pod" -c kafka -- \
      bash -c 'bin/kafka-metadata-quorum.sh --bootstrap-server localhost:9092 describe --status' \
      2>/dev/null || true)"
    # Línea esperada: "LeaderId:               <int>"
    local leader_id
    leader_id="$(printf '%s\n' "$out" | awk '/^LeaderId:/ {print $2; exit}')"
    if [[ -n "$leader_id" && "$leader_id" =~ ^[0-9]+$ ]]; then
      printf '%s' "$leader_id"
      return 0
    fi
  done
  echo "chaos: no se pudo obtener LeaderId del quorum KRaft" >&2
  return 1
}

# Helper: cuenta cuántos pods del pool reportan ActiveControllerCount=1
# vía el endpoint de métricas JMX exporter de Strimzi (puerto 9404, path
# /metrics). Usa kubectl-exec + curl para no depender del scrape de
# Prometheus desde el chaos runner.
active_controller_count_sum() {
  if [[ "$CHAOS_DRY_RUN" == "1" ]]; then
    printf '1'
    return 0
  fi
  local total=0 pod val
  for pod in $(kubectl -n "$NAMESPACE" get pods \
      -l "$SELECTOR" \
      --field-selector=status.phase=Running \
      -o jsonpath='{.items[*].metadata.name}'); do
    val="$(kubectl -n "$NAMESPACE" exec "$pod" -c kafka -- \
      bash -c 'curl -sf http://localhost:9404/metrics 2>/dev/null \
        | awk "/^kafka_controller_kafkacontroller_activecontrollercount[ {]/ {print \$NF; exit}"' \
      2>/dev/null || true)"
    if [[ "$val" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
      # Truncamos a entero (la métrica es 0 o 1 por broker).
      total=$(( total + ${val%.*} ))
    fi
  done
  printf '%d' "$total"
}

# 1. Identificar el active controller actual.
leader_id="$(quorum_leader_id)"
victim="${CLUSTER}-${POOL}-${leader_id}"
log "active controller actual: id=$leader_id pod=$victim"

# 2. Matar al líder del quorum.
delete_pod "$NAMESPACE" "$victim"

# 3. Esperar a que todos los pods del pool vuelvan a estar Ready.
wait_pods_ready "$NAMESPACE" "$SELECTOR"

# 4. Esperar a que el CR Kafka reporte Ready (reconciliación convergida).
wait_resource_condition "$NAMESPACE" "kafka.kafka.strimzi.io/$CLUSTER" "Ready"

# 5. Contrato no-SPOF del controller: tras el incidente, el quorum debe
#    haber elegido a un *otro* pod como líder.
new_leader_id="$(quorum_leader_id)"
log "active controller tras chaos: id=$new_leader_id"
if [[ "$CHAOS_DRY_RUN" != "1" && "$new_leader_id" == "$leader_id" ]]; then
  echo "chaos: el quorum reeligió al mismo id ($leader_id) — esperaba reelección a otro pod" >&2
  exit 1
fi

# 6. Sanity check: exactamente un active controller en todo el cluster.
acc_sum="$(active_controller_count_sum)"
log "sum(ActiveControllerCount)=$acc_sum (esperado: 1)"
if [[ "$acc_sum" != "1" ]]; then
  echo "chaos: ActiveControllerCount sum=$acc_sum (esperado 1; 0=sin controller, >1=split-brain)" >&2
  exit 1
fi

log "kafka active-controller chaos completado"
