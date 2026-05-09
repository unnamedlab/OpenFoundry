# PoC end-to-end: detección de anomalías sobre Online Retail II

Plan vivo, marcable. Cada bloque es un commit atómico.

## Stack (idéntico dev / stg / prod)

| Pieza | Software | Imagen / versión |
|---|---|---|
| Storage S3 | Rook-Ceph + CephObjectStore (RGW) | `rook/ceph` `v1.19.5`, `quay.io/ceph/ceph:v19.2.1` |
| REST catalog | Lakekeeper | `quay.io/lakekeeper/catalog:v0.12.0` |
| Compute | Spark on k8s via Spark Operator | `apache/spark:3.5.4-scala2.12-java17-python3-ubuntu`, `kubeflow/spark-operator:2.5.0` |
| Table format | Apache Iceberg | `iceberg-spark-runtime-3.5_2.12:1.5.2` + `iceberg-aws-bundle:1.5.2` |
| Pipeline DAG engine | `pipeline-build-service` (Go) | local image `localhost:5001/pipeline-build-service:dev` |
| Pipeline runner orchestrator | `pipeline-runner` (Go) + `pipeline-runner-spark` (Scala JAR baked-in) | local image |

Diferencia entre entornos: replicas/recursos/redundancia. Tecnologías idénticas.

---

## Fases

### F0 · Plataforma — `git log a06eadce` ✅
- [x] Reverso SeaweedFS / Hadoop catalog atajo
- [x] `infra/dev/ceph-single-node.yaml` (Rook v1.19, Ceph v19.2.1, 1 mon/mgr/RGW)
- [x] `services/pipeline-runner-spark/` (Scala 2.12 + sbt-assembly + Dockerfile)
- [x] `services/pipeline-runner/Dockerfile` extendido con stage `scala-jar`
- [x] Spark Operator desplegado en `spark-operator` namespace
- [x] Rook-Ceph operator desplegado (CSI desactivado, no necesario)

### F1 · Ceph HEALTH_OK + S3 credentials
- [x] 3 nodos Lima detectados → OSD en cada uno via `/dev/loop0` (25G)
- [x] `infra/dev/bootstrap-osd-loopback.sh` (idempotente)
- [ ] `kubectl apply -f infra/dev/ceph-single-node.yaml` → Ready
- [ ] CephObjectStore `openfoundry-store` activo (3 OSDs up)
- [ ] OBC `openfoundry-iceberg` Bound → secret `openfoundry-iceberg` con `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY`
- [ ] Smoke S3: `mc ls` lista el bucket

**Salida esperada**: secret consumible por Lakekeeper + Spark.

### F2 · Lakekeeper deploy (mismo helm chart de prod, values-dev)
- [ ] `infra/dev/lakekeeper-dev-values.yaml` (1 replica, OIDC stub o desactivado, sin podDisruptionBudget, sin topologySpreadConstraints)
- [ ] Mirror del secret `openfoundry-iceberg` al ns `lakekeeper`
- [ ] Crear `lakekeeper-encryption-key` y `pg-lakekeeper-app` si no existen
- [ ] `helm upgrade --install lakekeeper infra/helm/infra/lakekeeper -f infra/dev/lakekeeper-dev-values.yaml`
- [ ] Pod `lakekeeper-catalog-*` Running 1/1
- [ ] `curl http://lakekeeper.lakekeeper.svc:8181/health` 200

### F3 · Bootstrap warehouse en Lakekeeper
- [ ] Crear warehouse `openfoundry` apuntando a `s3://openfoundry-iceberg/warehouse/` con las creds del OBC
- [ ] Crear namespace `default` (o `poc`) en el warehouse
- [ ] Smoke: `curl /iceberg/v1/config?warehouse=openfoundry`

