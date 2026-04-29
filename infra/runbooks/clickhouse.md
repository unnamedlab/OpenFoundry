# ClickHouse Runbook

Fecha: 29 de abril de 2026

OpenFoundry usa **ClickHouse** (Apache-2.0) como motor analítico para los
paneles BI sub-segundo. El despliegue está operado por el
**Altinity `clickhouse-operator`** (Apache-2.0) sobre Kubernetes y usa
**ClickHouse Keeper** como servicio de coordinación (no ZooKeeper).

Manifestos: `infra/k8s/clickhouse/`
Catálogo Trino: `infra/k8s/clickhouse/trino-catalog.yaml`

> **Imágenes**: usamos `clickhouse/clickhouse-server` y
> `clickhouse/clickhouse-keeper` oficiales (Apache-2.0). **No** usamos las
> imágenes Bitnami por su política de distribución restringida. El chart
> elegido es `altinity-clickhouse-operator/altinity-clickhouse-operator`,
> también Apache-2.0.

## 1. Arquitectura desplegada

| Componente                       | Configuración                                                                 |
|----------------------------------|-------------------------------------------------------------------------------|
| `clickhouse-operator`            | Chart Altinity, namespace `clickhouse`                                        |
| `ClickHouseKeeperInstallation`   | `chk/openfoundry`, replicas=3, antiAffinity por hostname, PVC log+data 25Gi   |
| `ClickHouseInstallation`         | `chi/openfoundry`, 1 cluster, **shards=2 × replicas=2** (4 pods), PVC 200Gi   |
| Motor de tablas por defecto      | `ReplicatedMergeTree` (path zk derivado de macros del operador)               |
| Servicio in-cluster (clientes)   | `clickhouse-openfoundry.clickhouse.svc.cluster.local:8123` (HTTP) / `:9000` (TCP) |
| Servicio Keeper                  | `keeper-openfoundry.clickhouse.svc.cluster.local:2181`                        |
| Métricas Prometheus              | ClickHouse `:9363/metrics`, Keeper `:7000/metrics`                            |
| Catálogo Trino                   | ConfigMap `trino-catalog-clickhouse` en namespace `trino`                     |

Topología por shard:

```
shard-0 ── replica-0 (chi-openfoundry-openfoundry-0-0)
        └─ replica-1 (chi-openfoundry-openfoundry-0-1)
shard-1 ── replica-0 (chi-openfoundry-openfoundry-1-0)
        └─ replica-1 (chi-openfoundry-openfoundry-1-1)
```

Quorum Keeper: 3 nodos → tolera 1 caída sin perder coordinación.
Replicación: cada shard tolera la pérdida de 1 réplica sin perder datos.

## 2. Instalación

### 2.1 Operator (Altinity, Helm)

```bash
helm repo add altinity-clickhouse-operator \
  https://docs.altinity.com/clickhouse-operator
helm repo update

# Validar el render del chart antes de aplicarlo.
helm template clickhouse-operator \
  altinity-clickhouse-operator/altinity-clickhouse-operator \
  --namespace clickhouse | kubectl apply --dry-run=client -f -

helm upgrade --install --create-namespace -n clickhouse clickhouse-operator \
  altinity-clickhouse-operator/altinity-clickhouse-operator
```

Espera a que el operator esté `Ready`:

```bash
kubectl -n clickhouse rollout status deploy/clickhouse-operator --timeout=5m
kubectl -n clickhouse get crd | grep altinity.com
# Debe listar:
#   clickhouseinstallations.clickhouse.altinity.com
#   clickhousekeeperinstallations.clickhouse-keeper.altinity.com
#   ...
```

### 2.2 Namespace + CRs

```bash
kubectl apply -f infra/k8s/clickhouse/namespace.yaml

# Validez de las CRs (server-side dry-run usa el OpenAPI del CRD ya
# instalado por el operador y rechaza cualquier campo desconocido):
kubectl apply --dry-run=server -f infra/k8s/clickhouse/keeper.yaml
kubectl apply --dry-run=server -f infra/k8s/clickhouse/clickhouse.yaml
```

Aplicar en orden (Keeper primero):

```bash
kubectl apply -f infra/k8s/clickhouse/keeper.yaml
kubectl -n clickhouse wait --for=jsonpath='{.status.status}'=Completed \
  chk/openfoundry --timeout=15m

kubectl apply -f infra/k8s/clickhouse/clickhouse.yaml
kubectl -n clickhouse wait --for=jsonpath='{.status.status}'=Completed \
  chi/openfoundry --timeout=20m
```

### 2.3 Sembrar credenciales del usuario `openfoundry`

`clickhouse.yaml` referencia el secret `clickhouse-users`, clave
`openfoundry-password-sha256`. Genera el hash y créalo:

