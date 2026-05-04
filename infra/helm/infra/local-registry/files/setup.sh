#!/usr/bin/env bash
# Bootstrap an in-cluster Docker registry for k3s-on-lima dev clusters.
#
# Result:
#   - registry:2 running in namespace `registry`, exposed via NodePort 30501
#   - each lima node configured to mirror image refs `localhost:5000/...`
#     to http://127.0.0.1:30501 (the NodePort served by kube-proxy)
#   - push from the host: tag and push as `<lima-node-ip>:30501/<repo>:<tag>`,
#     e.g. `docker push 192.168.105.3:30501/myimg:dev`. Requires Docker
#     Desktop daemon.json `insecure-registries` to include the lima IPs
#     (HTTP, no TLS). NO port-forward needed.
#   - pull from the cluster: image refs as `localhost:5001/<repo>:<tag>`.
#     The mirror in registries.yaml rewrites that to http://127.0.0.1:30501,
#     which kube-proxy routes to the registry pod on whatever node it lives.
#
# Why this asymmetry (push to NodeIP, pull from localhost): Docker Desktop
# runs the daemon in a Linux VM whose `localhost` is not the Mac's, so push
# via `localhost:port` cannot reach a `kubectl port-forward` listener bound
# to the Mac's loopback. Pushing directly to a lima node IP sidesteps that
# entirely. The image bytes land in the same registry; pull uses the
# in-node localhost alias that containerd already understands.
#
# Idempotent: re-running upgrades manifests and refreshes registries.yaml.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIMA_NODES=("k3s-master" "k3s-node1" "k3s-node2")
WORKER_NODES=("k3s-node1" "k3s-node2")
MASTER_NODE="k3s-master"

log() { printf '\033[1;34m▶\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✔\033[0m %s\n' "$*"; }

require() {
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "missing: $cmd" >&2; exit 1; }
  done
}

require kubectl limactl

log "Applying registry manifests"
kubectl apply -f "$SCRIPT_DIR/registry.yaml"

log "Waiting for registry pod to become Ready"
kubectl -n registry rollout status deploy/registry --timeout=120s

log "Verifying NodePort 30501 is reachable from each lima node"
for node in "${LIMA_NODES[@]}"; do
  if limactl shell "$node" -- curl -fsS --max-time 5 http://127.0.0.1:30501/v2/ >/dev/null; then
    ok "$node → http://127.0.0.1:30501/v2/ OK"
  else
    echo "✘ $node cannot reach NodePort 30501" >&2
    exit 1
  fi
done

log "Writing /etc/rancher/k3s/registries.yaml on each node"
for node in "${LIMA_NODES[@]}"; do
  limactl shell "$node" -- sudo install -d -m 0755 /etc/rancher/k3s
  limactl shell "$node" -- sudo tee /etc/rancher/k3s/registries.yaml >/dev/null < "$SCRIPT_DIR/registries.yaml"
  ok "$node ← registries.yaml installed"
done

log "Restarting k3s on workers (k3s-agent)"
for node in "${WORKER_NODES[@]}"; do
  limactl shell "$node" -- sudo systemctl restart k3s-agent
  ok "$node k3s-agent restarted"
done

log "Restarting k3s on master (control plane brief blip ~10s)"
limactl shell "$MASTER_NODE" -- sudo systemctl restart k3s
ok "master k3s restarted"

log "Waiting for nodes Ready again"
kubectl wait --for=condition=Ready nodes --all --timeout=120s

ok "Local registry ready."
cat <<'TXT'

Push workflow (host, one-time setup):
  1. Add to ~/.docker/daemon.json:
       "insecure-registries": ["192.168.105.3:30501",
                               "192.168.105.4:30501",
                               "192.168.105.2:30501"]
  2. Restart Docker Desktop.

Daily inner-loop:
  IMG=192.168.105.3:30501/<repo>:<tag>
  docker buildx build --platform linux/arm64 -t "$IMG" --push .
  # OR (slower): docker build --platform linux/arm64 -t "$IMG" . && docker push "$IMG"

Cluster references (in helm values, manifests, etc.):
  image: "localhost:5001/<repo>:<tag>"
The mirror in registries.yaml on each node redirects that to the local
NodePort, which routes to the registry pod.
Tear down: kubectl delete -f registry.yaml && remove registries.yaml on nodes.
TXT
