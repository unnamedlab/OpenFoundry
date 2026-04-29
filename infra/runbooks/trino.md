# Trino Runbook

Fecha: 29 de abril de 2026

OpenFoundry usa **Trino** (Apache-2.0) como motor de consultas federadas
sobre las distintas fuentes analíticas: Iceberg (catálogo Polaris) sobre
Ceph RGW, PostgreSQL operado por CloudNativePG, Kafka (solo lectura para
troubleshooting) y, cuando exista, ClickHouse.

Manifestos: `infra/k8s/trino/`
Chart upstream: `trino/trino` desde `https://trinodb.github.io/charts/`
(alt. OCI `oci://ghcr.io/trinodb/charts/trino`).

## 1. Arquitectura desplegada

| Componente             | Configuración                                                              |
|------------------------|----------------------------------------------------------------------------|
| Coordinator            | `replicas: 2`, HA experimental con leader election, anti-afinidad por host |
| Worker                 | `replicas: 6` (alias `server.workers`), pods stateless                      |
| PodDisruptionBudget    | `trino-coordinator` con `minAvailable: 1`                                   |
| Catálogos              | `iceberg`, `postgresql`, `kafka` (ConfigMaps independientes)                |
| Auth interna           | mTLS automática vía Linkerd (`linkerd.io/inject: enabled`)                  |
| Auth externa           | JWT contra el IdP de plataforma (opcional, se activa por overlay)           |
| Endpoint in-cluster    | `http://trino.trino.svc:8080`                                               |
| Almacenamiento Iceberg | Bucket `openfoundry-iceberg` en Ceph RGW (ver `infra/runbooks/ceph.md`)     |

## 2. Instalación

### 2.1 Prerrequisitos

- Cluster Kubernetes con Linkerd instalado y la política mTLS activa.
- Polaris desplegado en el namespace `polaris` con un warehouse `openfoundry`.
- CloudNativePG con la base `openfoundry` y un rol read-mostly para Trino.
- Bucket `openfoundry-iceberg` provisto por Rook (ver `infra/k8s/rook/`).
- `kubectl` y `helm` ≥ 3.14 con acceso al cluster.

### 2.2 Secretos requeridos

Los catálogos referencian variables de entorno que se inyectan al coordinator
y a los workers desde estos Secrets:

| Secret                          | Claves                                  | Uso                                  |
|---------------------------------|-----------------------------------------|--------------------------------------|
| `trino-internal-shared-secret`  | `shared-secret`                         | Handshake interno entre coordinators |
| `trino-s3-iceberg`              | `S3_ICEBERG_ACCESS_KEY`, `S3_ICEBERG_SECRET_KEY` | Acceso S3 para Iceberg      |
| `trino-polaris-oauth`           | `POLARIS_OAUTH2_CREDENTIAL`             | Cliente OAuth2 contra Polaris        |
| `trino-postgres-credentials`    | `PG_TRINO_USER`, `PG_TRINO_PASSWORD`    | Conexión JDBC a CNPG                 |

```bash
kubectl create namespace trino
kubectl -n trino create secret generic trino-internal-shared-secret \
  --from-literal=shared-secret="$(openssl rand -hex 32)"
# Las credenciales S3 se cosechan del ObjectBucketClaim de Iceberg
# (ver infra/runbooks/ceph.md §4 "Harvest OBC credentials").
kubectl -n trino create secret generic trino-s3-iceberg \
  --from-literal=S3_ICEBERG_ACCESS_KEY="$AWS_ACCESS_KEY_ID" \
  --from-literal=S3_ICEBERG_SECRET_KEY="$AWS_SECRET_ACCESS_KEY"
kubectl -n trino create secret generic trino-polaris-oauth \
  --from-literal=POLARIS_OAUTH2_CREDENTIAL="trino:$(vault read -field=password secret/polaris/trino)"
kubectl -n trino create secret generic trino-postgres-credentials \
  --from-literal=PG_TRINO_USER=trino_ro \
  --from-literal=PG_TRINO_PASSWORD="$(vault read -field=password secret/cnpg/trino_ro)"
```

Después se inyectan vía `coordinator.envFrom` / `worker.envFrom` mediante un
overlay del values.yaml o `--set-file`. Mantener los Secrets fuera del repo.

### 2.3 Aplicar manifiestos y desplegar el chart

