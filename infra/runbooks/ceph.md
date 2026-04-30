# Ceph (Rook) Runbook

Fecha: 29 de abril de 2026

OpenFoundry usa **Ceph RGW** operado por **Rook** como backend S3-compatible
en producción. La capa `libs/storage-abstraction` no cambia: solo se le
apunta a un endpoint distinto. En desarrollo se utiliza **RustFS**, no
MinIO.

Manifestos: `infra/k8s/rook/`
Módulo Terraform: `infra/terraform/modules/ceph/`
Helm values prod: `infra/k8s/helm/open-foundry/values-prod.yaml`

## 1. Arquitectura desplegada

| Componente            | Configuración                                             |
|-----------------------|-----------------------------------------------------------|
| `CephCluster`         | mon=5, mgr=2, `dataDirHostPath=/var/lib/rook`, discovery on |
| `CephObjectStore`     | metadata pool replicated=3, data pool EC 8+3, RGW=3        |
| `StorageClass`        | `ceph-bucket` (provisioner `rook-ceph.ceph.rook.io/bucket`)|
| Endpoint S3 in-cluster| `http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80`        |
| Buckets               | `openfoundry-datasets`, `openfoundry-models`, `openfoundry-iceberg` |

## 2. Instalación

### 2.1 Vía Terraform (recomendado)

```hcl
module "ceph" {
  source               = "../../modules/ceph"
  chart_version        = "v1.15.5"
  namespace            = "rook-ceph"
  app_namespace        = "openfoundry"
  create_app_namespace = true
}
```

```bash
cd infra/terraform/<env>
terraform init
terraform validate
terraform apply -target=module.ceph
```

El módulo:

1. Crea el namespace `rook-ceph`.
2. Instala el chart oficial `rook-ceph` (repo
   `https://charts.rook.io/release`, ver chart_version).
3. Aplica `cluster.yaml`, espera `status.phase=Ready`.
4. Aplica `objectstore.yaml` (CephObjectStore + StorageClass `ceph-bucket`).
5. Aplica los `ObjectBucketClaim` para los tres buckets de OpenFoundry.

### 2.2 Vía kubectl (manual / DR)

```bash
helm repo add rook-release https://charts.rook.io/release
helm repo update
helm upgrade --install --create-namespace -n rook-ceph rook-ceph \
  rook-release/rook-ceph --version v1.15.5 \
  --set crds.enabled=true --set enableDiscoveryDaemon=true

kubectl apply -f infra/k8s/rook/cluster.yaml
kubectl -n rook-ceph wait --for=jsonpath='{.status.phase}'=Ready \
  cephcluster/openfoundry --timeout=30m

kubectl apply -f infra/k8s/rook/objectstore.yaml
kubectl -n rook-ceph wait --for=jsonpath='{.status.phase}'=Ready \
  cephobjectstore/openfoundry --timeout=15m

kubectl apply -f infra/k8s/rook/bucket.yaml
```

### 2.3 Verificación de salud

```bash
# Pod toolbox para hablar con el cluster Ceph
kubectl -n rook-ceph exec -it deploy/rook-ceph-tools -- ceph status
kubectl -n rook-ceph exec -it deploy/rook-ceph-tools -- ceph osd tree
kubectl -n rook-ceph exec -it deploy/rook-ceph-tools -- ceph df

# RGW endpoint
kubectl -n rook-ceph get svc rook-ceph-rgw-openfoundry
```

Un cluster sano reporta `HEALTH_OK` y `n osds: n up, n in`.

## 3. Comando E2E para crear OBC y obtener credenciales

Cada `ObjectBucketClaim` (OBC) declara un bucket en el `CephObjectStore`.
Cuando el provisioner lo enlaza, crea en el mismo namespace de la OBC:

- `Secret` `<bucketName>` con claves `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`
- `ConfigMap` `<bucketName>` con `BUCKET_HOST`, `BUCKET_PORT`, `BUCKET_NAME`, `BUCKET_REGION`

Workflow E2E (ejemplo con `openfoundry-datasets`):

