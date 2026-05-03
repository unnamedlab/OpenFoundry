# Kafka (Strimzi) Runbook

Fecha: 29 de abril de 2026

OpenFoundry usa **Strimzi** (Apache-2.0) como operador de Kafka y **Apicurio
Registry** (Apache-2.0) como Schema Registry. Confluent queda explícitamente
excluido (Confluent Community License no es OSS).

Manifestos: `infra/k8s/platform/manifests/strimzi/`
Runbook relacionado: `infra/runbooks/ceph.md` (los volúmenes JBOD de los
brokers se aprovisionan sobre la `StorageClass` `ceph-rbd` gestionada por
Rook).

## 1. Arquitectura desplegada

| Componente             | Configuración                                                       |
|------------------------|---------------------------------------------------------------------|
| `Kafka` (KRaft)        | 3 brokers/controllers combinados, sin ZooKeeper                     |
| `KafkaNodePool` `kafka`| `roles: [controller, broker]`, JBOD 2 × 200Gi en `ceph-rbd`         |
| Durabilidad            | `default.replication.factor=3`, `min.insync.replicas=2`, `unclean.leader.election.enable=false` |
| Listeners              | `plain` (9092 internal), `tls` (9093 internal, mTLS)                |
| Topics base            | `cdc.<source>`, `dataset.changes`, `lineage.events`, `model.inferences`, `audit.events` (P=12, RF=3) |
| Schema Registry        | Apicurio Registry, backend Postgres (CNPG `apicurio-pg`)            |
| NetworkPolicy          | `CiliumNetworkPolicy` por servicio (productor/consumidor)           |

## 2. Operaciones rutinarias

### 2.1 Rolling upgrade (operador y/o brokers)

Strimzi reconcilia cualquier cambio en el `Kafka` CR de forma rolling,
respetando `min.insync.replicas`. El procedimiento estándar:

```bash
# 1. Actualizar el operador (CRDs + controller).
helm repo update strimzi
helm upgrade -n kafka strimzi-operator strimzi/strimzi-kafka-operator \
  -f infra/k8s/platform/manifests/strimzi/values-strimzi-operator.yaml

# 2. Subir versión de Kafka. Editar `spec.kafka.version` y
#    `spec.kafka.metadataVersion` en kafka-cluster.yaml siguiendo
#    https://strimzi.io/docs/operators/latest/deploying#con-upgrade-cluster-str
kubectl apply -f infra/k8s/platform/manifests/strimzi/kafka-cluster.yaml

# 3. Observar el rollout. Los pods se reinician de uno en uno.
kubectl -n kafka get pods -l strimzi.io/cluster=openfoundry -w
kubectl -n kafka get kafka openfoundry -o jsonpath='{.status.conditions}'
```

Forzar un rolling restart manual (p.ej. tras rotar TLS):

```bash
kubectl -n kafka annotate pod -l strimzi.io/cluster=openfoundry,strimzi.io/kind=Kafka \
  strimzi.io/manual-rolling-update=true
```

Pre-flight obligatorio antes de cualquier upgrade (ver también
[ADR-0013](../../docs/architecture/adr/ADR-0013-kafka-kraft-no-spof-policy.md)
§"Layer D — Upgrade policy" y `infra/runbooks/upgrade-playbook.md`
§"KRaft upgrade preflight"):

```bash
# 1. Lint del manifest contra el contrato KRaft (Layer A).
python3 tools/kafka-lint/check_kraft.py

# 2. Sin particiones under-replicated (operación de runtime, Layer B).
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- \
  bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe --under-replicated-partitions
# Salida esperada: vacía.

# 3. Quorum KRaft sano: exactamente 1 active controller.
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- \
  bin/kafka-metadata-quorum.sh --bootstrap-server localhost:9092 describe --status

# 4. Las dos alertas KRaft del Prometheus rules deben estar en silencio
#    durante la última hora (Layer B):
#      - KafkaUnderMinIsrPartitions
#      - KafkaActiveControllerCountAbnormal
#    Verificar en el Alertmanager / dashboard de Grafana.

# 5. La última ejecución verde del chaos-suite (Layer C) debe ser ≤ 7 días.
#    Comprobarlo en el workflow `Chaos Smoke (Data Plane no-SPOF)`.
```

