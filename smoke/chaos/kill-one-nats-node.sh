#!/usr/bin/env bash
#
# Mata un nodo de NATS y espera a que el StatefulSet vuelva a Ready.
#
# Justificación de selectores:
#   - El cluster NATS forma parte del control plane referenciado en
#     `docs/architecture/adr/ADR-0012-data-plane-slos.md` (SLO
#     `dp-slo-nats`, métricas `nats_control_event_e2e_seconds_*`).
#   - El chart oficial de NATS (Apache-2.0) etiqueta los pods con
#     `app.kubernetes.io/name=nats` (convención estable upstream),
#     parametrizable via env si el deployment local usa otro selector.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${NATS_NAMESPACE:-nats}"
SELECTOR="${NATS_SELECTOR:-app.kubernetes.io/name=nats}"

require_kubectl

victim="$(pick_victim_pod "$NAMESPACE" "$SELECTOR")"
log "víctima nats: $victim"
delete_pod "$NAMESPACE" "$victim"

wait_pods_ready "$NAMESPACE" "$SELECTOR"

log "nats node chaos completado"