### F4 · Build + push imagen `pipeline-runner` (con JAR)
- [ ] `docker buildx build -f services/pipeline-runner/Dockerfile -t localhost:5001/pipeline-runner:dev`
- [ ] `docker push localhost:5001/pipeline-runner:dev`
- [ ] Smoke: `docker run --rm localhost:5001/pipeline-runner:dev --pipeline-id smoke --run-id smoke --output-dataset x --inline-sql "SELECT 1" --smoke`
- [ ] Verificar que `/opt/spark/jars/pipeline-runner-spark.jar` existe en la imagen

### F5 · Wire `executeDistributedComputeTransform` + k8s client
Archivos a tocar:
- `services/pipeline-build-service/internal/domain/engine/runtime.go` (sustituir el stub `transform_runtime_not_wired:distributed`)
- `services/pipeline-build-service/internal/handler/...` (k8s client + render del template)
- `services/pipeline-build-service/cmd/.../main.go` (boot del k8s client si `KUBERNETES_API_URL` o in-cluster)

Sub-tareas:
- [ ] `internal/spark/template.go`: cargar el template YAML, sustituir `${...}` placeholders.
- [ ] `internal/spark/dispatcher.go`: k8s client (clientset Rook-Spark), `Create()` el SparkApplication CR.
- [ ] `internal/spark/watcher.go`: watch del CR hasta `state in {COMPLETED, FAILED}`, devolver error si FAILED.
- [ ] `engine/runtime.go::executeDistributedComputeTransform`: monta dispatcher + watcher, devuelve `TransformResult`.
- [ ] Boot wiring: si `KUBERNETES_API_URL` o ServiceAccount in-cluster, instanciar el dispatcher; si no, devolver `transform_runtime_not_wired:distributed` con mensaje claro.
- [ ] Test: `go test ./services/pipeline-build-service/internal/domain/engine/...` con dispatcher fake.

### F6 · Smoke test E2E del engine
- [ ] Crear pipeline DAG con un nodo `transform_type: "spark"` y `inline_sql: "SELECT 1 AS one"`
- [ ] `curl POST /api/v1/pipelines/<id>/runs` → SparkApplication CR creado
- [ ] `kubectl get sparkapplication -n openfoundry pipeline-run-<id>-<run>` → Running → COMPLETED
- [ ] `curl GET /api/v1/iceberg/v1/namespaces/default/tables/<output>` (vía Lakekeeper) → tabla con 1 fila
- [ ] Logs: `kubectl logs <driver pod>` muestra el prefijo `[pipeline-runner-spark pipeline_id=… run_id=…]`

### F7 · PoC Online Retail II end-to-end

#### F7.1 Ingesta
- [ ] `tools/online-retail/convert.py` — descarga UCI .xlsx, combina hojas 2009-2010 + 2010-2011, normaliza tipos (`InvoiceDate` timestamp ISO, `Quantity` int, `Price` double), escribe `online_retail.csv`.
- [ ] `tools/online-retail/ingest.sh` — sube como dataset `online_retail_raw` via API `/api/v1/datasets` + presigned upload. Idempotente.
- [ ] `previewDataset` confirma schema y `row_count > 0`.

#### F7.2 Pipeline (4 outputs)
- [ ] Nodo `transactions_clean` — filter `Quantity > 0 AND Price > 0`, computa `revenue = Quantity * Price`.
- [ ] Nodo `returns` — filter `Quantity < 0`.
- [ ] Nodo `customer_metrics` — agregado `GROUP BY customer_id` con sum(revenue), count(distinct invoice), count(distinct country).
- [ ] Nodo `transactions_anomalies` — añade `revenue_zscore` (window) e `is_anomaly = ABS(zscore) > 3`. **Tabla completa con flag**, no solo anomalías.
- [ ] Pipeline persistido + ejecutado vía Spark + 4 tablas Iceberg con filas.