### 2.2 Expansión del cluster (añadir brokers)

Con `KafkaNodePool` la operación es declarativa: incrementar `spec.replicas`
y aplicar. Strimzi crea los nuevos pods, formatea sus discos JBOD y los
añade al quorum. **Las particiones existentes no se mueven automáticamente**
— hay que reasignarlas (sección 2.3).

```bash
# 1. Editar `spec.replicas: 3 → 5` en KafkaNodePool `kafka`.
kubectl -n kafka edit kafkanodepool kafka

# 2. Esperar a que los nuevos brokers estén Ready.
kubectl -n kafka get pods -l strimzi.io/pool-name=kafka -w

# 3. Validar que aparecen en el cluster.
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- \
  bin/kafka-broker-api-versions.sh --bootstrap-server localhost:9092 | grep id:
```

Para reducir el cluster (scale-in) primero hay que **vaciar** los brokers a
remover usando reasignación (sección 2.3) y solo después bajar `replicas`.
Strimzi rechaza el scale-down si detecta particiones residentes en el
broker objetivo.

### 2.3 Reasignación de particiones

Strimzi expone reasignación declarativa vía la CR `KafkaRebalance` (Cruise
Control). Es el método **recomendado**: optimiza por throughput, espacio
en disco y leadership simultáneamente.

```bash
# 1. Habilitar Cruise Control en el Kafka CR (one-time).
kubectl -n kafka patch kafka openfoundry --type merge -p '
spec:
  cruiseControl: {}
'

# 2. Crear una propuesta de rebalanceo.
cat <<'EOF' | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaRebalance
metadata:
  name: full-rebalance
  namespace: kafka
  labels:
    strimzi.io/cluster: openfoundry
spec:
  goals:
    - RackAwareGoal
    - ReplicaCapacityGoal
    - DiskCapacityGoal
    - NetworkInboundCapacityGoal
    - NetworkOutboundCapacityGoal
    - ReplicaDistributionGoal
    - LeaderReplicaDistributionGoal
EOF

# 3. Cruise Control evalúa y publica `status.optimizationResult`.
kubectl -n kafka get kafkarebalance full-rebalance -o yaml

# 4. Aprobar la propuesta para ejecutar.
kubectl -n kafka annotate kafkarebalance full-rebalance \
  strimzi.io/rebalance=approve
```

Para drenar **un broker específico** (p.ej. broker 4 antes de un
scale-in), usar el modo `remove-brokers`:

```yaml
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaRebalance
metadata:
  name: drain-broker-4
  namespace: kafka
  labels:
    strimzi.io/cluster: openfoundry
spec:
  mode: remove-brokers
  brokers: [4]
```

Modo manual (fallback sin Cruise Control), usando los scripts incluidos en
la imagen de Kafka:

```bash
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- bash
# dentro del pod:
cat > /tmp/topics.json <<'EOF'
{ "topics": [ { "topic": "dataset.changes" } ], "version": 1 }
EOF
bin/kafka-reassign-partitions.sh --bootstrap-server localhost:9092 \
  --topics-to-move-json-file /tmp/topics.json \
  --broker-list "0,1,2,3,4" --generate
# revisar el plan, guardarlo como reassignment.json y:
bin/kafka-reassign-partitions.sh --bootstrap-server localhost:9092 \
  --reassignment-json-file /tmp/reassignment.json --execute
bin/kafka-reassign-partitions.sh --bootstrap-server localhost:9092 \
  --reassignment-json-file /tmp/reassignment.json --verify
```

## 3. Topics base

Los cinco topics del data plane se crean vía CRs `KafkaTopic`
(`infra/k8s/platform/manifests/strimzi/kafka-topics.yaml`). Para añadir un nuevo topic CDC:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: cdc.mysql                 # cdc.<source>
  namespace: kafka
  labels:
    strimzi.io/cluster: openfoundry
    openfoundry.io/topic-class: cdc
