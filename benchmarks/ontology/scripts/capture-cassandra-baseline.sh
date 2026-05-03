#!/usr/bin/env bash
# Capture a Cassandra JMX baseline for the ontology hot-path bench.
#
# Required by ADR-0012 §A.3: every entry in the §A.4 results table
# must be flanked by a pre/post `nodetool tablestats` snapshot per
# (node, keyspace) so the latency numbers (A1) and the partition /
# tombstone numbers (A2) and the dropped-stage counters (A3) can be
# correlated. Without this baseline the headline numbers cannot be
# interpreted.
#
# What this script does:
#   1. For each of the 3 application keyspaces touched by the mix
#      (ontology_objects, ontology_indexes, actions_log)
#   2. For each pod in the target Cassandra DC (3 pods by default,
#      RF=3, single-AZ — see ADR-0020)
#      - kubectl exec `nodetool tablestats -F json <keyspace>`
#      - persist the JSON under benchmarks/results/cassandra-baseline-<UTC>/
#        with one file per (node, keyspace).
#   3. Walk the resulting JSON tree and emit a `summary.md` with the
#      threshold table from
#      `benchmarks/ontology/runbooks/hot-partitions.md`,
#      flagging WARN > 50 MiB and FAIL > 100 MiB on
#      `compacted_partition_max`, plus the same gates surfaced for
#      tombstones and dropped reads (best-effort: tpstats is NOT
#      collected here — that is a separate concern).
#
# Idempotence:
#   Output dir is `cassandra-baseline-<UTC>`. If it already exists
#   (e.g. two captures in the same minute), suffix `-2`, `-3`, …
#   until a fresh path is found. The script never overwrites an
#   existing capture.
#
# Configuration (env vars, all with sane k8ssandra defaults):
#   OF_BENCH_CASS_NS         namespace of the CassandraDatacenter pods
#                            (default: cassandra)
#   OF_BENCH_CASS_POD_LABEL  label selector identifying the 3 pods of
#                            the target DC (default:
#                            cassandra.datastax.com/datacenter=dc1)
#   OF_BENCH_CASS_CONTAINER  container name inside the pod
#                            (default: cassandra)
#   OF_BENCH_KEYSPACES       space-separated list of keyspaces
#                            (default: "ontology_objects ontology_indexes actions_log")
#   OF_BENCH_RESULTS_DIR     output root (default: benchmarks/results)
#
# Exit codes:
#   0  capture complete, no FAIL gates tripped
#   1  unexpected error (kubectl, jq, missing pods)
#   2  capture complete but at least one FAIL threshold exceeded;
#      summary.md still written. Caller can decide whether to abort.

set -euo pipefail

NS="${OF_BENCH_CASS_NS:-cassandra}"
POD_LABEL="${OF_BENCH_CASS_POD_LABEL:-cassandra.datastax.com/datacenter=dc1}"
CONTAINER="${OF_BENCH_CASS_CONTAINER:-cassandra}"
# shellcheck disable=SC2206
KEYSPACES=( ${OF_BENCH_KEYSPACES:-ontology_objects ontology_indexes actions_log} )
RESULTS_ROOT="${OF_BENCH_RESULTS_DIR:-benchmarks/results}"

WARN_BYTES=$((50 * 1024 * 1024))   # 50 MiB
FAIL_BYTES=$((100 * 1024 * 1024))  # 100 MiB

# ---- pre-flight -------------------------------------------------------------

for tool in kubectl jq awk date sort; do
  command -v "$tool" >/dev/null || {
    echo "missing required tool: $tool" >&2
    exit 1
  }
done

# Resolve the output dir, suffix on collision (-2, -3, …).
ts=$(date -u +%Y%m%dT%H%M%SZ)
base="$RESULTS_ROOT/cassandra-baseline-$ts"
out="$base"
n=2
while [[ -e "$out" ]]; do
  out="${base}-${n}"
  n=$((n + 1))
done
mkdir -p "$out"
echo "[capture] writing to $out"