```bash
PASSWORD="$(openssl rand -base64 32 | tr -d '\n')"
SHA256="$(printf '%s' "$PASSWORD" | sha256sum | cut -d' ' -f1)"

kubectl -n clickhouse create secret generic clickhouse-users \
  --from-literal=openfoundry-password-sha256="${SHA256}" \
  --dry-run=client -o yaml | kubectl apply -f -

# Reflejar la contraseña en plano para Trino (mismo password literal):
kubectl -n trino create secret generic clickhouse-trino \
  --from-literal=CLICKHOUSE_PASSWORD="${PASSWORD}" \
  --dry-run=client -o yaml | kubectl apply -f -
```

Forzar al operador a recargar `users.xml`:

```bash
kubectl -n clickhouse rollout restart sts -l clickhouse.altinity.com/chi=openfoundry
```

### 2.4 Verificación de salud

```bash
# Estado de los CRs
kubectl -n clickhouse get chk,chi
# Pods esperados: 3 keeper + 4 clickhouse
kubectl -n clickhouse get pods -o wide

# Cluster topology y quorum desde dentro
kubectl -n clickhouse exec -it chi-openfoundry-openfoundry-0-0-0 -c clickhouse -- \
  clickhouse-client -q "SELECT cluster, shard_num, replica_num, host_name \
                        FROM system.clusters WHERE cluster='openfoundry' ORDER BY shard_num, replica_num"

# Keeper four-letter words
kubectl -n clickhouse exec -it chk-openfoundry-keeper-0-0-0 -- \
  bash -lc 'echo mntr | nc 127.0.0.1 2181'
# Buscar zk_server_state=leader / follower y zk_synced_followers >= 2.
```

Un cluster sano reporta `status=Completed` en ambos CRs y los 4 pods de
ClickHouse aparecen en `system.clusters` con `is_local=1` desde su propia
perspectiva.

## 3. Tablas ReplicatedMergeTree

Con la `zookeeper` definida en `clickhouse.yaml`, las macros
`{cluster}`, `{shard}` y `{replica}` están inyectadas por el operador.
Esto permite crear tablas replicadas sin paths zk explícitos:

```sql
-- Una sola sentencia ON CLUSTER crea la tabla en los 4 pods.
CREATE TABLE openfoundry.events ON CLUSTER 'openfoundry'
(
  event_time   DateTime,
  user_id      UInt64,
  event_type   LowCardinality(String),
  payload      String
)
ENGINE = ReplicatedMergeTree
ORDER BY (event_time, user_id)
PARTITION BY toYYYYMM(event_time);

-- Tabla Distributed para fan-out de queries entre los 2 shards.
CREATE TABLE openfoundry.events_dist ON CLUSTER 'openfoundry'
AS openfoundry.events
ENGINE = Distributed('openfoundry', 'openfoundry', 'events', user_id);
```

> El operador rellena `ENGINE = ReplicatedMergeTree` con
> `('/clickhouse/tables/{shard}/openfoundry.events', '{replica}')`
> automáticamente cuando no se especifican parámetros. Se recomienda
> apuntar siempre a la tabla `*_dist` desde dashboards / Trino.

## 4. Catálogo Trino

El ConfigMap `trino-catalog-clickhouse` provee
`clickhouse.properties` para el conector ClickHouse de Trino
(Apache-2.0). Para enchufarlo en el release Helm de Trino:

```yaml
# values.yaml del chart trinodb/trino
additionalCatalogs: {}  # vacíalo o usa el ConfigMap a continuación

coordinator:
  additionalVolumes:
    - name: clickhouse-catalog
      configMap:
        name: trino-catalog-clickhouse
  additionalVolumeMounts:
    - name: clickhouse-catalog
      mountPath: /etc/trino/catalog/clickhouse.properties
      subPath: clickhouse.properties
  envFrom:
    - secretRef:
        name: clickhouse-trino    # exporta CLICKHOUSE_PASSWORD

worker:
  additionalVolumes:
    - name: clickhouse-catalog
      configMap:
        name: trino-catalog-clickhouse
  additionalVolumeMounts:
    - name: clickhouse-catalog
      mountPath: /etc/trino/catalog/clickhouse.properties
      subPath: clickhouse.properties
  envFrom:
    - secretRef:
        name: clickhouse-trino
```

Verificación:

```bash
kubectl -n trino exec -it deploy/trino-coordinator -- \
  trino --execute "SHOW CATALOGS"
# Debe incluir: clickhouse

kubectl -n trino exec -it deploy/trino-coordinator -- \
  trino --execute "SHOW SCHEMAS FROM clickhouse"
kubectl -n trino exec -it deploy/trino-coordinator -- \
  trino --execute "SELECT count(*) FROM clickhouse.openfoundry.events_dist"
```

## 5. Backups