```bash
# 1. Crear el OBC (idempotente)
kubectl apply -f infra/k8s/rook/bucket.yaml

# 2. Esperar a que se aprovisione el bucket
kubectl -n openfoundry wait --for=jsonpath='{.status.phase}'=Bound \
  obc/openfoundry-datasets --timeout=5m

# 3. Recuperar credenciales y metadatos
ACCESS_KEY=$(kubectl -n openfoundry get secret openfoundry-datasets \
  -o jsonpath='{.data.AWS_ACCESS_KEY_ID}' | base64 -d)
SECRET_KEY=$(kubectl -n openfoundry get secret openfoundry-datasets \
  -o jsonpath='{.data.AWS_SECRET_ACCESS_KEY}' | base64 -d)
BUCKET_HOST=$(kubectl -n openfoundry get cm openfoundry-datasets \
  -o jsonpath='{.data.BUCKET_HOST}')
BUCKET_PORT=$(kubectl -n openfoundry get cm openfoundry-datasets \
  -o jsonpath='{.data.BUCKET_PORT}')
BUCKET_NAME=$(kubectl -n openfoundry get cm openfoundry-datasets \
  -o jsonpath='{.data.BUCKET_NAME}')

echo "endpoint=http://${BUCKET_HOST}:${BUCKET_PORT}"
echo "bucket=${BUCKET_NAME}"
echo "access_key=${ACCESS_KEY}"
echo "secret_key=${SECRET_KEY}"

# 4. Smoke test S3 (usa awscli o mc apuntando al endpoint in-cluster)
kubectl -n openfoundry run s3-smoke --rm -it --restart=Never \
  --image=amazon/aws-cli --env="AWS_ACCESS_KEY_ID=${ACCESS_KEY}" \
  --env="AWS_SECRET_ACCESS_KEY=${SECRET_KEY}" -- \
  --endpoint-url "http://${BUCKET_HOST}:${BUCKET_PORT}" \
  s3 ls "s3://${BUCKET_NAME}"
```

### Proyectar credenciales en `open-foundry-prod-env`

Los servicios consumen `OBJECT_STORE_ACCESS_KEY` /
`OBJECT_STORE_SECRET_KEY` del Secret referenciado por
`global.existingSecret` (en prod: `open-foundry-prod-env`). Para
materializarlas desde el OBC primario (`openfoundry-datasets`):

```bash
kubectl -n openfoundry create secret generic open-foundry-prod-env \
  --from-literal=OBJECT_STORE_ENDPOINT=http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80 \
  --from-literal=OBJECT_STORE_ACCESS_KEY="${ACCESS_KEY}" \
  --from-literal=OBJECT_STORE_SECRET_KEY="${SECRET_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -
```

> Las tres OBCs comparten el mismo CephObjectStore, así que las claves de
> cualquiera de ellas son válidas para acceder a los tres buckets *si* el
> usuario tiene políticas para los otros (por defecto, cada OBC genera un
> usuario con permisos solo sobre su bucket). Para acceso multi-bucket
> centralizado, crea un usuario RGW dedicado con `radosgw-admin user create`
> y enlázalo al secret.

## 4. Expansión de OSDs

### 4.1 Agregar nuevos discos a nodos existentes

1. Insertar/atachar el disco en el nodo (debe aparecer como dispositivo
   crudo, sin filesystem).
2. El daemon de descubrimiento (`enableDiscoveryDaemon=true`) detecta el
   nuevo dispositivo en ≤ 60 s.
3. Si el dispositivo casa con `deviceFilter` de `cluster.yaml` (por
   defecto `^(sd[b-z]|nvme[0-9]+n[0-9]+)$`), el operador crea un nuevo
   OSD automáticamente.
4. Verificar:

   ```bash
   kubectl -n rook-ceph exec deploy/rook-ceph-tools -- ceph osd tree
   kubectl -n rook-ceph exec deploy/rook-ceph-tools -- ceph status
   ```

5. El balanceo PG se realiza por el módulo `pg_autoscaler` (ya activo).

### 4.2 Agregar nuevos nodos al cluster

1. Etiquetar el nodo:

   ```bash
   kubectl label node <node> role=storage
   kubectl taint node <node> storage-node=true:NoSchedule  # opcional
   ```

   (Coincide con el `nodeAffinity` y `tolerations` de `cluster.yaml`.)

2. El operador planifica mons/mgrs/osds nuevos cuando aplique.
3. Para limitar qué dispositivos consumir, edita `spec.storage` en
   `cluster.yaml` y reaplica.

