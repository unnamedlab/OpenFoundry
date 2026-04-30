#!/usr/bin/env bash
#
# Mata un nodo de ClickHouse Keeper y espera a que el ensemble recupere
# quorum (todos los pods Ready de nuevo).
#
# Justificación de selectores:
#   - Namespace `clickhouse` y CHK `openfoundry` → ver
#     `infra/k8s/clickhouse/keeper.yaml` (kind: ClickHouseKeeperInstallation,
#     name=openfoundry, namespace=clickhouse).
#   - El operador Altinity Keeper etiqueta los pods con
#     `clickhouse-keeper.altinity.com/chk=<chk>` y
#     `clickhouse-keeper.altinity.com/app=chop`.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${KEEPER_NAMESPACE:-clickhouse}"
CHK="${KEEPER_CHK:-openfoundry}"
SELECTOR="clickhouse-keeper.altinity.com/chk=$CHK,clickhouse-keeper.altinity.com/app=chop"

require_kubectl

victim="$(pick_victim_pod "$NAMESPACE" "$SELECTOR")"
log "víctima keeper: $victim"
delete_pod "$NAMESPACE" "$victim"

# Quorum reformado ⇒ todos los pods del ensemble Ready.
wait_pods_ready "$NAMESPACE" "$SELECTOR"

log "keeper chaos completado"
