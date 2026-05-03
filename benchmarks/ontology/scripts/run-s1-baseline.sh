#!/usr/bin/env bash
# Run the Stream S1 ontology baseline and render an ADR-0012 snippet.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

for tool in k6 jq git python3 date wc; do
  command -v "$tool" >/dev/null || {
    echo "missing required tool: $tool" >&2
    exit 1
  }
done

required_env=(
  OF_BENCH_BASE_URL
  OF_BENCH_TOKEN
  OF_BENCH_TENANT
  OF_BENCH_TYPE_ID
  OF_BENCH_OBJECT_IDS
  OF_BENCH_ACTION_ID
)

for name in "${required_env[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing required env var: $name" >&2
    exit 1
  fi
done

if [[ ! -f "$OF_BENCH_OBJECT_IDS" ]]; then
  echo "OF_BENCH_OBJECT_IDS does not point to a file: $OF_BENCH_OBJECT_IDS" >&2
  exit 1
fi

object_count="$(wc -l < "$OF_BENCH_OBJECT_IDS" | tr -d ' ')"
if [[ "$object_count" -lt 1000 ]]; then
  echo "need at least 1000 object ids for a representative run; got $object_count" >&2
  exit 1
fi

RESULTS_DIR="${OF_BENCH_RESULTS_DIR:-benchmarks/results}"
RUN_ID="${OF_BENCH_RUN_ID:-s1-ontology-$(date -u +%Y%m%dT%H%M%SZ)}"
SUMMARY="$RESULTS_DIR/ontology-mix-summary.json"
RAW="$RESULTS_DIR/ontology-mix-k6.json"
METADATA="$RESULTS_DIR/ontology-mix-metadata.json"
ADR_SNIPPET="$RESULTS_DIR/adr-0012-s1-baseline.md"

mkdir -p "$RESULTS_DIR"

if [[ "${OF_BENCH_CAPTURE_CASSANDRA:-0}" == "1" ]]; then
  OF_BENCH_RESULTS_DIR="$RESULTS_DIR" \
    bash benchmarks/ontology/scripts/capture-cassandra-baseline.sh
fi

jq -n \
  --arg run_id "$RUN_ID" \
  --arg date_utc "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg commit "$(git rev-parse --short HEAD)" \
  --arg workspace_dirty "$(git diff --quiet && echo false || echo true)" \
  --arg environment "${OF_BENCH_ENVIRONMENT:-unrecorded-live-cluster}" \
  --arg dataset "${OF_BENCH_DATASET:-tenant=$OF_BENCH_TENANT,type=$OF_BENCH_TYPE_ID,objects=$object_count}" \
  --arg command "k6 run --summary-export=$SUMMARY --out json=$RAW benchmarks/ontology/k6/ontology-mix.js" \
  '{
    run_id: $run_id,
    date_utc: $date_utc,
    commit: $commit,
    workspace_dirty: $workspace_dirty,
    environment: $environment,
    dataset: $dataset,
    command: $command
  }' > "$METADATA"

OF_BENCH_RUN_ID="$RUN_ID" \
  k6 run \
    --summary-export="$SUMMARY" \
    --out "json=$RAW" \
    benchmarks/ontology/k6/ontology-mix.js

if [[ "${OF_BENCH_CAPTURE_CASSANDRA:-0}" == "1" ]]; then
  OF_BENCH_RESULTS_DIR="$RESULTS_DIR" \
    bash benchmarks/ontology/scripts/capture-cassandra-baseline.sh
fi

python3 benchmarks/ontology/scripts/render-adr-0012-s1-baseline.py \
  --summary "$SUMMARY" \
  --metadata "$METADATA" \
  --output "$ADR_SNIPPET"

echo "wrote $RAW"
echo "wrote $SUMMARY"
echo "wrote $METADATA"
echo "wrote $ADR_SNIPPET"
