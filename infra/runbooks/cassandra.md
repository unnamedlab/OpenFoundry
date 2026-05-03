# Cassandra (k8ssandra-operator) Runbook

OpenFoundry usa **Apache Cassandra 5.0** (Apache-2.0) gestionado por
**k8ssandra-operator** (Apache-2.0). El umbrella incluye `cass-operator`,
**Reaper** (auto-repair) y **Medusa** (backups a Ceph S3). Stargate
queda explícitamente desactivado: los servicios Rust hablan CQL
directamente con el crate `scylla`.

Manifestos: `infra/k8s/platform/manifests/cassandra/`
Runbooks relacionados:
- `infra/runbooks/ceph.md` — los PVs de los nodos viven en
  `ceph-rbd-fast` (NVMe, replicación 3x con failure domain `zone`).
- `infra/runbooks/disaster-recovery.md` — flujos cross-DC y restauración
  desde backups Medusa.

ADRs relacionadas:
- [ADR-0020](../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md) — adopción y reglas de modelado.
- [ADR-0021](../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md) — Temporal sobre Cassandra.
- Modelo de datos completo: [`docs/architecture/data-model-cassandra.md`](../../docs/architecture/data-model-cassandra.md).

## 1. Arquitectura desplegada

| Componente              | Configuración                                                                 |
|-------------------------|-------------------------------------------------------------------------------|
| `K8ssandraCluster` prod | 3 DCs (`dc1`, `dc2`, `dc3`) × 3 nodos × 3 racks (zone-aware)                  |
| Estrategia replicación  | `NetworkTopologyStrategy {dc1:3, dc2:3, dc3:3}` para todas las keyspaces      |
| Consistencia por defecto| `LOCAL_QUORUM` (lectura y escritura)                                          |
| Snitch                  | `GossipingPropertyFileSnitch`                                                 |
| Tokens                  | `num_tokens=16`, `allocate_tokens_for_local_replication_factor=3`             |
| JVM                     | G1GC, heap 32 GiB, young 4 GiB                                                |
| Storage                 | 2 TiB por pod sobre `ceph-rbd-fast` (NVMe)                                    |
| Reaper                  | `deploymentMode: PER_DC`, autoScheduling cada 12h, sub-range parallelism      |
| Medusa                  | Bucket `cassandra-backups-prod` (Ceph RGW), full nightly + diff cada 6h, 30d  |
| Métricas                | MCAC sidecar (port 9103) + Reaper `/healthcheck/metrics`                      |
| TLS                     | Internode + cliente, certificados emitidos por cert-manager                   |

Keyspaces aplicativas (creadas por
[`keyspaces-job.yaml`](../k8s/platform/manifests/cassandra/keyspaces-job.yaml)):
`ontology_objects`, `ontology_indexes`, `actions_log`, `auth_runtime`,
`notifications_inbox`, `agent_state`. Las dos de Temporal
(`temporal_persistence`, `temporal_visibility`) las gestiona el chart
de Temporal con `temporal-cassandra-tool`.

## 2. Acceso operacional

```bash
# Shell cqlsh contra DC1 con las credenciales de superuser.
SU_USER=$(kubectl -n cassandra get secret of-cass-prod-superuser -o jsonpath='{.data.username}' | base64 -d)
SU_PASS=$(kubectl -n cassandra get secret of-cass-prod-superuser -o jsonpath='{.data.password}' | base64 -d)

kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c cassandra -- \
  cqlsh -u "$SU_USER" -p "$SU_PASS"

# Estado del cluster (replicación, ownership, UN/DN por nodo).
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c cassandra -- \
  nodetool status
```

Toda operación destructiva (truncate, drop, repair forzado en
producción) **requiere ticket de change** y validación con el on-call.

## 3. Operaciones rutinarias

### 3.1 Repair (anti-entropy)

Reaper corre por defecto: schedule per-DC cada 12h con sub-range
parallelism. Comprobar el estado:

```bash
# UI de Reaper (port-forward).
kubectl -n cassandra port-forward svc/of-cass-prod-reaper-service 8080:8080
# http://localhost:8080/webui

# Última repair exitosa por keyspace (Prometheus).
# Alerta: CassandraRepairOverdue (> 10 días) en
# infra/k8s/platform/manifests/cassandra/servicemonitor.yaml.
```

