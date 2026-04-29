#!/usr/bin/env bash
#
# Mata una réplica de ClickHouse y espera a que la CHI vuelva a Completed.
#
# Justificación de selectores:
#   - Namespace `clickhouse` y CHI `openfoundry` → ver
#     `infra/k8s/clickhouse/clickhouse.yaml` (kind: ClickHouseInstallation,
#     name=openfoundry, namespace=clickhouse).
#   - El operador Altinity etiqueta los pods de réplica con
#     `clickhouse.altinity.com/chi=<chi>` y
#     `clickhouse.altinity.com/app=chop` (convención estable del operator).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${CLICKHOUSE_NAMESPACE:-clickhouse}"
CHI="${CLICKHOUSE_CHI:-openfoundry}"
SELECTOR="clickhouse.altinity.com/chi=$CHI,clickhouse.altinity.com/app=chop"

require_kubectl

victim="$(pick_victim_pod "$NAMESPACE" "$SELECTOR")"
log "víctima clickhouse replica: $victim"
delete_pod "$NAMESPACE" "$victim"

wait_pods_ready "$NAMESPACE" "$SELECTOR"

# La CHI expone .status.status="Completed" cuando todos los hosts están up.
if [[ "$CHAOS_DRY_RUN" != "1" ]]; then
  end=$(( $(date +%s) + 600 ))
  while (( $(date +%s) < end )); do
    status="$(kubectl -n "$NAMESPACE" get chi "$CHI" \
      -o jsonpath='{.status.status}' 2>/dev/null || true)"
    if [[ "$status" == "Completed" ]]; then
      log "CHI status: Completed"
      exit 0
    fi
    log "CHI status aún: ${status:-desconocido}, reintentando..."
    sleep 10
  done
  echo "chaos: CHI $CHI no volvió a Completed a tiempo" >&2
  exit 1
fi

log "clickhouse replica chaos completado"
