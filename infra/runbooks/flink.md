# Apache Flink Kubernetes Operator Runbook

Fecha: 29 de abril de 2026

OpenFoundry usa el [**Apache Flink Kubernetes Operator**][fko] (Apache-2.0,
proyecto top-level de la ASF) para correr jobs Flink en streaming sobre el
clúster. El operador es responsable de reconciliar `FlinkDeployment` /
`FlinkSessionJob` en `mode: native`, gestionar HA, savepoints periódicos y
upgrades sin pérdida de estado.

Manifestos: `infra/k8s/platform/manifests/flink/`
Helm values: `infra/k8s/platform/manifests/flink/values.yaml`
Ejemplo: `infra/k8s/platform/manifests/flink/flinkdeployment-cdc-iceberg.yaml`
Backend de estado: bucket `openfoundry-iceberg` en Ceph RGW (ver
[`infra/runbooks/ceph.md`](./ceph.md))

[fko]: https://nightlies.apache.org/flink/flink-kubernetes-operator-docs-release-1.10/

## 0. Versiones soportadas

| Componente                   | Versión           | Notas                                    |
|------------------------------|-------------------|------------------------------------------|
| flink-kubernetes-operator    | `1.10.0`          | Apache-2.0, release 2024-11-19           |
| Flink runtime                | `v1_19` (default) | el operador 1.10 soporta `v1_16..v1_20`  |
| Kubernetes                   | `>= 1.27`         | requiere `cert-manager` para webhook      |
| cert-manager                 | `>= v1.13`        | instalación previa, *cluster-wide*       |

> **Verificar la versión vigente** antes de actualizar:
>
> ```bash
> helm repo add flink-operator-repo \
>   https://downloads.apache.org/flink/flink-kubernetes-operator-1.10.0/
> helm repo update
> helm search repo flink-operator-repo --versions | head
> ```

## 1. Arquitectura desplegada

| Componente            | Configuración                                                      |
|-----------------------|--------------------------------------------------------------------|
| Operator              | 2 réplicas, leader-election, namespaces vigilados: `flink`, `openfoundry` |
| Webhook               | 2 réplicas, certificados emitidos por cert-manager                 |
| `FlinkDeployment`     | `mode: native`, `flinkVersion: v1_19`, JM=2, TM bajo demanda        |
| Estado / HA           | `high-availability.type: kubernetes` + `s3://openfoundry-iceberg/flink/ha/<job>` |
| Checkpoints           | RocksDB incremental → `s3://openfoundry-iceberg/flink/checkpoints/<job>` |
| Savepoints            | `s3://openfoundry-iceberg/flink/savepoints/<job>`, periódicos cada 6 h |
| Métricas              | Prometheus reporter en `:9249` (scrape vía `PodMonitor`)            |

## 2. Pre-requisitos

1. **cert-manager** instalado en el clúster (el chart del operador despliega
   un `Certificate` para el webhook):

   ```bash
   helm repo add jetstack https://charts.jetstack.io
   helm upgrade --install cert-manager jetstack/cert-manager \
     -n cert-manager --create-namespace \
     --version v1.16.1 --set crds.enabled=true
   ```

2. **Ceph RGW** disponible y bucket `openfoundry-iceberg` aprovisionado por
   OBC (ver `infra/runbooks/ceph.md` §3).

3. Namespace `flink`:

   ```bash
   kubectl apply -f infra/k8s/platform/manifests/flink/namespace.yaml
   ```

## 3. Credenciales S3 (Ceph) para los pods Flink

Los pods Flink consumen `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` desde el
Secret `flink-s3-credentials` (referenciado por `envFrom` en el ejemplo). Se
materializa desde el OBC `openfoundry-iceberg`:

```bash
ACCESS_KEY=$(kubectl -n openfoundry get secret openfoundry-iceberg \
  -o jsonpath='{.data.AWS_ACCESS_KEY_ID}' | base64 -d)
SECRET_KEY=$(kubectl -n openfoundry get secret openfoundry-iceberg \
  -o jsonpath='{.data.AWS_SECRET_ACCESS_KEY}' | base64 -d)

kubectl -n flink create secret generic flink-s3-credentials \
  --from-literal=AWS_ACCESS_KEY_ID="${ACCESS_KEY}" \
  --from-literal=AWS_SECRET_ACCESS_KEY="${SECRET_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -
```

> Repite el procedimiento en cada namespace donde despliegues
> `FlinkDeployment` (también en `openfoundry`).

## 4. Instalación del operador

```bash
# 1. Repo oficial Apache (Apache-2.0)
helm repo add flink-operator-repo \
  https://downloads.apache.org/flink/flink-kubernetes-operator-1.10.0/
helm repo update

# 2. Render local para validación
helm template flink-kubernetes-operator \
  flink-operator-repo/flink-kubernetes-operator \
  --version 1.10.0 \
  -n flink \
  -f infra/k8s/platform/manifests/flink/values.yaml > /tmp/flink-operator.rendered.yaml

# 3. Instalación / upgrade
helm upgrade --install --create-namespace -n flink flink-kubernetes-operator \
  flink-operator-repo/flink-kubernetes-operator \
  --version 1.10.0 \
  -f infra/k8s/platform/manifests/flink/values.yaml

# 4. Esperar a que el operador y el webhook estén Ready
kubectl -n flink rollout status deploy/flink-kubernetes-operator --timeout=5m
kubectl -n flink get pods -l app.kubernetes.io/name=flink-kubernetes-operator
```

## 5. Desplegar el ejemplo CDC → Iceberg

```bash
# Validación previa (sin tocar el clúster)
kubectl apply --dry-run=client -f infra/k8s/platform/manifests/flink/flinkdeployment-cdc-iceberg.yaml
kubectl apply --dry-run=server  -f infra/k8s/platform/manifests/flink/flinkdeployment-cdc-iceberg.yaml

# Aplicar
kubectl apply -f infra/k8s/platform/manifests/flink/flinkdeployment-cdc-iceberg.yaml

# Observar el ciclo de vida
kubectl -n flink get flinkdeployment cdc-iceberg -w
kubectl -n flink describe flinkdeployment cdc-iceberg
kubectl -n flink logs deploy/cdc-iceberg -c flink-main-container --tail=200
```

Salida esperada en `status`:

```text
jobManagerDeploymentStatus: READY
jobStatus.state: RUNNING
lifecycleState: STABLE
```

## 6. Operaciones cotidianas

### 6.1 Forzar un savepoint

```bash
kubectl -n flink patch flinkdeployment cdc-iceberg --type=merge \
  -p '{"spec":{"job":{"savepointTriggerNonce":'"$(date +%s)"'}}}'

# Verificar savepoints retenidos
kubectl -n flink get flinkdeployment cdc-iceberg \
  -o jsonpath='{.status.jobStatus.savepointInfo}' | jq .
```

### 6.2 Upgrade (cambio de imagen / config) sin pérdida de estado

`upgradeMode: savepoint` (configurado en el ejemplo) hace que el operador:

1. dispare un savepoint final,
2. detenga el job,
3. relance la nueva versión desde ese savepoint.

```bash
# Editar la imagen
kubectl -n flink patch flinkdeployment cdc-iceberg --type=merge \
  -p '{"spec":{"image":"ghcr.io/unnamedlab/openfoundry/flink-cdc-iceberg:1.19.1-0.1.1"}}'

kubectl -n flink get flinkdeployment cdc-iceberg -w
```

### 6.3 Suspender / reanudar un job

```bash
# Suspender (dispara savepoint, conserva CR)
kubectl -n flink patch flinkdeployment cdc-iceberg --type=merge \
  -p '{"spec":{"job":{"state":"suspended"}}}'

# Reanudar desde el último savepoint
kubectl -n flink patch flinkdeployment cdc-iceberg --type=merge \
  -p '{"spec":{"job":{"state":"running"}}}'
```

