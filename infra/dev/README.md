# Dev cluster overlays

Single-node manifests for Lima/k3s development clusters. **Mismo software
y mismas piezas que producción** — distintas escalas/redundancia.

| Pieza | Dev | Prod (`infra/helm/infra/`) |
|---|---|---|
| Rook-Ceph operator | helm v1.19.5 (CSI desactivado) | helm v1.19.5 (CSI activo) |
| CephCluster | 1 mon, 1 mgr, directory OSD | 5 mon, 2 mgr, raw block OSDs |
| CephObjectStore (RGW) | 1 instance, replicated×1 | 3 instances, EC 8+3 |
| Lakekeeper | 1 replica, OIDC stub | 3 replicas, OIDC real |

Apply:

```sh
kubectl apply -f infra/dev/ceph-single-node.yaml
kubectl apply -f infra/dev/lakekeeper-single-node.yaml  # after Ceph HEALTH_OK
```
