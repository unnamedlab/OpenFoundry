#!/usr/bin/env bash
#
# Provoca un failover de CloudNativePG matando al primary del cluster.
#
# Justificación de selectores:
#   - El template base de CNPG está en
#     `infra/k8s/platform/manifests/cnpg/templates/cluster.yaml` con `instances=3` y
#     `minSyncReplicas=maxSyncReplicas=1`, así que matar el primary debe
#     promover una réplica síncrona sin pérdida.
#   - CloudNativePG etiqueta cada instancia con
#     `cnpg.io/cluster=<cluster>` y `cnpg.io/instanceRole=primary` (o
#     `replica`) — convención estable del operador (Apache-2.0).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${PG_NAMESPACE:-default}"
CLUSTER="${PG_CLUSTER:-openfoundry-pg}"
PRIMARY_SELECTOR="cnpg.io/cluster=$CLUSTER,cnpg.io/instanceRole=primary"
ALL_SELECTOR="cnpg.io/cluster=$CLUSTER"

require_kubectl

victim="$(pick_victim_pod "$NAMESPACE" "$PRIMARY_SELECTOR")"
log "víctima pg primary: $victim"
old_primary="$victim"
delete_pod "$NAMESPACE" "$victim"

# Tras el delete, el operador debe promover una réplica. Esperamos a:
#   1. Que TODAS las instancias del cluster vuelvan a Ready.
#   2. Que el primary actual sea distinto del que matamos.
wait_pods_ready "$NAMESPACE" "$ALL_SELECTOR"

if [[ "$CHAOS_DRY_RUN" != "1" ]]; then
  end=$(( $(date +%s) + 600 ))
  while (( $(date +%s) < end )); do
    new_primary="$(kubectl -n "$NAMESPACE" get pods \
      -l "$PRIMARY_SELECTOR" \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    if [[ -n "$new_primary" && "$new_primary" != "$old_primary" ]]; then
      log "failover OK: primary $old_primary → $new_primary"
      exit 0
    fi
    log "esperando promoción de nuevo primary (actual='${new_primary:-none}')..."
    sleep 5
  done
  echo "chaos: no se observó promoción de nuevo primary distinto de $old_primary" >&2
  exit 1
fi

log "cnpg failover chaos completado"
