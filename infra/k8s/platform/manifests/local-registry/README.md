# Local image registry (k3s + lima)

Dev-only Docker registry that lives **inside** the k3s cluster so that
`helmfile -e dev apply` can pull OpenFoundry service images without
internet access. Designed for the lima multi-node k3s setup. Not
intended for staging/prod — use GHCR or your enterprise registry there.

## Architecture

```
Mac (Docker Desktop)                lima k3s cluster
─────────────────────              ─────────────────────────────────
docker push ───────►  192.168.105.3:30501  (NodePort on master)
                                  │
                                  ▼ kube-proxy
                          ┌──────────────────┐
                          │  Service:5000    │
                          │  Deployment      │
                          │  registry:2      │
                          │  PVC 50Gi (RWO)  │
                          └──────────────────┘
                                  ▲
                                  │ http://127.0.0.1:30501
                                  │ (registries.yaml mirror)
                                  │
kubelet pulls "localhost:5001/<repo>:<tag>" ◄── containerd on each node
```

Two endpoints, same registry:

| Use   | Endpoint                | Why                                                                                               |
| ----- | ----------------------- | ------------------------------------------------------------------------------------------------- |
| Push  | `192.168.105.3:30501`   | Docker Desktop's daemon is in a Linux VM; its `localhost` is not the Mac's. Use the lima node IP. |
| Pull  | `localhost:5001`        | Containerd on each node mirrors that name to `http://127.0.0.1:30501` (NodePort on the same box). |

The two views resolve to the same blobs — repository names are stored
without the hostname prefix.

## One-time host setup

Add the lima node IPs to `~/.docker/daemon.json`:

```jsonc
{
  "insecure-registries": [
    "192.168.105.3:30501",
    "192.168.105.4:30501",
    "192.168.105.2:30501"
  ]
}
```

Restart Docker Desktop after editing.

## Deploy / refresh the registry

```sh
./infra/k8s/platform/manifests/local-registry/setup.sh
```

Idempotent. Re-running:

- applies `registry.yaml` (namespace, PVC, Deployment, Service)
- copies `registries.yaml` to `/etc/rancher/k3s/registries.yaml` on each lima node
- restarts `k3s-agent` on workers and `k3s` on master (~10s control-plane blip)

## Inner-loop workflow

```sh
IMG=192.168.105.3:30501/edge-gateway-service:dev

# build for the cluster's arch (lima nodes are linux/arm64)
docker buildx build --platform linux/arm64 -t "$IMG" --push \
  -f services/edge-gateway-service/Dockerfile .

# point the chart at the local registry
helm upgrade --reuse-values -n openfoundry of-platform \
  ./infra/k8s/helm/of-platform \
  --set edgeGatewayService.image.repository=localhost:5001/edge-gateway-service \
  --set edgeGatewayService.image.tag=dev
```

`docker buildx build --push` and `docker push` both work; buildx is
preferred because it builds + pushes in one step and caches layers.

## Inspect

```sh
# what's in the registry
curl -s http://192.168.105.3:30501/v2/_catalog | jq

# tags for a repo
curl -s http://192.168.105.3:30501/v2/edge-gateway-service/tags/list | jq

# from inside the cluster (sanity check the mirror)
kubectl run debug --rm -it --restart=Never \
  --image=localhost:5001/edge-gateway-service:dev -- /bin/sh
```

## Tear down

```sh
kubectl delete -f infra/k8s/platform/manifests/local-registry/registry.yaml
for n in k3s-master k3s-node1 k3s-node2; do
  limactl shell $n -- sudo rm -f /etc/rancher/k3s/registries.yaml
done
limactl shell k3s-master -- sudo systemctl restart k3s
limactl shell k3s-node1 -- sudo systemctl restart k3s-agent
limactl shell k3s-node2 -- sudo systemctl restart k3s-agent
```

Image data is on `local-path` PVC bound to one node — a `kubectl delete pvc`
clears it.

## Limitations

- **Single replica**: PVC is RWO on `local-path`. If the host node dies, the
  registry pod is rescheduled but the data does not move with it.
  Acceptable for dev — for HA, swap to a `MinIO`-backed registry or a CSI
  driver that supports RWX.
- **HTTP only**: insecure on purpose. Keep this off staging/prod clusters.
- **Push depends on master being reachable**: 192.168.105.3 is the master
  IP. NodePort 30501 also lives on `192.168.105.4` and `.2`; switch the
  `IMG` prefix if master is down.
