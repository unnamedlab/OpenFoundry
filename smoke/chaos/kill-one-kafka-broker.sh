#!/usr/bin/env bash
#
# Mata un broker de Kafka (Strimzi) y espera a que el cluster reporte Ready.
#
# Justificación de selectores:
#   - Namespace `kafka` y cluster `openfoundry` → ver
#     `infra/k8s/strimzi/kafka-cluster.yaml` (KafkaNodePool `kafka` con
#     replicas=3 y rol controller+broker).
#   - Strimzi etiqueta los pods con `strimzi.io/cluster=openfoundry` y
#     `strimzi.io/pool-name=kafka` (mismas labels que el spec del
#     `topologySpreadConstraints` declarado en el manifest).
#   - Con `min.insync.replicas=2` y `unclean.leader.election.enable=false`
#     (cabecera del manifest) matar 1 de 3 brokers debe ser tolerado sin
#     pérdida ni indisponibilidad sostenida.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${KAFKA_NAMESPACE:-kafka}"
CLUSTER="${KAFKA_CLUSTER:-openfoundry}"
POOL="${KAFKA_POOL:-kafka}"
SELECTOR="strimzi.io/cluster=$CLUSTER,strimzi.io/pool-name=$POOL"

require_kubectl

victim="$(pick_victim_pod "$NAMESPACE" "$SELECTOR")"
log "víctima broker: $victim"
delete_pod "$NAMESPACE" "$victim"

# El StatefulSet del nodepool tiene el mismo nombre `<cluster>-<pool>`.
wait_pods_ready "$NAMESPACE" "$SELECTOR"

# El CR Kafka expone una condition `Ready` cuando el reconciler converge.
wait_resource_condition "$NAMESPACE" "kafka.kafka.strimzi.io/$CLUSTER" "Ready"

log "kafka broker chaos completado"