spec:
  partitions: 12
  replicas: 3
  config:
    min.insync.replicas: "2"
    cleanup.policy: compact
    retention.ms: "604800000"
EOF
```

Nunca editar `partitions` a la baja (Kafka no lo soporta). Para aumentar
particiones, editar el CR; el operador propaga el cambio. Para cambiar
RF, usar reasignación (sección 2.3).

## 4. Apicurio Registry

* Endpoint in-cluster: `http://apicurio-registry.apicurio.svc:8080`
* Backend: PostgreSQL HA via CNPG (`apicurio-pg-rw.apicurio.svc:5432`).
* Backups: heredan la política de CNPG del cluster (Barman/WAL archive a
  Ceph S3).

Operaciones típicas:

```bash
# Listar grupos de schemas.
curl -s http://apicurio-registry.apicurio.svc:8080/apis/registry/v3/groups | jq

# Subir un schema Avro.
curl -X POST -H 'Content-Type: application/json; artifactType=AVRO' \
  --data-binary @schemas/dataset-changes.avsc \
  http://apicurio-registry.apicurio.svc:8080/apis/registry/v3/groups/openfoundry/artifacts
```

Recovery del backend Postgres: ver `infra/runbooks/disaster-recovery.md`
(sección CNPG); la `Cluster` de Apicurio sigue el mismo procedimiento que
cualquier otro `postgresql.cnpg.io/v1 Cluster` de la plataforma (plantilla
de referencia en `infra/k8s/platform/manifests/cnpg/templates/cluster.yaml`; el antiguo
subchart de Apache Polaris fue retirado por
[ADR-0008](../../docs/architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md)).

## 5. NetworkPolicies

Las `CiliumNetworkPolicy` en `network-policies.yaml` aplican
**default-deny** sobre los pods de Kafka y Apicurio, y abren puertos sólo
a pods etiquetados con:

* `openfoundry.io/kafka-role: producer | consumer | producer-consumer`
* `openfoundry.io/service: <nombre>` (dataset-service, lineage-service…)

Cuando se incorpora un servicio nuevo:

1. Etiquetar el Deployment/StatefulSet con ambas labels.
2. Añadir el nombre a la lista `matchExpressions[*].values` correspondiente
   en `network-policies.yaml`.
3. Crear un `KafkaUser` con las ACLs mínimas (un PR aparte).
4. `kubectl apply -f infra/k8s/platform/manifests/strimzi/network-policies.yaml`.

## 6. Troubleshooting

| Síntoma                                 | Causa probable                              | Acción                                                 |
|-----------------------------------------|---------------------------------------------|--------------------------------------------------------|
| Productor recibe `NOT_ENOUGH_REPLICAS`  | `min.insync.replicas=2` y un broker caído   | Verificar `kubectl get pods -n kafka`, esperar rolling. Alerta canónica: `KafkaUnderMinIsrPartitions` (ver [ADR-0013](../../docs/architecture/adr/ADR-0013-kafka-kraft-no-spof-policy.md) §"Layer B"). |
| `LEADER_NOT_AVAILABLE` tras scale-in    | Particiones quedaron en broker eliminado    | Reasignar (sección 2.3) y reintentar                   |
| Pods Pending por PVC                    | `ceph-rbd` sin capacidad                    | Ver `infra/runbooks/ceph.md` (expansión de OSDs)       |
| Apicurio devuelve 503                   | CNPG primario en failover                   | `kubectl -n apicurio get cluster apicurio-pg`          |
| NetworkPolicy bloquea tráfico legítimo  | Falta etiquetado del servicio               | Ver sección 5 y reaplicar policies                     |
| Sospecha de pérdida de active controller (cero) o split-brain (dos) | Bug del operador, fallo de red entre controladores, o estado inconsistente tras un upgrade | Alerta `KafkaActiveControllerCountAbnormal`. Inspeccionar `bin/kafka-metadata-quorum.sh ... describe --status`. La validación nightly que cubre este caso es `smoke/chaos/kill-active-kafka-controller.sh` (ver [ADR-0013](../../docs/architecture/adr/ADR-0013-kafka-kraft-no-spof-policy.md) §"Layer C"). |
