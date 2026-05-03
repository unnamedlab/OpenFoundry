#!/usr/bin/env bash
# Pobla `object-ids.txt` con IDs reales del tenant de benchmarks.
#
# Uso:
#   export OF_BENCH_BASE_URL=…
#   export OF_BENCH_TOKEN=…
#   export OF_BENCH_TENANT=tenant-bench
#   export OF_BENCH_TYPE_ID=Aircraft
#   export OF_BENCH_SEED_COUNT=10000     # opcional, default 10 000
#   ./benchmarks/ontology/k6/seed.sh
#
# Estrategia: paginar `GET …/by-type/{type_id}` con `size=500` y
# acumular hasta `OF_BENCH_SEED_COUNT`. Si el tenant no tiene suficientes
# objetos, hace fallback a `apply_object_with_outbox` vía
# `POST /api/v1/ontology/actions/{seed-action}/execute` (requiere
# `OF_BENCH_SEED_ACTION_ID`) hasta llegar al número objetivo.
#
# El fichero resultante es el input de `OF_BENCH_OBJECT_IDS` en
# `ontology-mix.js`.

set -euo pipefail

: "${OF_BENCH_BASE_URL:?OF_BENCH_BASE_URL no definida}"
: "${OF_BENCH_TOKEN:?OF_BENCH_TOKEN no definida}"
: "${OF_BENCH_TENANT:?OF_BENCH_TENANT no definida}"
: "${OF_BENCH_TYPE_ID:?OF_BENCH_TYPE_ID no definida}"
TARGET=${OF_BENCH_SEED_COUNT:-10000}
OUT=${1:-./benchmarks/ontology/k6/object-ids.txt}

mkdir -p "$(dirname "$OUT")"
: > "$OUT"

cursor=""
while [[ $(wc -l <"$OUT") -lt $TARGET ]]; do
  url="$OF_BENCH_BASE_URL/api/v1/ontology/objects/$OF_BENCH_TENANT/by-type/$OF_BENCH_TYPE_ID?size=500"
  if [[ -n "$cursor" ]]; then
    url+="&token=$cursor"
  fi
  resp=$(curl -fsS -H "Authorization: Bearer $OF_BENCH_TOKEN" -H 'X-Consistency: eventual' "$url")
  echo "$resp" | jq -r '.items[] | .id' >> "$OUT"
  cursor=$(echo "$resp" | jq -r '.next_token // empty')
  if [[ -z "$cursor" ]]; then
    break
  fi
done

echo "$(wc -l <"$OUT") ids escritos a $OUT"
