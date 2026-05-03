# Vespa Runbook

Fecha: 29 de abril de 2026

OpenFoundry usa **Vespa.ai** (Apache-2.0) como motor de búsqueda híbrida
(BM25 + ANN sobre embeddings densos) para la capa de
*ontology semantic search* y para los *knowledge bases* de
`ai-service` (ver `docs/ontology-building/semantic-search.md`).

| Recurso                        | Ruta / nombre                                              |
|--------------------------------|------------------------------------------------------------|
| Application package canónico   | `infra/k8s/platform/packages/vespa-app/`                                     |
| Helm release                   | `infra/k8s/platform/helmfile.yaml.gotmpl` (`vespa`)        |
| Chart source                   | `infra/k8s/platform/charts/vespa/`                         |
| Mirror del package en el chart | `infra/k8s/platform/charts/vespa/files/`                   |
| Toggle app chart               | `vespa.enabled=false` en `of-ontology/values-*.yaml`       |
| Storage backend                | Ceph RBD (Tarea 2.1) — `storageClassName: ceph-rbd`        |

## 1. Arquitectura desplegada

| Componente                 | Configuración                                                    |
|----------------------------|------------------------------------------------------------------|
| `admin` cluster            | 3 config-server / cluster-controller / slobrok (StatefulSet)     |
| `default` container cluster| 2 nodos sin estado (StatefulSet, query + feed entry-point)       |
| `documents` content cluster| 3 nodos, `redundancy=2`, `searchable-copies=1`                   |
| Schema                     | `document.sd` — BM25(title, body) + tensor `embedding[768]` HNSW |
| PDB configserver           | `minAvailable=2`                                                  |
| PDB content                | `minAvailable=2`                                                  |
| Persistencia               | PVC `var` por pod en `ceph-rbd` (config 5Gi / content 50Gi)      |
| Endpoint query/feed        | `http://of-ontology-vespa.<ns>.svc.cluster.local:8080`           |
| Endpoint deploy            | `http://of-ontology-vespa-configserver-lb.<ns>:19071`            |
| Métricas Prometheus        | port `19092`, `/prometheus/v1/values?consumer=prometheus`        |

### K8s ↔ Vespa hostname mapping

Cada pod del StatefulSet recibe un DNS estable
`<pod>.<headless-svc>.<ns>.svc.cluster.local`. El chart genera
automáticamente `hosts.xml` con esos nombres (basado en `release`,
`namespace` y los counts de `values.yaml`). Si despliegas el package
manualmente, edita `infra/k8s/platform/packages/vespa-app/hosts.xml` para reflejar tu
release/namespace antes de hacer zip.

## 2. Despliegue

### 2.1 Vía Helm (recomendado)

```bash
# Producción: Ceph RBD ya provisionado por Tarea 2.1
cd infra/k8s/platform
helmfile -e prod apply
```

El release `vespa` crea, en orden:

