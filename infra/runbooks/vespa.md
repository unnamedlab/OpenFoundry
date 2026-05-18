# Vespa Runbook

Date: April 29, 2026

OpenFoundry uses **Vespa.ai** (Apache-2.0) as a hybrid search engine
(BM25 + ANN over dense embeddings) for the *ontology semantic search*
layer and for `knowledge-index-service` knowledge bases (the retired `ai-service` name is only a gateway legacy alias; see
`docs/ontology-building/semantic-search.md`).

| Resource                       | Path / name                                                |
|--------------------------------|------------------------------------------------------------|
| Canonical application package  | `infra/k8s/platform/packages/vespa-app/`                                     |
| Helm release                   | `infra/k8s/platform/helmfile.yaml.gotmpl` (`vespa`)        |
| Chart source                   | `infra/k8s/platform/charts/vespa/`                         |
| Package mirror in the chart    | `infra/k8s/platform/charts/vespa/files/`                   |
| App chart toggle               | `vespa.enabled=false` in `of-ontology/values-*.yaml`       |
| Storage backend                | Ceph RBD (Task 2.1) — `storageClassName: ceph-rbd`         |

## 1. Deployed architecture

| Component                  | Configuration                                                    |
|----------------------------|------------------------------------------------------------------|
| `admin` cluster            | 3 config-server / cluster-controller / slobrok (StatefulSet)     |
| `default` container cluster| 2 stateless nodes (StatefulSet, query + feed entry-point)        |
| `documents` content cluster| 3 nodes, `redundancy=2`, `searchable-copies=1`                   |
| Schema                     | `document.sd` — BM25(title, body) + tensor `embedding[768]` HNSW |
| PDB configserver           | `minAvailable=2`                                                  |
| PDB content                | `minAvailable=2`                                                  |
| Persistence                | PVC `var` per pod on `ceph-rbd` (config 5Gi / content 50Gi)      |
| Query/feed endpoint        | `http://of-ontology-vespa.<ns>.svc.cluster.local:8080`           |
| Deploy endpoint            | `http://of-ontology-vespa-configserver-lb.<ns>:19071`            |
| Prometheus metrics         | port `19092`, `/prometheus/v1/values?consumer=prometheus`        |

### K8s ↔ Vespa hostname mapping

Each pod in the StatefulSet gets a stable DNS name
`<pod>.<headless-svc>.<ns>.svc.cluster.local`. The chart automatically
generates `hosts.xml` with those names (based on `release`, `namespace`,
and the counts in `values.yaml`). If you deploy the package manually,
edit `infra/k8s/platform/packages/vespa-app/hosts.xml` to reflect your
release/namespace before zipping.

## 2. Deployment

### 2.1 Via Helm (recommended)

```bash
# Production: Ceph RBD already provisioned by Task 2.1
cd infra/k8s/platform
helmfile -e prod apply
```

The `vespa` release creates, in order:

1. ServiceAccount + headless Services (configserver, content, container).
2. ConfigMap `*-vespa-app` with the package (services.xml, hosts.xml,
   schemas/*.sd).
3. StatefulSets — the pods wait in their `initContainer` for the
   configservers to respond on `:19071`.
4. PDBs (configserver `minAvailable=2`, content `minAvailable=2`).
5. **Job `*-vespa-deploy-<sha10>`** (Helm hook `post-install,post-upgrade`)
   that rebuilds the directory tree, runs `zip -r`, and POSTs to the
   configservers' `prepareandactivate`. It retries up to
   `backoffLimit=30` to tolerate the initial bring-up.

> The Job name embeds the SHA-256 of the package; changing any file
> under `platform/charts/vespa/files/` produces a new Job on the next
> `helmfile apply`.

### 2.2 Manifest validation

```bash
helm lint infra/k8s/platform/charts/vespa
( cd infra/k8s/platform && helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor" ) \
  | kubectl apply --dry-run=server -f -
```

### 2.3 Manual package deployment (without Helm)

```bash
( cd infra/k8s/platform/packages/vespa-app && zip -r /tmp/vespa-app.zip . )
kubectl -n openfoundry port-forward svc/of-ontology-vespa-configserver-lb 19071:19071 &
curl -fsS --header "Content-Type: application/zip" \
  --data-binary @/tmp/vespa-app.zip \
  http://localhost:19071/application/v2/tenant/default/prepareandactivate \
  | jq .
```

## 3. Rolling upgrade of the application package

1. Edit the files in `infra/k8s/platform/packages/vespa-app/` **and** copy the changes
   to the mirror at `infra/k8s/platform/charts/vespa/files/`
   (rule: the mirror is what ends up in the ConfigMap; both must match
   bit-for-bit).
2. Commit and review `helm template` to confirm the Job SHA changes:
   `( cd infra/k8s/platform && helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor" ) | grep -E 'name: .*vespa-deploy'`.
3. `helmfile -e prod apply`. The new Job will run `prepareandactivate` and
   the relevant nodes will *reload-on-the-fly*:
   - Schemas with compatible changes (new fields, new rank-profiles)
     **do not require a restart** and are applied online.
   - Incompatible changes require a *reindex*: launch it from the container
     ```bash
     kubectl -n openfoundry exec deploy/<container-pod> -- \
       vespa-reindex --cluster documents --type document
     ```
4. Verify:
   ```bash
   curl -fsS http://<lb>:19071/application/v2/tenant/default/application/default | jq .
   ```

## 4. Expanding the content cluster

Raising `vespa.content.replicas` from 3 → 5 (for example) requires
**two steps** because the topology is defined by the package, not by Helm:

1. Edit `infra/k8s/platform/packages/vespa-app/services.xml` and `hosts.xml` to add
   the new nodes (`vespa-content-3`, `vespa-content-4`) and replicate the
   change in the chart mirror.
2. Update `infra/k8s/platform/values/vespa-prod.yaml` with
   `content.replicas=5` and run `helmfile -e prod apply`.
   - The StatefulSet scales first.
   - The deploy Job publishes the updated package.
   - The Vespa cluster-controller redistributes the buckets to the new
     nodes automatically; `redundancy` and `searchable-copies` are
     preserved.
3. Monitor the migration:
   ```bash
   kubectl -n openfoundry exec <content-0> -- vespa-get-cluster-state
   ```
   The `Init` column must drop to 0 and `Up` must match the new total
   before the expansion can be considered complete.

> **Do not** lower `redundancy` or `searchable-copies` during the
> expansion: do it in a later upgrade once the balancing has finished.

## 5. Node recovery

### 5.1 Configserver

The 3 configservers form a ZooKeeper quorum (PDB `minAvailable=2`):

* If **one** node goes down the cluster keeps operating. Kubernetes will
  recreate the pod and the RBD PVC reattaches automatically; the new
  process resyncs state from the other two.
* If **two** nodes go down at the same time, quorum is lost: feed and
  deploy block (queries keep working). Action:
  ```bash
  kubectl -n openfoundry get pods -l app.kubernetes.io/component=vespa-configserver
  kubectl -n openfoundry logs <pod> -c configserver --tail=200
  ```
  If the PVC is corrupt, delete it and let the `volumeClaimTemplate`
  recreate the volume — the remaining node will reseed the state.

### 5.2 Content

PDB `minAvailable=2` protects against drains that would leave `redundancy < 2`.
To replace a content node:

```bash
# 1. Mark the node "retired" so it migrates its buckets away
kubectl -n openfoundry exec <content-0> -- \
  vespa-set-node-state --type content --index 2 retired

# 2. Wait for active buckets on index 2 to fall to 0
kubectl -n openfoundry exec <content-0> -- vespa-get-cluster-state

# 3. Delete the pod and its PVC; the StatefulSet recreates it
kubectl -n openfoundry delete pvc var-of-ontology-vespa-content-2
kubectl -n openfoundry delete pod of-ontology-vespa-content-2

# 4. Mark it "up" again
kubectl -n openfoundry exec <content-0> -- \
  vespa-set-node-state --type content --index 2 up
```

### 5.3 Container (stateless)

It has no PVC. `kubectl delete pod <container-N>` is enough — the new pod
runs `wait-configservers` and starts up.

## 6. Observability (Prometheus)

Each pod exposes the *metrics-proxy* endpoint on port **19092**:

```
GET /prometheus/v1/values?consumer=prometheus
```

The `prometheus` consumer is declared in `services.xml` with the
`default` + `vespa` metric-sets.

### 6.1 Scrape via ServiceMonitor

If `kube-prometheus-stack` is installed, enable:

```yaml
vespa:
  metrics:
    serviceMonitor:
      enabled: true
      interval: 30s
```

The chart renders a single `ServiceMonitor` that covers the three
roles (configserver, content, container) using a `matchExpressions`
on the `app.kubernetes.io/component` label.

### 6.2 Manual scrape

```bash
kubectl -n openfoundry port-forward <content-0> 19092:19092
curl -s 'http://localhost:19092/prometheus/v1/values?consumer=prometheus' | head
```

### 6.3 Key metrics

| Metric                                           | Used for                              |
|--------------------------------------------------|---------------------------------------|
| `content_proton_documentdb_documents_total`      | Corpus size per schema                |
| `content_proton_documentdb_disk_usage`           | RBD disk growth                       |
| `content_proton_resource_usage_disk`             | Feed-block trigger (>0.85 = pause)    |
| `content_proton_resource_usage_memory`           | Feed-block memory                     |
| `vds_distributor_docsstored`                     | Replication across content nodes      |
| `vds_filestor_alldisks_queuesize`                | Mutation backlog                      |
| `searchnode_documentdb_matching_query_latency`   | Query latency (p50/p95/p99)           |
| `container_http_requests_per_second`             | RPS hitting the container             |
| `container_http_status_5xx_rate`                 | Feed/query errors                     |
| `cluster-controller_resource_usage_nodes_above_limit` | Nodes in feed-block              |

### 6.4 Logs

```bash
kubectl -n openfoundry logs -f <pod> -c vespa
# Vespa also writes to /opt/vespa/logs/vespa/vespa.log inside the pod
kubectl -n openfoundry exec <pod> -- tail -F /opt/vespa/logs/vespa/vespa.log
```
