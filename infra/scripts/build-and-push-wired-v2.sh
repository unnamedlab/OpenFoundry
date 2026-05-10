#!/usr/bin/env bash
# build-and-push-wired-v2.sh — push to 192.168.105.2:30501 (Lima k3s gateway IP).
# Lower parallelism (-P 3) to avoid registry overload; rebuild only services whose
# image tag is not already present locally for the v2 registry.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

REGISTRY="${REGISTRY:-192.168.105.2:30501}"
LIST="${1:-/tmp/wired-services.txt}"
LOG=/tmp/wired-build-v2.log
: > "$LOG"

build_one() {
  local svc="$1"
  local image_local="localhost:30501/${svc}:0.1.0"
  local image_target="${REGISTRY}/${svc}:0.1.0"
  local start=$(date +%s)
  local logf="/tmp/build2-${svc}.log"
  : > "$logf"

  # If we already have the local image (built this session), retag instead of rebuilding.
  if docker image inspect "$image_local" >/dev/null 2>&1; then
    echo "$(date -Iseconds) RETAG ${svc}" >> "$logf"
    docker tag "$image_local" "$image_target" >> "$logf" 2>&1 || {
      echo "$(date -Iseconds) FAIL tag  ${svc}" | tee -a "$LOG"; return 1; }
  else
    if ! docker buildx build --platform linux/arm64 --load \
         -t "$image_target" -f "services/${svc}/Dockerfile" . >> "$logf" 2>&1; then
      echo "$(date -Iseconds) FAIL build ${svc} ($(($(date +%s) - start))s)" | tee -a "$LOG"
      return 1
    fi
  fi

  if ! docker push "$image_target" >> "$logf" 2>&1; then
    echo "$(date -Iseconds) FAIL push  ${svc} ($(($(date +%s) - start))s)" | tee -a "$LOG"
    return 1
  fi

  echo "$(date -Iseconds) OK    ${svc} ($(($(date +%s) - start))s)" | tee -a "$LOG"
}

export -f build_one
export REGISTRY LOG

xargs -P 3 -I{} bash -c 'build_one "$@"' _ {} < "$LIST"

echo
echo "=== Summary ==="
ok=$(grep -c ' OK ' "$LOG" 2>/dev/null)
fail=$(grep -c ' FAIL ' "$LOG" 2>/dev/null)
echo "OK:   ${ok:-0}"
echo "FAIL: ${fail:-0}"
if [ "${fail:-0}" -gt 0 ]; then
  echo "Failed services:"
  grep ' FAIL ' "$LOG" | awk '{print $4}' | sort -u
fi