#### F7.3 Ontología + relaciones
- [ ] `Customer` — backing dataset `customer_metrics`, PK `customer_id`.
- [ ] `Transaction` — backing `transactions_anomalies`, PK = column derivada `concat(invoice,'_',stockcode)`.
- [ ] `Product` — backing `distinct(stockcode, description)` (SQL en pipeline si hace falta), PK `stockcode`.
- [ ] Property editable enum `review_status` en Transaction (default `pending`).
- [ ] Allow edits ON en Transaction (real backend, no localStorage).
- [ ] LinkType `Customer→Transaction` (FK customer_id), `Transaction→Product` (FK stockcode).
- [ ] Smoke: navegar de Customer a sus Transactions vía link.

#### F7.4 Actions
- [ ] `MarkAsReviewed` (Modify object) → `review_status = 'reviewed'`.
- [ ] `EscalateAnomaly` (Modify object) → `review_status = 'escalated'`.
- [ ] Smoke: ejecutar action vía API contra una fila + re-fetch confirma persistencia.

#### F7.5 Dashboard (3 páginas)
- [ ] Página 1 Overview — KPIs (total tx, total anomalías, %, revenue) + Pie por status + XY anomalías por día.
- [ ] Página 2 Anomalies list — filter list (Order ID, status, date) + Object table sobre Transaction (filtered `is_anomaly=true`) + Property list active object + Button group con `MarkAsReviewed` / `EscalateAnomaly`.
- [ ] Página 3 Customer drilldown — Object table Customer → click → Property list customer + Object table de sus Transactions.
- [ ] Smoke: end-to-end click action → toast + tabla refresca con nuevo `review_status`.

#### F7.6 README + reproducibilidad
- [ ] `docs/poc-online-retail/README.md` con: requisitos, `make poc-bootstrap` (1 comando), screenshots, troubleshooting.
- [ ] `Makefile` target `poc-bootstrap`: ejecuta convert.py + ingest.sh + crea pipeline + ontology + app.
- [ ] Idempotencia verificada: re-run del bootstrap no rompe nada.

---

## Comandos clave (cheatsheet)

```sh
# F1 - bootstrap loopback OSDs
./infra/dev/bootstrap-osd-loopback.sh

# F1 - apply Ceph
kubectl apply -f infra/dev/ceph-single-node.yaml

# F1 - watch Ceph health
watch 'kubectl get cephcluster -n rook-ceph; kubectl get obc -n rook-ceph'

# F4 - build runner image (incluye JAR Scala via stage scala-jar)
docker buildx build --platform linux/arm64 --builder orbstack \
  -f services/pipeline-runner/Dockerfile \
  -t localhost:5001/pipeline-runner:dev --load .
docker push localhost:5001/pipeline-runner:dev

# F4 - smoke local del runner (no Spark, solo orchestrator)
docker run --rm localhost:5001/pipeline-runner:dev \
  --pipeline-id smoke --run-id smoke \
  --output-dataset lakekeeper.default.smoke \
  --inline-sql "SELECT 1 AS one" --smoke

# F6 - lanzar SparkApplication smoke desde el cluster
kubectl apply -f infra/dev/spark-smoke.yaml
kubectl get sparkapplication -n openfoundry -w
```

## Riesgos identificados (en orden de probabilidad)

1. **Ceph OSD prepare falla en loopback** (medio) — si el host losetup no es persistente tras reinicio, los OSDs se pierden. Mitigación: bootstrap script docs.
2. **Lakekeeper requiere OIDC válido** (alto) — el chart upstream puede no permitir desactivarlo. Mitigación: si falla, desplegar un Keycloak-stub mínimo o forquear el chart.
3. **Iceberg AWS bundle vs Hadoop AWS classpath** (medio) — la primera ejecución suele dar `NoClassDefFoundError`. Mitigación: tener ambos JARs en `/opt/spark/jars/` y verificar versiones compatibles (Hadoop 3.3.4 / aws-java-sdk 1.12.x).
4. **k8s client en pipeline-build-service** (medio) — necesita ServiceAccount con permisos para crear SparkApplication CRs en el ns `openfoundry`. Mitigación: helm chart de pipeline-build-service ya tiene sa-account.yaml; verificar Role.
