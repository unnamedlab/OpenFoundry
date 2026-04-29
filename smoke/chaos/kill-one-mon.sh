#!/usr/bin/env bash
#
# Mata un mon de Rook-Ceph y espera a que el cluster vuelva a HEALTH_OK.
#
# Justificación de selectores:
#   - Namespace `rook-ceph` → ver `infra/k8s/rook/cluster.yaml` (CephCluster
#     `openfoundry` reside en `namespace: rook-ceph`).
#   - El operador Rook etiqueta los pods de monitor con
#     `app=rook-ceph-mon` (convención upstream estable).
#   - `mon.count=5` (ver cabecera de `infra/k8s/rook/cluster.yaml`) tolera
#     dos fallos simultáneos manteniendo quorum=3, así que matar uno
#     debe converger a HEALTH_OK sin pérdida de datos.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${ROOK_CEPH_NAMESPACE:-rook-ceph}"
SELECTOR="${ROOK_CEPH_MON_SELECTOR:-app=rook-ceph-mon}"

require_kubectl

victim="$(pick_victim_pod "$NAMESPACE" "$SELECTOR")"
log "víctima mon: $victim"
delete_pod "$NAMESPACE" "$victim"

# Esperar a que TODOS los mons vuelvan a estar Ready (quorum reformado).
wait_pods_ready "$NAMESPACE" "$SELECTOR"

# Verificación adicional: ceph status reporta HEALTH_OK / HEALTH_WARN
# transitorio aceptable. Solo lo intentamos si el toolbox está desplegado.
if [[ "$CHAOS_DRY_RUN" != "1" ]] \
  && kubectl -n "$NAMESPACE" get deploy rook-ceph-tools >/dev/null 2>&1; then
  log "verificando ceph status via rook-ceph-tools"
  end=$(( $(date +%s) + 300 ))
  while (( $(date +%s) < end )); do
    status="$(kubectl -n "$NAMESPACE" exec deploy/rook-ceph-tools -- \
      ceph health 2>/dev/null || true)"
    if [[ "$status" == HEALTH_OK* ]]; then
      log "ceph health: $status"
      exit 0
    fi
    log "ceph health aún: ${status:-desconocido}, reintentando..."
    sleep 10
  done
  echo "chaos: ceph no volvió a HEALTH_OK a tiempo" >&2
  exit 1
fi

log "mon chaos completado"
