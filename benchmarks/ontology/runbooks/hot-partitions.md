# Hot-partition runbook (S1.8.d)

> Owner: ontology-query-service maintainers + Cassandra/SRE on-call.
> Last evidence attempt: 2026-05-03.
> Current result: BLOCKED; no accepted S1 benchmark run is attached.

Durante (y al cierre de) cada run de `bench-ontology` se inspeccionan
las particiones más calientes de los keyspaces tocados por la mezcla:
`ontology_objects`, `ontology_indexes` y `actions_log`. Este runbook
documenta las métricas que importan, los thresholds operativos y las
acciones correctivas si una tabla se sale del rango.

## Último intento de ejecución

| Campo | Valor |
|---|---|
| Fecha | 2026-05-03 |
| Owner | ontology-query-service maintainers + Cassandra/SRE on-call |
| Entorno | Kubernetes context `default` |
| Evidencia | [`docs/architecture/slo-evidence/2026-05-03/summary.md`](../../../docs/architecture/slo-evidence/2026-05-03/summary.md) |
| Resultado | BLOCKED; no hay snapshot `nodetool tablestats` aceptado |

Comandos y resultados:

```bash
kubectl get cassandradatacenters -A
kubectl get statefulset -A
```

Resultado:

```text
No resources found.
No resources found.
```

Sin un `CassandraDatacenter` o StatefulSet de Cassandra no se puede
ejecutar `nodetool tablestats`, `toppartitions` ni `tablehistograms`.
Este intento no aprueba la evidencia de hot partitions de S1.

## Flujo

```bash
# Snapshot pre-run (línea base).
kubectl exec -n data cassandra-0 -- nodetool tablestats -F json \
  ontology_objects ontology_indexes actions_log \
  > benchmarks/results/ontology-mix-tablestats-pre.json

# Run (5 minutos, 5 000 RPS) — ver README.md.
just bench-ontology

# Snapshot post-run.
kubectl exec -n data cassandra-0 -- nodetool tablestats -F json \
  ontology_objects ontology_indexes actions_log \
  > benchmarks/results/ontology-mix-tablestats-post.json

# Diff humano.
diff <(jq -S . benchmarks/results/ontology-mix-tablestats-pre.json) \
     <(jq -S . benchmarks/results/ontology-mix-tablestats-post.json) | less
```

## Métricas que vigilar

Por cada CF (`objects_by_id`, `objects_by_type`, `objects_by_owner`,
`objects_by_marking`, `links_outgoing`, `links_incoming`, `actions_log.*`):

| Métrica | Umbral operativo | Acción si se cruza |
|---|---|---|
| `Compacted partition maximum bytes` | < 100 MiB | Repartir PK (S1.8.e). Probable hot tenant o type. |
| `Compacted partition mean bytes` | < 1 MiB | Revisar diseño de la CF; añadir bucket temporal. |
| `Local read latency p99` | < 5 ms | Verificar `nodetool tpstats` (`ReadStage` queue). |
| `Local write latency p99` | < 3 ms | Verificar tombstones y compaction backlog. |
| `Tombstones per slice (avg)` | < 100 | Truncar dataset bench y limpiar; revisar TTLs. |
| `Bloom filter false positive ratio` | < 0.01 | `nodetool upgradesstables` o `compact` puntual. |
| `Off heap memory used` | < 2 GiB por nodo | Subir heap o repartir keyspace. |

`nodetool tpstats` complementa: `ReadStage` y `MutationStage` deben
mantener `Pending` ≈ 0 con `Active` ≤ `concurrent_reads/_writes` del
yaml. `Dropped` debe ser 0 en cualquier categoría.

## Inspección puntual de la partición más caliente

```bash
# Top-10 particiones por tamaño en una CF.
kubectl exec -n data cassandra-0 -- nodetool toppartitions \
  ontology_objects objects_by_id 30000 -k 10 -s 1000

# Histograma de tamaños y latencia por host.
kubectl exec -n data cassandra-0 -- nodetool tablehistograms \
  ontology_objects objects_by_id
```

`tablehistograms` reporta percentiles de **partition size**, **cell
count**, **read latency** y **write latency**. Si P99 de partition
size > 10 MiB la PK necesita un segundo nivel de bucket (típicamente
una columna `hour_bucket` o `marking_band`).

## Hot tenants

`objects_by_type` puede convertirse en hot por (tenant, type) si un
tenant concentra el 80 % del catálogo. Mitigaciones, en orden de
preferencia:

1. **Cliente-side fan-out**: que el read service emita N queries
   paralelas con `IN (bucket_0, bucket_1, …)` sobre una columna de
   bucket determinista (`object_id % 16`).
2. **Re-modelar PK**: añadir `object_id_bucket smallint` a la PK
   compuesta. Cambia el contrato de `list_by_type` (deja de ser
   ordenado). Hacer solo si (1) no alcanza el SLO.
3. **Read-replica caliente**: aumentar `caching = { 'keys': 'ALL',
   'rows_per_partition': 'NONE' }` en la CF concreta. Validar que el
   key cache en heap no se desborda.

## Limpieza post-bench

Cada run usa un `tenant_id` único. Para liberar espacio:

```bash
# Borrar todas las rows del tenant del run con timestamp anterior.
kubectl exec -n data cassandra-0 -- cqlsh -e "
  DELETE FROM ontology_objects.objects_by_id
   WHERE tenant_id = 'tenant-bench-2026-05-02';
  DELETE FROM ontology_objects.objects_by_type
   WHERE tenant_id = 'tenant-bench-2026-05-02';"

# Forzar major compaction (solo en entorno bench, NO en prod).
kubectl exec -n data cassandra-0 -- nodetool compact ontology_objects
```

Los tombstones se evacuan con `gc_grace_seconds = 86400` (1 d) en el
keyspace de bench para evitar que el dataset siguiente arrastre lápidas
del anterior. **No** copiar este `gc_grace` a producción — el default
de 10 días sigue siendo correcto allí.
