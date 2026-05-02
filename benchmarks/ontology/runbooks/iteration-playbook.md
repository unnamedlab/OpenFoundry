# Iteration playbook (S1.8.e) — qué tocar si el SLO no cumple

Si el run de `bench-ontology` cierra con thresholds rojos, este
runbook ordena las palancas de mitigación de **menor a mayor coste**.
Aplicar una a la vez, re-correr el bench, y solo escalar a la
siguiente si la métrica fallida persiste.

## Síntoma → palanca

### P50 alto (> 5 ms) en `read-by-id`

1. **Cache miss-rate elevado** — verificar `cache::tests` rate en el
   read service (`/metrics` expone `ontology_query_cache_hits_total`
   / `ontology_query_cache_misses_total`). Si miss > 30 %:
   - Subir `CACHE_CAPACITY` (default 100 000, S1.5.a). Cada entrada
     ≈ 4 KiB → 100 k = 400 MiB heap. 200 k es seguro en pods 1 GiB.
   - Subir `CACHE_TTL_SECONDS` (default 30) a 60-120 si la
     invalidación NATS llega fiable (verificar
     `ontology_query_invalidation_consumed_total`).
2. **Connection-pool starvation Scylla driver** — `scylla::session`
   defaults a 1 conn/host; subir vía `ClusterConfig::connection_pool_per_host`
   a 4 si hay > 2 cores y CPU disponible.
3. **Quorum forzado** — si > 80 % de la carga viene `X-Consistency:
   strong`, verificar que el cliente real necesita LOCAL_QUORUM. La
   mayoría de UI reads tolera `eventual`. Documentar default por
   ruta.

### P95/P99 alto (> 20 / > 50 ms)

1. **Hot partition** — ver
   [`hot-partitions.md`](hot-partitions.md). El `tablehistograms` P99
   de partition size > 10 MiB es la causa más común. Aplicar bucketing
   en PK (S1.8.e tradicional) y re-correr.
2. **GC en JVM Cassandra** — `nodetool gcstats`. Si `MaxGCPause` > 200
   ms, subir heap (`MAX_HEAP_SIZE` 8G→16G), validar G1 collector
   config, y considerar Cassandra 5.0 con JDK 17 + ZGC en pods
   bench.
3. **Read repair amplificado** — `nodetool tablestats … | grep
   "Read repaired"`. Si > 0.1 % en LOCAL_QUORUM hay drift entre
   réplicas; ejecutar `nodetool repair -pr` por nodo en ventana
   tranquila.

### Throughput < 5 000 RPS

1. **Dropped iterations en k6** — el resumen de k6 reporta
   `dropped_iterations`. Si > 0:
   - Subir `preAllocatedVUs` y `maxVUs` en
     [`k6/ontology-mix.js`](../k6/ontology-mix.js).
   - Verificar `noConnectionReuse: false` (default). Cada VU mantiene
     keep-alive con el read service.
2. **CPU del read service** — `kubectl top pod -n ontology`. Si > 80
   % sostenido, escalar HPA y re-correr. La meta de RPS asume 3
   réplicas en t-shirt `m5.large`-equivalente.
3. **Saturación NIC entre k6 y read service** — si el k6 se ejecuta
   fuera del cluster, mover a un pod del cluster con
   `kind: PodSpec.tolerations` para apuntar al mismo nodo del read
   service (`benchmarks/ontology/k8s/k6-job.yaml` — TBD si se decide
   correr in-cluster por defecto).

### Error rate > 0.1 %

1. **`429 Too Many Requests`** — el rate-limit del gateway está más
   bajo que la carga. El bench bypassa el gateway (apunta directo al
   read service); si el run se hace contra el gateway, subir el cupo
   por tenant del bench en `services/edge-gateway-service/config/`.
2. **`503 Service Unavailable`** — Cassandra unreachable o LWT
   timeout. Verificar `nodetool status`; si un nodo está en `DN`,
   abortar el run y documentar — no aceptar SLO con cluster
   degradado.
3. **`401 / 403`** — el token caducó. Refrescar `OF_BENCH_TOKEN`. La
   suite no implementa refresh automático para evitar contaminar la
   métrica con CPU del cliente.

## Cuándo cambiar el modelo de datos

Llegar al modelo es el último recurso porque rompe contratos de
clientes. Solo proceder si:

- Las palancas anteriores se agotaron y P95 sigue > 20 ms en P99 < 50
  ms.
- `tablehistograms` muestra P99 partition > 10 MiB sostenido aún tras
  major compaction.
- El tráfico real (no el bench) se asemeja al perfil que dispara el
  hot partition.

Cambios viables, **siempre con ADR**:

1. **Añadir bucket a PK** de `objects_by_type` (`tenant, type, bucket
   = object_id_hash % N`). Requiere que el read service emita N
   queries paralelas.
2. **Materialized view** de `objects_by_id` con `marking` como
   clustering — solo si las queries autorizadas dominan; OJO
   con la amplificación de write.
3. **Secondary index SAI** sobre `marking` — Cassandra 5.0 trae
   `StorageAttachedIndex` con cost model decente para filtros con
   selectividad > 5 %. Validar antes con un benchmark dedicado.

Cualquiera de estos tres cambios cierra una migración menor (data
backfill + cambio de proto/SDK) y tiene que documentarse antes de
incorporarse al tronco principal.

## Definición de éxito de la iteración

El bench cierra verde cuando, sobre 3 runs consecutivos en una
ventana de 1 hora:

- Los 4 thresholds globales del k6 pasan.
- `dropped_iterations` < 0.01 % de `iterations`.
- `nodetool tablestats … | grep "Compacted partition maximum"` no
  superó 100 MiB en ninguna CF tocada.
- `nodetool tpstats` reporta 0 dropped en `ReadStage` y
  `MutationStage`.

Solo entonces se anota el resultado en `ADR-0012-data-plane-slos.md`
(S1.9.c) y se cierra S1.8.