1. ServiceAccount + headless Services (configserver, content, container).
2. ConfigMap `*-vespa-app` con el package (services.xml, hosts.xml,
   schemas/*.sd).
3. StatefulSets — los pods esperan en `initContainer` a que los
   configservers respondan en `:19071`.
4. PDBs (configserver `minAvailable=2`, content `minAvailable=2`).
5. **Job `*-vespa-deploy-<sha10>`** (Helm hook `post-install,post-upgrade`)
   que reconstruye el árbol de directorios, hace `zip -r` y POST al
   `prepareandactivate` de los configservers. Se reintenta hasta
   `backoffLimit=30` para tolerar el bring-up inicial.

> El nombre del Job embebe el SHA-256 del package; cambiar cualquier
> archivo bajo `platform/charts/vespa/files/` produce un Job nuevo en el siguiente
> `helmfile apply`.

### 2.2 Validación de manifiestos

```bash
helm lint infra/k8s/platform/charts/vespa
( cd infra/k8s/platform && helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor" ) \
  | kubectl apply --dry-run=server -f -
```

### 2.3 Despliegue manual del package (sin Helm)

```bash
( cd infra/k8s/platform/packages/vespa-app && zip -r /tmp/vespa-app.zip . )
kubectl -n openfoundry port-forward svc/of-ontology-vespa-configserver-lb 19071:19071 &
curl -fsS --header "Content-Type: application/zip" \
  --data-binary @/tmp/vespa-app.zip \
  http://localhost:19071/application/v2/tenant/default/prepareandactivate \
  | jq .
```

## 3. Rolling upgrade del application package

1. Edita los archivos en `infra/k8s/platform/packages/vespa-app/` **y** copia los cambios
   al mirror `infra/k8s/platform/charts/vespa/files/`
   (regla: el mirror es el que termina en el ConfigMap; ambos deben
   coincidir bit-a-bit).
2. Commit y revisa el `helm template` para confirmar que el SHA del Job
   cambia: `( cd infra/k8s/platform && helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor" ) | grep -E 'name: .*vespa-deploy'`.
3. `helmfile -e prod apply`. El nuevo Job correrá `prepareandactivate` y los
   nodos relevantes harán *reload-on-the-fly*:
   - Schemas con cambios compatibles (añadir campos, nuevos rank-profiles)
     **no requieren reinicio** y se aplican online.
   - Cambios incompatibles requieren un *reindex*: lanza desde el container
     ```bash
     kubectl -n openfoundry exec deploy/<container-pod> -- \
       vespa-reindex --cluster documents --type document
     ```
4. Verifica:
   ```bash
   curl -fsS http://<lb>:19071/application/v2/tenant/default/application/default | jq .
   ```

## 4. Expansión del content cluster

Aumentar `vespa.content.replicas` de 3 → 5 (por ejemplo) requiere
**dos pasos** porque la topología la define el package, no Helm:

1. Edita `infra/k8s/platform/packages/vespa-app/services.xml` y `hosts.xml` para añadir
   los nuevos nodos (`vespa-content-3`, `vespa-content-4`) y replica el
   cambio en el mirror del chart.
2. Actualiza `infra/k8s/platform/values/vespa-prod.yaml` con
   `content.replicas=5` y ejecuta `helmfile -e prod apply`.
   - El StatefulSet escala primero.
   - El Job de deploy publica el package actualizado.
   - El cluster-controller de Vespa redistribuye los buckets a los nuevos
     nodos automáticamente; `redundancy` y `searchable-copies` se
     mantienen.
3. Monitoriza la migración:
   ```bash
   kubectl -n openfoundry exec <content-0> -- vespa-get-cluster-state
   ```
   La columna `Init` debe ir a 0 y `Up` igualar al nuevo total antes de
   considerar la expansión completa.

> **No** reduzcas `redundancy` ni `searchable-copies` durante la
> expansión: hazlo en un upgrade posterior una vez el balanceo termine.

## 5. Recuperación de un nodo

### 5.1 Configserver

Los 3 configservers forman quórum ZooKeeper (PDB `minAvailable=2`):

* Si **un** nodo cae el cluster sigue operativo. Kubernetes recreará el
  pod y el PVC RBD se reattach automáticamente; el nuevo proceso
  resincroniza el estado desde los otros dos.
* Si **dos** nodos caen simultáneamente se pierde quórum: feed y deploy
  se bloquean (queries siguen funcionando). Acción:
  ```bash
  kubectl -n openfoundry get pods -l app.kubernetes.io/component=vespa-configserver
  kubectl -n openfoundry logs <pod> -c configserver --tail=200
  ```
  Si el PVC está corrupto, bórralo y deja que la `volumeClaimTemplate`
  recree el volumen — el nodo restante reseed-eará el estado.

### 5.2 Content

PDB `minAvailable=2` protege contra drains que dejarían `redundancy < 2`.
Para reemplazar un content node:

```bash
# 1. Marca el nodo como "retired" para que migre los buckets
kubectl -n openfoundry exec <content-0> -- \
  vespa-set-node-state --type content --index 2 retired

# 2. Espera a que `Up` baje a 0 buckets activos en el index 2
kubectl -n openfoundry exec <content-0> -- vespa-get-cluster-state

# 3. Borra el pod y su PVC; el StatefulSet lo recrea
kubectl -n openfoundry delete pvc var-of-ontology-vespa-content-2
kubectl -n openfoundry delete pod of-ontology-vespa-content-2

# 4. Vuelve a marcarlo como "up"
kubectl -n openfoundry exec <content-0> -- \
  vespa-set-node-state --type content --index 2 up
```

### 5.3 Container (stateless)

No tiene PVC. `kubectl delete pod <container-N>` basta — el nuevo pod
hace `wait-configservers` y arranca.

## 6. Observabilidad (Prometheus)

Cada pod expone el endpoint del *metrics-proxy* en el puerto **19092**:

```
GET /prometheus/v1/values?consumer=prometheus
```

El consumer `prometheus` está declarado en `services.xml` con los
metric-sets `default` + `vespa`.

### 6.1 Scrape via ServiceMonitor

Si tienes `kube-prometheus-stack` instalado, activa:

```yaml
vespa:
  metrics:
    serviceMonitor:
      enabled: true
      interval: 30s
```

El chart renderiza un único `ServiceMonitor` que cubre los tres
roles (configserver, content, container) usando un `matchExpressions`
sobre el label `app.kubernetes.io/component`.

### 6.2 Scrape manual

```bash
kubectl -n openfoundry port-forward <content-0> 19092:19092
curl -s 'http://localhost:19092/prometheus/v1/values?consumer=prometheus' | head
```

### 6.3 Métricas clave

| Métrica                                          | Para qué                              |
|--------------------------------------------------|---------------------------------------|
| `content_proton_documentdb_documents_total`      | Tamaño del corpus por schema          |
| `content_proton_documentdb_disk_usage`           | Crecimiento de disco RBD              |
| `content_proton_resource_usage_disk`             | Feed-block trigger (>0.85 = pause)    |
| `content_proton_resource_usage_memory`           | Feed-block memory                     |
| `vds_distributor_docsstored`                     | Replicación entre content nodes       |
| `vds_filestor_alldisks_queuesize`                | Backlog de mutaciones                 |
| `searchnode_documentdb_matching_query_latency`   | Latencia de query (p50/p95/p99)       |
| `container_http_requests_per_second`             | RPS al container                       |
| `container_http_status_5xx_rate`                 | Errores en feed/query                 |
| `cluster-controller_resource_usage_nodes_above_limit` | Nodos en feed-block               |

### 6.4 Logs

```bash
kubectl -n openfoundry logs -f <pod> -c vespa
# Vespa también escribe a /opt/vespa/logs/vespa/vespa.log dentro del pod
kubectl -n openfoundry exec <pod> -- tail -F /opt/vespa/logs/vespa/vespa.log
```