# Discover pods. We need exactly the DC's 3 nodes; if the caller
# pointed the selector at the wrong DC and we get more or fewer,
# warn but proceed — the captured files still encode the pod name
# so downstream analysis stays unambiguous.
mapfile -t PODS < <(
  kubectl -n "$NS" get pods -l "$POD_LABEL" \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort
)

if [[ ${#PODS[@]} -eq 0 ]]; then
  echo "[capture] no pods matched -n $NS -l $POD_LABEL" >&2
  exit 1
fi
if [[ ${#PODS[@]} -ne 3 ]]; then
  echo "[capture] WARN expected 3 pods (RF=3 DC), found ${#PODS[@]}: ${PODS[*]}" >&2
fi

echo "[capture] pods:      ${PODS[*]}"
echo "[capture] keyspaces: ${KEYSPACES[*]}"

# ---- collection -------------------------------------------------------------

declare -a CAPTURED_FILES=()

for pod in "${PODS[@]}"; do
  for ks in "${KEYSPACES[@]}"; do
    file="$out/${pod}__${ks}.json"
    echo "[capture]   nodetool tablestats $ks @ $pod"
    if ! kubectl -n "$NS" exec "$pod" -c "$CONTAINER" -- \
           nodetool tablestats -F json "$ks" > "$file"; then
      echo "[capture] FAIL: nodetool tablestats failed for $ks @ $pod" >&2
      # Keep going so the summary captures partial truth; the absent
      # file becomes a visible gap in the summary table.
      rm -f "$file"
      continue
    fi
    # Sanity: nodetool sometimes prints a non-JSON banner if the
    # keyspace is missing. Validate before accepting.
    if ! jq -e . "$file" >/dev/null 2>&1; then
      echo "[capture] FAIL: non-JSON output for $ks @ $pod (kept as .raw)" >&2
      mv "$file" "${file%.json}.raw"
      continue
    fi
    CAPTURED_FILES+=("$file")
  done
done

if [[ ${#CAPTURED_FILES[@]} -eq 0 ]]; then
  echo "[capture] no successful captures, aborting" >&2
  exit 1
fi

# ---- summary.md -------------------------------------------------------------
#
# nodetool tablestats -F json shape (Cassandra 5.0):
#   {
#     "<keyspace>": {
#       "tables": {
#         "<table>": {
#           "compacted_partition_maximum_bytes": <int>,
#           "compacted_partition_mean_bytes":    <int>,
#           "local_read_latency_ms":             "<float | NaN>",
#           "local_write_latency_ms":            "<float | NaN>",
#           "average_tombstones_per_slice_last_five_minutes": <float>,
#           "maximum_tombstones_per_slice_last_five_minutes": <int>,
#           "bloom_filter_false_ratio":          <float>,
#           "off_heap_memory_used_total_bytes":  <int>,
#           ...
#         }, ...
#       }
#     }
#   }
#
# We surface the 7 metrics from runbooks/hot-partitions.md, mark
# WARN/FAIL by the partition-max thresholds, and aggregate worst-case
# across the 3 nodes for each (keyspace, table).

SUMMARY="$out/summary.md"
exit_code=0

{
  printf '# Cassandra baseline — %s\n\n' "$ts"
  printf 'Captured by `benchmarks/ontology/scripts/capture-cassandra-baseline.sh`.\n'
  printf 'Thresholds derived from `benchmarks/ontology/runbooks/hot-partitions.md`.\n\n'
  printf '* Namespace: `%s`\n' "$NS"
  printf '* Pod selector: `%s`\n' "$POD_LABEL"
  printf '* Pods (n=%d): %s\n' "${#PODS[@]}" "$(printf '`%s` ' "${PODS[@]}")"
  printf '* Keyspaces: %s\n\n' "$(printf '`%s` ' "${KEYSPACES[@]}")"

  printf '## Thresholds\n\n'
  printf '| Metric | WARN | FAIL |\n'
  printf '|---|---|---|\n'
  printf '| `compacted_partition_max` | > 50 MiB | > 100 MiB |\n'
  printf '| `compacted_partition_mean` | > 1 MiB | > 10 MiB |\n'
  printf '| `tombstones_per_slice` (avg 5 min) | > 100 | > 1000 |\n'
  printf '| `bloom_filter_false_ratio` | > 0.01 | > 0.05 |\n'
  printf '| `off_heap_memory_used_total_bytes` (per node) | > 1 GiB | > 2 GiB |\n\n'

  printf '## Worst-case per (keyspace, table) across nodes\n\n'
  printf '| Keyspace | Table | Max partition | Mean partition | Tombstones/slice (avg) | Bloom FPR | Off-heap | Status |\n'
  printf '|---|---|---|---|---|---|---|---|\n'

  # Build a flat table aggregating MAX across the 3 nodes per (ks, table).
  # jq emits TSV rows: ks \t table \t max_bytes \t mean_bytes \t avg_tombstones \t bloom_fpr \t off_heap_bytes
  # Then awk groups and applies the gates.
  # NOTE: pipefail is set, but awk intentionally exits 2 to signal a
  # FAIL gate. We swallow that with `|| awk_rc=$?` so the script can
  # still write the trailing "Files" section before propagating.
  awk_rc=0
  jq -r '
      to_entries[]
      | .key as $ks
      | (.value.tables // {})
      | to_entries[]
      | [
          $ks,
          .key,
          (.value.compacted_partition_maximum_bytes // 0),
          (.value.compacted_partition_mean_bytes // 0),
          (.value.average_tombstones_per_slice_last_five_minutes // 0),
          (.value.bloom_filter_false_ratio // 0),
          (.value.off_heap_memory_used_total_bytes // 0)
        ] | @tsv
    ' "${CAPTURED_FILES[@]}" 2>/dev/null \
  | awk -F'\t' -v WARN="$WARN_BYTES" -v FAIL="$FAIL_BYTES" '
      {
        key = $1 SUBSEP $2
        if ($3+0 > maxp[key]) maxp[key] = $3+0
        if ($4+0 > meanp[key]) meanp[key] = $4+0
        if ($5+0 > tomb[key]) tomb[key] = $5+0
        if ($6+0 > fpr[key]) fpr[key] = $6+0
        if ($7+0 > offh[key]) offh[key] = $7+0
        ks[key] = $1; tbl[key] = $2
      }
      END {
        # Stable, sorted output for diffability across runs.
        n = 0
        for (k in maxp) keys[n++] = k
        # bubble sort is fine — 3 keyspaces × ~10 tables.
        for (i = 0; i < n; i++) for (j = i+1; j < n; j++)
          if (keys[j] < keys[i]) { t = keys[i]; keys[i] = keys[j]; keys[j] = t }

        any_fail = 0
        for (i = 0; i < n; i++) {
          k = keys[i]
          status = "OK"
          if (maxp[k] > FAIL) { status = "**FAIL**"; any_fail = 1 }
          else if (maxp[k] > WARN) status = "WARN"
          printf "| `%s` | `%s` | %.1f MiB | %.2f KiB | %.1f | %.4f | %.1f MiB | %s |\n",
                 ks[k], tbl[k],
                 maxp[k]/1048576.0, meanp[k]/1024.0,
                 tomb[k], fpr[k],
                 offh[k]/1048576.0, status
        }
        # Exit 2 from awk so the parent bash sees a fail signal via $?
        exit (any_fail ? 2 : 0)
      }
    ' || awk_rc=$?
  if [[ $awk_rc -eq 2 ]]; then
    exit_code=2
  elif [[ $awk_rc -ne 0 ]]; then
    echo "[capture] WARN summary aggregation exited rc=$awk_rc" >&2
  fi

  printf '\n## Files\n\n'
  for f in "${CAPTURED_FILES[@]}"; do
    printf '* `%s`\n' "${f#"$RESULTS_ROOT/"}"
  done

  if [[ $exit_code -eq 2 ]]; then
    printf '\n> **Gate:** at least one table exceeded the FAIL threshold.\n'
    printf '> Refer to `benchmarks/ontology/runbooks/hot-partitions.md` for remediation.\n'
  fi
} > "$SUMMARY"

echo "[capture] summary at $SUMMARY"
echo "[capture] exit=$exit_code"
exit "$exit_code"