ClickHouse + Keeper se respaldan con
[`clickhouse-backup`](https://github.com/Altinity/clickhouse-backup)
(Apache-2.0) apuntado al endpoint S3 de Rook-Ceph descrito en
`infra/runbooks/ceph.md`.

```bash
# CronJob diario (referencia) -- el secret openfoundry-datasets vive en
# el namespace clickhouse tras ser proyectado desde la OBC.
kubectl -n clickhouse create cronjob clickhouse-backup --schedule="0 3 * * *" \
  --image=altinity/clickhouse-backup:2.6.4 \
  -- /bin/sh -c 'clickhouse-backup create_remote daily-$(date +%F)'
```

Restore:

```bash
kubectl -n clickhouse exec -it chi-openfoundry-openfoundry-0-0-0 -c clickhouse -- \
  clickhouse-backup restore_remote daily-2026-04-28
```

## 6. Disaster Recovery

### 6.1 Pérdida de un pod ClickHouse

- ReplicatedMergeTree replica las partes desde la otra réplica del shard
  en cuanto el pod vuelve.
- Acciones: ninguna manual. Si el PVC se perdió, borrar el pod y dejar
  que el operador re-aprovisione; la replica se sincroniza vía Keeper.

```bash
kubectl -n clickhouse delete pod chi-openfoundry-openfoundry-0-1-0
# Esperar a que vuelva Ready y comprobar:
kubectl -n clickhouse exec -it chi-openfoundry-openfoundry-0-0-0 -c clickhouse -- \
  clickhouse-client -q "SELECT database, table, is_leader, total_replicas, active_replicas \
                        FROM system.replicas FORMAT Vertical"
```

### 6.2 Pérdida de un nodo Keeper

- 3 réplicas → el quorum sobrevive a una caída.
- Si se pierden 2: ClickHouse pasa a *read-only* hasta que vuelva el
  quorum. Restaurar el segundo Keeper desde su PVC. **Nunca** borrar
  manualmente los datos de los Keepers supervivientes.

### 6.3 Pérdida de un shard completo (ambas réplicas)

1. Restaurar las PVCs desde snapshots si están disponibles.
2. Si no, recrear los pods vacíos y restaurar desde `clickhouse-backup`:

   ```bash
   kubectl -n clickhouse exec -it chi-openfoundry-openfoundry-0-0-0 -c clickhouse -- \
     clickhouse-backup restore_remote --schema --data daily-<fecha>
   ```

3. La otra réplica del shard re-sincroniza desde Keeper automáticamente
   en cuanto las dos réplicas vuelven al cluster.

### 6.4 Pérdida total del cluster (catastrófico)

```bash
# 1. Reinstalar operador y CRDs
helm upgrade --install -n clickhouse clickhouse-operator \
  altinity-clickhouse-operator/altinity-clickhouse-operator

# 2. Re-aplicar CRs
kubectl apply -f infra/k8s/clickhouse/keeper.yaml
kubectl -n clickhouse wait --for=jsonpath='{.status.status}'=Completed \
  chk/openfoundry --timeout=15m
kubectl apply -f infra/k8s/clickhouse/clickhouse.yaml
kubectl -n clickhouse wait --for=jsonpath='{.status.status}'=Completed \
  chi/openfoundry --timeout=20m

# 3. Re-sembrar usuarios (§2.3) y restaurar datos desde S3
kubectl -n clickhouse exec -it chi-openfoundry-openfoundry-0-0-0 -c clickhouse -- \
  clickhouse-backup restore_remote daily-<fecha>
```

El endpoint in-cluster no cambia, así que el catálogo Trino y los
dashboards no necesitan reconfiguración.

## 7. Limpieza

Para destruir el cluster y los datos (¡irreversible!):

```bash
kubectl -n clickhouse delete chi openfoundry
kubectl -n clickhouse delete chk openfoundry
kubectl -n clickhouse delete pvc -l clickhouse.altinity.com/chi=openfoundry
kubectl -n clickhouse delete pvc -l clickhouse-keeper.altinity.com/chk=openfoundry
helm -n clickhouse uninstall clickhouse-operator
kubectl delete ns clickhouse
```

## 8. Criterios de aceptación

- `helm template altinity-clickhouse-operator/altinity-clickhouse-operator`
  renderiza sin errores.
- `kubectl apply --dry-run=server -f infra/k8s/clickhouse/keeper.yaml`
  acepta la CR (CRD `clickhousekeeperinstallations.clickhouse-keeper.altinity.com`).
- `kubectl apply --dry-run=server -f infra/k8s/clickhouse/clickhouse.yaml`
  acepta la CR (CRD `clickhouseinstallations.clickhouse.altinity.com`).
- Tras aplicar, `kubectl -n clickhouse get chi/openfoundry` reporta
  `shards=2 replicas=2 hosts=4 status=Completed`.
- `SELECT count() FROM system.clusters WHERE cluster='openfoundry'` = 4.
- Trino lista el catálogo `clickhouse` en `SHOW CATALOGS` y puede
  ejecutar `SELECT 1 FROM clickhouse.system.one`.