Forzar una repair manual de una keyspace concreta (ej. tras un
incidente o cambio de schema grande):

```bash
# Vía Reaper (preferido — sub-range, throttled).
curl -X POST "http://localhost:8080/repair_run?clusterName=of-cass-prod&keyspace=ontology_objects&owner=oncall&segmentCount=64&repairParallelism=DATACENTER_AWARE&intensity=0.5"

# Vía nodetool (último recurso, una sola DC, primary range).
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c cassandra -- \
  nodetool repair -pr -j 4 ontology_objects
```

Reglas:
- Nunca lanzar `nodetool repair` full multi-DC en horario de tráfico.
- `-pr` (primary range) es obligatorio si se ejecuta en cada nodo del DC.
- Monitorizar `mcac_compaction_pending_tasks` durante la repair.

### 3.2 Scale-out (añadir nodos a un DC)

```bash
# 1. Editar el K8ssandraCluster: subir `datacenters[i].size`.
kubectl -n cassandra edit k8ssandracluster of-cass-prod
#    Cambiar: spec.cassandra.datacenters[?(@.metadata.name=="dc1")].size
#             de 3 a 6 (siempre múltiplo de #racks para mantener balance).

# 2. cass-operator añade nodos uno a uno respetando los racks.
#    Verificar el join progresivo:
kubectl -n cassandra get pods -l cassandra.datastax.com/cluster=of-cass-prod -w
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c cassandra -- nodetool status

# 3. Tras `UN` (Up/Normal) en todos los nuevos nodos, ejecutar cleanup
#    en los nodos previos para liberar el ownership transferido.
for pod in $(kubectl -n cassandra get pods -l cassandra.datastax.com/datacenter=dc1,cassandra.datastax.com/cluster=of-cass-prod -o name | head -3); do
  kubectl -n cassandra exec -it "$pod" -c cassandra -- nodetool cleanup
done
```

Reglas:
- Subir `size` en pasos múltiplos del número de racks (3) para no
  desbalancear el ownership por zona.
- `concurrent_compactors=4` y throughput limitado evitan saturar los
  vecinos durante el bootstrap.
- Tras scale-out, lanzar Reaper sobre todas las keyspaces (al menos
  `ontology_objects` y `actions_log`) antes de considerar el cluster
  estable.

### 3.3 Replace-node (sustituir un nodo perdido)

Aplica cuando un PVC se ha corrompido o un nodo lleva > 1h `DN` (Down/Normal):

```bash
# 1. Confirmar el host_id del nodo perdido.
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c cassandra -- \
  nodetool status | grep DN
#    DN  10.42.7.18  ...  cd7e3a1c-...

# 2. cass-operator gestiona el reemplazo vía CassandraTask.
cat <<'EOF' | kubectl apply -f -
apiVersion: control.k8ssandra.io/v1alpha1
kind: CassandraTask
metadata:
  name: replace-dc1-rack2-sts-1
  namespace: cassandra
spec:
  datacenter:
    name: of-cass-prod-dc1
    namespace: cassandra
  jobs:
    - name: replace-node
      command: replacenode
      args:
        pod_name: of-cass-prod-dc1-rack2-sts-1
EOF

# 3. Seguir el progreso.
kubectl -n cassandra get cassandratask replace-dc1-rack2-sts-1 -w

# 4. Verificar que el nuevo pod aparece UN y el antiguo host_id ha
#    desaparecido del ring.
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c cassandra -- \
  nodetool status
```

Si `cass-operator` no puede orquestar el reemplazo (caso degradado,
sin un PVC sano del que arrancar), el procedimiento manual es:

```bash
# 1. Decommission del nodo muerto (si todavía responde gossip).
nodetool removenode <host_id>

# 2. Borrar el PVC y dejar que el StatefulSet recree el pod.
kubectl -n cassandra delete pvc server-data-of-cass-prod-dc1-rack2-sts-1

# 3. Tras el bootstrap, repair total de la DC.
```

### 3.4 Restore desde Medusa

Asume backup `full_2026-04-29` y bucket `cassandra-backups-prod`.

