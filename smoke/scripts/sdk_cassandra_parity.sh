#!/usr/bin/env bash
# S1.9.b — Validar SDK clients (TS/Python) contra plataforma migrada.
#
# La migración a Cassandra (S1.1–S1.8) **no debe** romper a los SDK
# existentes: `proto/` y los OpenAPI no han cambiado, así que el
# binario generado de los clientes Python y TypeScript debe seguir
# resolviendo las mismas rutas con el mismo shape de payload.
#
# Este script ejerce los caminos cubiertos por el bench S1.8 desde
# ambos SDKs y reporta un PASS/FAIL agregado. No reemplaza al smoke
# completo (`smoke-p2-runtime-critical-path`); es un diff específico
# para detectar regresiones de contrato derivadas del corte a
# Cassandra.
#
# Variables requeridas:
#   OPENFOUNDRY_BASE_URL   p.ej. https://ontology.dev.openfoundry.local
#   OPENFOUNDRY_TOKEN      bearer JWT con permiso read+execute
#   OPENFOUNDRY_TENANT     tenant id sembrado por el bench
#   OPENFOUNDRY_OBJECT_ID  uno de los ids generados por seed.sh
#   OPENFOUNDRY_TYPE_ID    object type id usado en list_by_type
#   OPENFOUNDRY_ACTION_ID  action type id sembrado para el path de write
#
# Salida: `smoke/results/sdk-cassandra-parity.json`.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RESULTS_DIR="$REPO_ROOT/smoke/results"
mkdir -p "$RESULTS_DIR"
OUT="$RESULTS_DIR/sdk-cassandra-parity.json"

: "${OPENFOUNDRY_BASE_URL:?OPENFOUNDRY_BASE_URL no definida}"
: "${OPENFOUNDRY_TOKEN:?OPENFOUNDRY_TOKEN no definida}"
: "${OPENFOUNDRY_TENANT:?OPENFOUNDRY_TENANT no definida}"
: "${OPENFOUNDRY_OBJECT_ID:?OPENFOUNDRY_OBJECT_ID no definida}"
: "${OPENFOUNDRY_TYPE_ID:?OPENFOUNDRY_TYPE_ID no definida}"
: "${OPENFOUNDRY_ACTION_ID:?OPENFOUNDRY_ACTION_ID no definida}"

ts_log=$(mktemp)
py_log=$(mktemp)
trap 'rm -f "$ts_log" "$py_log"' EXIT

# ---- TypeScript SDK -------------------------------------------------
pushd "$REPO_ROOT/sdks/typescript/openfoundry-sdk" >/dev/null
if [[ ! -d node_modules ]]; then
  pnpm install --frozen-lockfile >/dev/null 2>&1 || pnpm install >/dev/null
fi
pnpm tsx "$REPO_ROOT/smoke/scripts/sdk_cassandra_parity.ts" \
  | tee "$ts_log"
ts_exit=${PIPESTATUS[0]}
popd >/dev/null

# ---- Python SDK -----------------------------------------------------
pushd "$REPO_ROOT/sdks/python/openfoundry-sdk" >/dev/null
if [[ ! -d .venv ]]; then
  python3 -m venv .venv
  ./.venv/bin/pip install -e . >/dev/null
fi
./.venv/bin/python "$REPO_ROOT/smoke/scripts/sdk_cassandra_parity.py" \
  | tee "$py_log"
py_exit=${PIPESTATUS[0]}
popd >/dev/null

# ---- Aggregate ------------------------------------------------------
status="pass"
if [[ "$ts_exit" -ne 0 || "$py_exit" -ne 0 ]]; then
  status="fail"
fi

jq -n \
  --arg status "$status" \
  --arg ts_log "$(cat "$ts_log")" \
  --arg py_log "$(cat "$py_log")" \
  --argjson ts_exit "$ts_exit" \
  --argjson py_exit "$py_exit" \
  '{status: $status, typescript: {exit: $ts_exit, log: $ts_log},
    python: {exit: $py_exit, log: $py_log}}' \
  > "$OUT"

echo "wrote $OUT (status=$status)"
[[ "$status" == "pass" ]]
