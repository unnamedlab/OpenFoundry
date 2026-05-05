#!/usr/bin/env bash
# build-and-push-all.sh — build & push ~98 Rust services to local-registry
# Parallelism: 2 (RAM-safe for cold builds on Apple Silicon)
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

LOG=/tmp/build-results.log
: > "$LOG"

build_one() {
  local svc="$1"
  local dockerfile="services/${svc}/Dockerfile"
  local image="localhost:30501/${svc}:0.1.0"
  local start=$(date +%s)

  if ! docker buildx build --platform linux/arm64 --load \
       -t "$image" -f "$dockerfile" . > "/tmp/build-${svc}.log" 2>&1; then
    echo "$(date -Iseconds) FAIL build ${svc} ($(($(date +%s) - start))s) see /tmp/build-${svc}.log" | tee -a "$LOG"
    return 1
  fi

  if ! docker push "$image" >> "/tmp/build-${svc}.log" 2>&1; then
    echo "$(date -Iseconds) FAIL push  ${svc} ($(($(date +%s) - start))s)" | tee -a "$LOG"
    return 1
  fi

  echo "$(date -Iseconds) OK         ${svc} ($(($(date +%s) - start))s)" | tee -a "$LOG"
}

export -f build_one

ls services/*/Dockerfile | xargs -n1 -I{} dirname {} | xargs -n1 basename \
  | xargs -P 2 -I{} bash -c 'build_one "$@"' _ {}

echo
echo "=== Summary ==="
ok=$(grep -c ' OK ' "$LOG" 2>/dev/null || echo 0)
fail=$(grep -c ' FAIL ' "$LOG" 2>/dev/null || echo 0)
echo "OK:   $ok"
echo "FAIL: $fail"
echo "Failed services:"
grep ' FAIL ' "$LOG" | awk '{print $4}' | sort -u