### 6.4 Restaurar desde un savepoint específico (rollback)

```bash
SP="s3://openfoundry-iceberg/flink/savepoints/cdc-iceberg/savepoint-xxx"

kubectl -n flink patch flinkdeployment cdc-iceberg --type=merge \
  -p '{"spec":{"job":{"initialSavepointPath":"'"${SP}"'","upgradeMode":"savepoint","state":"running"}}}'
```

## 7. Disaster Recovery

### 7.1 Pérdida del JobManager activo

JM=2 con HA Kubernetes: el standby asume el liderazgo en segundos sin perder
estado. No hay acción manual.

### 7.2 Pérdida total del namespace `flink`

1. Reinstalar operador (§4) y volver a aplicar `FlinkDeployment` (§5).
2. El operador detectará el `high-availability.storageDir` en S3 y
   reconstruirá el job desde el último checkpoint.
3. Si el `ConfigMap` de HA fue purgado pero el bucket S3 sigue intacto, se
   puede forzar el arranque desde el último savepoint con
   `spec.job.initialSavepointPath` (§6.4).

### 7.3 Pérdida del bucket `openfoundry-iceberg`

Sin estado en S3 no hay recuperación posible. Restaurar primero los datos
desde el mirror externo (ver `infra/runbooks/ceph.md` §5.4) y luego seguir
§7.2.

### 7.4 Pérdida del operador (CRDs intactas)

```bash
# Reinstalar (idempotente)
helm upgrade --install -n flink flink-kubernetes-operator \
  flink-operator-repo/flink-kubernetes-operator \
  --version 1.10.0 -f infra/k8s/platform/manifests/flink/values.yaml
```

Los `FlinkDeployment` existentes son reconciliados al primer ciclo (60 s).

## 8. Upgrades del operador

1. Revisar [release notes][rel].
2. Pre-flight con dry-run:

   ```bash
   helm repo update
   helm upgrade --install -n flink flink-kubernetes-operator \
     flink-operator-repo/flink-kubernetes-operator \
     --version <NEW> -f infra/k8s/platform/manifests/flink/values.yaml --dry-run
   ```

3. Aplicar nuevas CRDs (Helm no las actualiza automáticamente):

   ```bash
   helm pull flink-operator-repo/flink-kubernetes-operator \
     --version <NEW> --untar -d /tmp
   kubectl apply -f /tmp/flink-kubernetes-operator/crds/
   ```

4. `helm upgrade ...` sin `--dry-run`.
5. Verificar reconciliación de un `FlinkDeployment` de prueba.

[rel]: https://github.com/apache/flink-kubernetes-operator/releases

## 9. Limpieza

```bash
# Borra el job y dispara savepoint final (upgradeMode: savepoint)
kubectl -n flink delete flinkdeployment cdc-iceberg

# Desinstalar operador
helm -n flink uninstall flink-kubernetes-operator

# CRDs (¡borra TODOS los FlinkDeployment del clúster!)
kubectl delete crd flinkdeployments.flink.apache.org \
                   flinksessionjobs.flink.apache.org

kubectl delete ns flink
```

## 10. Validación de los manifestos (CI)

Ambos comandos forman parte del check de PR (`infra/k8s/platform/manifests/flink/` modificado):

```bash
# Render del chart con nuestros values
helm template flink-kubernetes-operator \
  flink-operator-repo/flink-kubernetes-operator \
  --version 1.10.0 -n flink -f infra/k8s/platform/manifests/flink/values.yaml > /dev/null

# Validación de los manifestos estáticos
kubectl apply --dry-run=client -f infra/k8s/platform/manifests/flink/namespace.yaml
kubectl apply --dry-run=client -f infra/k8s/platform/manifests/flink/flinkdeployment-cdc-iceberg.yaml
```