```bash
helm repo add trino https://trinodb.github.io/charts/
helm repo update

kubectl -n trino apply -f infra/k8s/trino/iceberg-catalog-configmap.yaml
kubectl -n trino apply -f infra/k8s/trino/postgresql-catalog-configmap.yaml
kubectl -n trino apply -f infra/k8s/trino/kafka-catalog-configmap.yaml
kubectl -n trino apply -f infra/k8s/trino/coordinator-pdb.yaml

helm upgrade --install trino trino/trino \
  -n trino \
  -f infra/k8s/trino/values.yaml

kubectl -n trino rollout status deploy/trino-coordinator --timeout=5m
kubectl -n trino rollout status deploy/trino-worker      --timeout=10m
```

### 2.4 Smoke test

```bash
kubectl -n trino exec deploy/trino-coordinator -c trino -- \
  trino --execute "SHOW CATALOGS"
# Esperado: iceberg, postgresql, kafka, system, jmx
kubectl -n trino exec deploy/trino-coordinator -c trino -- \
  trino --execute "SELECT count(*) FROM iceberg.openfoundry.\"<tabla>\""
```

## 3. Catálogos

Cada catálogo se define como un ConfigMap independiente y se monta como
archivo individual en `/etc/trino/catalog/`. Esto permite editarlos y
recargarlos uno a uno sin tocar el chart.

| Catálogo     | ConfigMap                       | Backend                                     |
|--------------|---------------------------------|---------------------------------------------|
| `iceberg`    | `trino-catalog-iceberg`         | Polaris REST + Ceph RGW S3                  |
| `postgresql` | `trino-catalog-postgresql`      | CNPG `openfoundry-pg-rw.cnpg.svc:5432`      |
| `kafka`      | `trino-catalog-kafka`           | `kafka-bootstrap.kafka.svc:9092` (read-only)|

### 3.1 Recarga de un catálogo

Trino no recarga catálogos automáticamente. Tras editar un ConfigMap:

```bash
kubectl -n trino apply -f infra/k8s/trino/iceberg-catalog-configmap.yaml
kubectl -n trino rollout restart deploy/trino-coordinator deploy/trino-worker
```

### 3.2 Añadir un catálogo nuevo (ej. ClickHouse)

1. Crear `clickhouse-catalog-configmap.yaml` con `connector.name=clickhouse`.
2. Añadir el volumen y el `subPath` correspondiente en `coordinator.additionalVolumes` y `worker.additionalVolumes` dentro de `values.yaml`.
3. Aplicar el ConfigMap y hacer `helm upgrade` + rollout.

## 4. Autenticación

### 4.1 Interna (pod ↔ pod)

El handshake interno entre coordinator y workers (y entre coordinators en HA)
está protegido por **Linkerd mTLS**. Las anotaciones `linkerd.io/inject:
enabled` en los pods son suficientes; **no** hay que generar keystores Java
ni configurar TLS dentro de Trino. Validar con:

```bash
linkerd -n trino check --proxy
linkerd -n trino edges deploy
# Todas las conexiones deben mostrar SECURED.
```

Adicionalmente, `experimental.coordinator.high-availability.enabled=true`
exige un secret compartido entre coordinators; lo provee el Secret
`trino-internal-shared-secret`.

### 4.2 Externa (cliente ↔ coordinator)

Los clientes (notebooks, BI, CLI) se autentican con **JWT** emitido por el
IdP de plataforma. Para activarlo:

1. En `values.yaml` poner `server.config.authenticationType: "JWT"`.
2. Añadir a `coordinator.additionalConfigProperties`:

   ```
   - "http-server.authentication.type=JWT"
   - "http-server.authentication.jwt.key-file=https://idp.openfoundry.svc/.well-known/jwks.json"
   - "http-server.authentication.jwt.required-issuer=https://idp.openfoundry"
   - "http-server.authentication.jwt.required-audience=trino"
   ```

3. `helm upgrade` y rollout del coordinator.
4. Probar:

   ```bash
   trino --server https://trino.openfoundry.dev \
     --access-token "$(idp-cli token --aud trino)" \
     --execute "SHOW CATALOGS"
   ```

El gateway de plataforma termina TLS hacia el cliente; el coordinator
recibe la cabecera `Authorization: Bearer <jwt>` ya intra-mesh.

## 5. Escalado

### 5.1 Workers

Stateless. Escalar en caliente:

```bash
helm upgrade trino trino/trino -n trino \
  -f infra/k8s/trino/values.yaml \
  --set server.workers=12 --set worker.replicas=12
```

Trino redistribuye splits en cuanto el nuevo worker se registra. Al reducir,
hacer drain con graceful shutdown:

```bash
kubectl -n trino exec deploy/trino-worker -c trino -- \
  curl -X PUT -d '"SHUTTING_DOWN"' -H 'Content-Type: application/json' \
  http://localhost:8080/v1/info/state
# El pod termina cuando todas las queries en curso finalizan.
```

### 5.2 Coordinators

`coordinator.replicas: 2` es el sweet spot: más coordinators no aumentan
throughput porque solo uno es leader. Si la latencia de planificación es
el cuello de botella, escalar **vertical**mente subiendo CPU/heap antes que
añadir réplicas.

## 6. Failover de coordinator

La PDB `trino-coordinator` (`minAvailable: 1`) garantiza que un drain de
nodo nunca elimine ambos coordinators a la vez.

Forzar failover (mantenimiento del leader actual):

```bash
LEADER=$(kubectl -n trino get pods -l app.kubernetes.io/component=coordinator \
  -o jsonpath='{range .items[?(@.metadata.annotations.openfoundry\.dev/role=="leader")]}{.metadata.name}{end}')
kubectl -n trino delete pod "$LEADER"
# El standby toma el liderazgo en <30 s; el cliente reintentará la query.
```

Verificar:

```bash
kubectl -n trino logs deploy/trino-coordinator | grep -i "elected leader"
```

## 7. Backups y estado

Trino es stateless: **no requiere backup propio**. La durabilidad vive en
los backends:

- Iceberg → tablas + metadata en Ceph RGW (snapshots Iceberg + replicación de bucket).
- PostgreSQL → backups CNPG (Barman) en bucket dedicado.
- Kafka → políticas de retención propias del cluster Kafka.

Los **query logs** y el `event-listener` se exportan a Loki vía stdout; no
hay que persistirlos desde el pod.

## 8. Troubleshooting

| Síntoma                                              | Diagnóstico                                                                                | Acción                                                          |
|------------------------------------------------------|--------------------------------------------------------------------------------------------|-----------------------------------------------------------------|
| `Catalog 'iceberg' does not exist`                   | ConfigMap no montado o `subPath` mal escrito                                               | `kubectl -n trino exec deploy/trino-coordinator -- ls /etc/trino/catalog` |
| `Failed to authenticate with Polaris`                | Secret `trino-polaris-oauth` desincronizado                                                | Rotar credencial en Vault y recrear el Secret + rollout         |
| `S3 SignatureDoesNotMatch`                           | Credenciales S3 expiradas o reloj desincronizado                                           | Recosechar OBC creds (runbook ceph §4); revisar NTP del nodo    |
| `Coordinator HA: not elected, going passive`         | Comportamiento normal del standby                                                          | Ninguna; verificar tráfico vía `linkerd edges`                  |
| Queries colgadas tras `helm upgrade`                 | Worker no completó graceful shutdown                                                       | `kubectl rollout undo` o esperar al `terminationGracePeriodSeconds` |
| `Query exceeded per-node memory limit`               | `query.maxMemoryPerNode` insuficiente para el plan                                          | Subir `worker.config.query.maxMemoryPerNode` y rollout          |
| Kafka catalog devuelve datos parciales               | `kafka.messages-per-split` o retención del topic                                            | Recordar que el catálogo es **read-only para troubleshooting**, no pipeline |

## 9. Upgrade

1. Revisar release notes de Trino (saltos `>=10` versions requieren
   pruebas en staging).
2. Actualizar `image.tag` en `values.yaml`.
3. Aplicar primero en staging vía `values-staging.yaml` overlay.
4. `helm upgrade` con `--atomic --timeout 15m`.
5. Validar con el smoke test (§2.4) y con la suite de queries de
   `tools/trino-smoke/` si existe.
6. Si falla, `helm rollback trino` (la PDB protege la disponibilidad
   durante el rollback).

## 10. Referencias

- Documentación Trino: https://trino.io/docs/current/
- Chart upstream: https://github.com/trinodb/charts
- Polaris (Iceberg REST catalog): https://polaris.apache.org
- Linkerd mTLS: https://linkerd.io/2/features/automatic-mtls/
- Runbook Ceph (backend S3): `infra/runbooks/ceph.md`