```bash
# 1. Listar backups disponibles.
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c medusa -- \
  medusa list-backups

# 2. Restaurar un keyspace concreto in-place (cluster vivo).
#    Útil tras corrupción lógica (DROP TABLE, etc.).
cat <<'EOF' | kubectl apply -f -
apiVersion: medusa.k8ssandra.io/v1alpha1
kind: MedusaRestoreJob
metadata:
  name: restore-ontology-objects-2026-04-29
  namespace: cassandra
spec:
  cassandraDatacenter: dc1
  backup: full_2026-04-29
  shutdown: false
EOF

# 3. Restauración de cluster completo (DR, ver disaster-recovery.md).
#    Requiere desescalar todas las DCs y partir de un cluster vacío.
#    Procedimiento detallado en infra/runbooks/disaster-recovery.md.
```

Reglas:
- Restore in-place no escala más allá de 1 keyspace pequeña; para
  recuperaciones grandes, restaurar en un cluster paralelo y reanudar
  servicios contra él.
- Verificar siempre con `nodetool status` y un `SELECT count(*)`
  representativo antes de declarar el restore terminado.

## 4. Troubleshooting

### 4.1 Tombstone storms

Síntoma: alerta `CassandraTombstoneScans`, p99 de lectura disparada en
una tabla concreta.

```sql
-- Identificar tablas problemáticas.
SELECT keyspace_name, table_name, tombstones_per_slice_p99
FROM system_views.coordinator_scans;
```

Causas habituales:
- TTL muy corto sobre tabla con compaction LCS.
- Patrón "delete then insert" en lugar de upsert.

Acción: revisar el modelo (ADR-0020 §"reglas duras"), considerar
TWCS si los datos son inmutables con expiración.

### 4.2 Large partitions

Alerta: `CassandraLargePartition` (> 100 MB).

```bash
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -c cassandra -- \
  nodetool tablehistograms <keyspace> <table>
```

Acción inmediata: **no** se arregla con repair ni con compaction. Es
un bug de modelo; abrir incidente con el equipo dueño del schema y
re-bucketear (añadir un componente temporal o de hash a la PK).

### 4.3 GC pauses largas

Síntoma: pods marcados `DN` brevemente, picos de latencia en intervalos
de minutos.

```bash
kubectl -n cassandra logs of-cass-prod-dc1-rack1-sts-0 -c cassandra | \
  grep -E 'GCInspector|Pause'
```

Si las pausas G1 superan 500 ms de forma consistente:
- Subir heap (revisar workload, no exceder 50% memoria del pod).
- Comprobar `concurrent_compactors` y `compaction_throughput`.
- Verificar que no hay tombstone storm en marcha.

### 4.4 Hints backlog creciente

Alerta: `CassandraHintsBacklog` (> 10k en progreso).

Indica que un nodo lleva tiempo inalcanzable. Confirmar con
`nodetool status` y, si el nodo no vuelve en `max_hint_window` (3h),
ejecutar `nodetool truncatehints` tras una repair completa para
evitar replays incoherentes.

## 5. Escalado on-call

| Severidad | Disparador                                                                 | Acción                                    |
|-----------|----------------------------------------------------------------------------|-------------------------------------------|
| `page`    | `CassandraQuorumAtRisk` (≥ 2 nodos `DN` en una DC)                         | Llamar al on-call de plataforma de inmediato. |
| `page`    | `CassandraNodeDown` (≥ 5 min)                                              | Triage; si `cass-operator` no recupera, replace-node. |
| `ticket`  | `CassandraLargePartition`, `CassandraTombstoneScans`, `CassandraReadLatencyP99High` | Abrir issue al dueño del schema; revisar ADR-0020. |
| `ticket`  | `CassandraPendingCompactions`, `CassandraHintsBacklog`, `CassandraRepairOverdue` | Acción operativa según secciones §3 y §4. |

## 6. Pre-flight para upgrades

Antes de subir versión de Cassandra o de k8ssandra-operator:

1. `nodetool status` — todos los nodos `UN` en todas las DCs.
2. Sin alertas activas en las últimas 2 h.
3. Última Reaper exitosa por keyspace ≤ 7 días.
4. Snapshot full Medusa reciente (≤ 24 h) y verificación de listado.
5. Validar la matriz de compatibilidad k8ssandra-operator ↔ Cassandra
   en https://docs.k8ssandra.io/install/release-notes/.
6. Aplicar primero en `cluster-dev.yaml`, dejar reposar 24 h.

El procedimiento general de upgrade (rolling, DC por DC) sigue las
reglas de `infra/runbooks/upgrade-playbook.md`.