### 4.3 Sustituir un OSD fallido

```bash
# 1. Identificar el OSD fallido
kubectl -n rook-ceph exec deploy/rook-ceph-tools -- ceph osd tree | grep down

# 2. Marcar el OSD como out y purgarlo
OSD_ID=<id>
kubectl -n rook-ceph exec deploy/rook-ceph-tools -- ceph osd out osd.${OSD_ID}
# Esperar a que el cluster recupere las PGs
kubectl -n rook-ceph exec deploy/rook-ceph-tools -- ceph status
kubectl -n rook-ceph exec deploy/rook-ceph-tools -- \
  ceph osd purge ${OSD_ID} --yes-i-really-mean-it

# 3. Quitar el deployment OSD residual
kubectl -n rook-ceph delete deploy rook-ceph-osd-${OSD_ID}

# 4. Sustituir físicamente el disco; el operador re-aprovisiona el OSD.
```

## 5. Disaster Recovery

### 5.1 Pérdida de un único nodo (mon/osd)

- mon=5 tolera la pérdida de hasta 2 mons sin perder quorum.
- EC 8+3 tolera la pérdida de hasta 3 chunks por PG.
- Acciones: reemplazar el nodo, etiquetarlo como `role=storage`,
  esperar al operador. Sin RPO ni intervención sobre datos.

### 5.2 Pérdida de quorum de mons

```bash
# 1. Listar mons supervivientes
kubectl -n rook-ceph get pods -l app=rook-ceph-mon

# 2. Forzar reconstrucción del quorum desde el mon superviviente
#    (procedimiento `rook-ceph mons restore-quorum` en toolbox)
kubectl -n rook-ceph rollout restart deploy/rook-ceph-operator
# Si persiste: seguir https://rook.io/docs/rook/latest/Troubleshooting/disaster-recovery/
```

### 5.3 Pérdida total del object store (RGW)

Los pools de RGW son persistentes; los pods RGW son stateless.

```bash
kubectl -n rook-ceph delete pod -l app=rook-ceph-rgw
# El operador re-crea las 3 instancias (gateway.instances=3).
```

### 5.4 Pérdida total del cluster Ceph (catastrófico)

1. Restaurar nodos de almacenamiento desde imágenes base con
   `/var/lib/rook` intacto si está disponible. Reaplicar manifestos:

   ```bash
   terraform apply -target=module.ceph
   ```

2. Si `/var/lib/rook` también se perdió pero tienes un backup S3 externo
   de los buckets (recomendado para `openfoundry-datasets`):

   ```bash
   # Reinstalar el cluster vacío
   terraform apply -target=module.ceph

   # Esperar a HEALTH_OK
   kubectl -n rook-ceph exec deploy/rook-ceph-tools -- ceph status

   # Re-hidratar buckets desde el backup externo (ej. snapshots offsite)
   aws s3 sync s3://openfoundry-dr-mirror/datasets \
     s3://openfoundry-datasets \
     --endpoint-url http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80
   ```

3. Reanudar servicios — el endpoint S3 no cambia, las nuevas credenciales
   de OBC se proyectan a `open-foundry-prod-env` con el procedimiento de
   §3.

### 5.5 Backups recomendados

- `radosgw-admin metadata list bucket` → exportar lista de buckets cada
  hora.
- Snapshot diario S3-to-S3 a un bucket externo (otra región, otro
  proveedor) para los datos críticos (`openfoundry-datasets`).
- Backup periódico de los Secrets `openfoundry-*` en `openfoundry`
  (contienen las credenciales de los OBCs) a una bóveda offline.

## 6. Limpieza

Para destruir el cluster y los datos (¡irreversible!):

```bash
kubectl -n openfoundry delete obc --all
kubectl -n rook-ceph delete cephobjectstore openfoundry
kubectl -n rook-ceph patch cephcluster openfoundry --type=merge \
  -p '{"spec":{"cleanupPolicy":{"confirmation":"yes-really-destroy-data"}}}'
kubectl -n rook-ceph delete cephcluster openfoundry
helm -n rook-ceph uninstall rook-ceph
kubectl delete ns rook-ceph
# Limpiar /var/lib/rook en cada nodo de storage.
```
