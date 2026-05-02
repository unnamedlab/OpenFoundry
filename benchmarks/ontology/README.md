# Ontology data-plane performance baseline (Stream S1.8)

Reproducible benchmark harness for the post-Cassandra ontology hot path.
Focuses on the workload mix that S1.8.b mandates and the SLO targets
that S1.8.c locks in:

| Mix | Path | Endpoint (substrate today) |
|---|---|---|
| 80 % | read by id | `GET /api/v1/ontology/objects/{tenant}/{object_id}` |
| 15 % | read by type | `GET /api/v1/ontology/objects/{tenant}/by-type/{type_id}` |
|  5 % | write | `POST /api/v1/ontology/actions/{id}/execute` |

## SLO targets (3-node Cassandra 5.0.2, single AZ)

| Percentile | Target | k6 threshold name |
|---|---|---|
| P50 | < 5 ms | `http_req_duration{group:read-by-id}` p(50) < 5 |
| P95 | < 20 ms | `http_req_duration` p(95) < 20 |
| P99 | < 50 ms | `http_req_duration` p(99) < 50 |
| Sostenido | 5 000 RPS | `iterations_per_second` ≥ 5000 |
| Errores | < 0.1 % | `http_req_failed` rate < 0.001 |

The thresholds are wired into [`k6/ontology-mix.js`](k6/ontology-mix.js)
and the run aborts early if breached.

## Authorization mix

50 % de los reads se ejecutan con `X-Consistency: strong` (LOCAL_QUORUM,
bypass cache) y 50 % con `X-Consistency: eventual` (LOCAL_ONE + cache
moka — ver S1.5.a/d). Esto asegura cobertura del path con cache caliente
y del path quorum-bound. Todas las requests llevan `Authorization:
Bearer ${OF_BENCH_TOKEN}` para ejercer los middleware de
`auth-middleware` y la evaluación Cedar de `authz-cedar`.

## Layout

```
benchmarks/ontology/
├── README.md                       # este archivo
├── k6/
│   ├── ontology-mix.js             # harness primario (RPS-shaped, threshold-aware)
│   └── seed.sh                     # poblar ids fixture vía API
├── scenarios/
│   └── ontology-mix.json           # latency-only baseline para `of-cli bench`
└── runbooks/
    ├── hot-partitions.md           # S1.8.d — `nodetool tablestats` workflow
    └── iteration-playbook.md       # S1.8.e — qué tocar si el SLO no cumple
```

## Cómo correr

### k6 (camino canónico, 5 000 RPS sostenidos)

Requiere k6 1.0+ (`brew install k6` o `docker run grafana/k6`).

```bash
export OF_BENCH_BASE_URL=https://ontology.dev.openfoundry.local
export OF_BENCH_TOKEN=<bearer>
export OF_BENCH_TENANT=tenant-bench
export OF_BENCH_TYPE_ID=Aircraft
export OF_BENCH_OBJECT_IDS=./benchmarks/ontology/k6/object-ids.txt
export OF_BENCH_ACTION_ID=<action-id-fixture>

just bench-ontology
```

Resultados a `benchmarks/results/ontology-mix-k6.json` (formato k6
nativo). Para Grafana basta apuntar el data source de Prometheus al
exporter de k6 (`--out experimental-prometheus-rw`).

### `of-cli bench` (sequential latency baseline, sin RPS shape)

Útil para regresión rápida en CI; mide warmup×1 + medidas×5 sin
mantener carga concurrente. No pretende validar el SLO de 5 000 RPS,
sino atrapar regresiones de latencia mediana.

```bash
just bench-critical-paths   # ya existente, no toca este harness
cargo run -p of-cli -- bench run \
  --scenario benchmarks/ontology/scenarios/ontology-mix.json \
  --output benchmarks/results/ontology-mix-baseline.json
```

## Cassandra observability durante el run

Mientras corre el k6, en una segunda shell (apunta a cualquier nodo
del 3-node cluster):

```bash
watch -n5 'kubectl exec -n data cassandra-0 -- nodetool tablestats \
  ontology_objects ontology_indexes actions_log | grep -E "Read|Write|Tombstone|Bloom|Compaction"'
```

Y al final del run:

```bash
kubectl exec -n data cassandra-0 -- nodetool tablestats -F json \
  ontology_objects ontology_indexes actions_log \
  > benchmarks/results/ontology-mix-tablestats.json
```

El runbook [`runbooks/hot-partitions.md`](runbooks/hot-partitions.md)
explica qué métricas mirar (S1.8.d).

## Convenciones

* IDs UUIDv7 (orden temporal). El fixture loader (`k6/seed.sh`)
  produce `object-ids.txt` para que el harness elija aleatoriamente.
* `tenant_id` es **único por run** para no contaminar particiones de
  benchmarks anteriores; el cleanup posterior es un `TRUNCATE … USING
  TIMESTAMP …` documentado en el runbook de iteración.
* El harness asume que el read service expone el endpoint por puerto
  HTTP plano (substrate de S1.5); el gateway TLS se prueba en la
  smoke suite, no aquí.
