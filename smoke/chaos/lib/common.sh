#!/usr/bin/env bash
# shellcheck shell=bash
#
# Helpers compartidos por los scripts de chaos del data plane.
#
# Cada script de chaos tiene la misma forma:
#   1. Selecciona un pod víctima con un selector preciso.
#   2. Lo elimina con `kubectl delete pod`.
#   3. Espera a que el cluster (o el statefulset/deployment owner) vuelva
#      a reportar health verde antes de finalizar.
#
# Las funciones aquí encapsulan ese patrón para que cada script de capa
# sea trivial y mantenible. El objetivo es validar las propiedades
# no-SPOF del DP descritas en `docs/architecture/adr/ADR-0012-data-plane-slos.md`.

set -euo pipefail

# Tiempo máximo de espera para que un componente vuelva a estar verde
# tras matar un pod. Configurable por entorno.
: "${CHAOS_WAIT_TIMEOUT:=600s}"

# Permite simular el script sin tocar nada (útil en CI sin un cluster).
: "${CHAOS_DRY_RUN:=0}"

require_kubectl() {
  if ! command -v kubectl >/dev/null 2>&1; then
    echo "chaos: kubectl no está instalado o no está en PATH" >&2
    exit 1
  fi
}

log() {
  printf '[chaos] %s\n' "$*"
}

# pick_victim_pod <namespace> <label-selector>
#
# Imprime el nombre del primer pod Running que matchea el selector.
# Si no hay candidatos, falla.
pick_victim_pod() {
  local namespace="$1"
  local selector="$2"
  local pod
  pod="$(kubectl -n "$namespace" get pods \
    -l "$selector" \
    --field-selector=status.phase=Running \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -z "$pod" ]]; then
    echo "chaos: no se encontraron pods Running con selector '$selector' en ns '$namespace'" >&2
    return 1
  fi
  printf '%s' "$pod"
}

# delete_pod <namespace> <pod>
#
# Elimina el pod. Honra CHAOS_DRY_RUN para entornos sin cluster.
delete_pod() {
  local namespace="$1"
  local pod="$2"
  log "kubectl delete pod $pod -n $namespace"
  if [[ "$CHAOS_DRY_RUN" == "1" ]]; then
    log "(dry-run) skip delete"
    return 0
  fi
  kubectl -n "$namespace" delete pod "$pod" --wait=false
}

# wait_pods_ready <namespace> <label-selector>
#
# Espera a que TODOS los pods que matchean el selector estén Ready.
# Cubre la mayoría de casos (StatefulSets / Deployments owner).
wait_pods_ready() {
  local namespace="$1"
  local selector="$2"
  log "esperando pods Ready ns=$namespace selector='$selector' timeout=$CHAOS_WAIT_TIMEOUT"
  if [[ "$CHAOS_DRY_RUN" == "1" ]]; then
    log "(dry-run) skip wait"
    return 0
  fi
  kubectl -n "$namespace" wait --for=condition=Ready pod \
    -l "$selector" \
    --timeout="$CHAOS_WAIT_TIMEOUT"
}

# wait_resource_condition <namespace> <resource> <condition>
#
# Espera a una condition concreta de un recurso (CR, sts, deploy...).
wait_resource_condition() {
  local namespace="$1"
  local resource="$2"
  local condition="$3"
  log "esperando $resource condition=$condition ns=$namespace timeout=$CHAOS_WAIT_TIMEOUT"
  if [[ "$CHAOS_DRY_RUN" == "1" ]]; then
    log "(dry-run) skip wait"
    return 0
  fi
  kubectl -n "$namespace" wait "$resource" \
    --for="condition=$condition" \
    --timeout="$CHAOS_WAIT_TIMEOUT"
}
