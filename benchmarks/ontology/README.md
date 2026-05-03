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
nativo), `benchmarks/results/ontology-mix-summary.json` y
`benchmarks/results/ontology-mix-metadata.json`. La receta llama a
[`scripts/run-s1-baseline.sh`](scripts/run-s1-baseline.sh), que hace
preflight de variables/dataset, ejecuta k6 y genera
`benchmarks/results/adr-0012-s1-baseline.md` con la tabla que se pega en
ADR-0012. Para Grafana basta apuntar el data source de Prometheus al
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

## Running against a live cluster

Para poblar la tabla A.4 de
[`docs/architecture/adr/ADR-0012-data-plane-slos.md`](../../docs/architecture/adr/ADR-0012-data-plane-slos.md)
y cerrar el gate **G-S1** del
[`migration-plan-cassandra-foundry-parity.md`](../../migration-plan-cassandra-foundry-parity.md)
hay que correr el harness **dentro del cluster**. Lanzarlo desde el
laptop introduce 10–40 ms de jitter WAN que enmascara el SLO P99 < 50
ms, así que el camino soportado es Job + PVC en
`infra/k8s/bench/`:

| Manifest | Contenido |
|---|---|
| [`infra/k8s/bench/ontology-bench-namespace.yaml`](../../infra/k8s/bench/ontology-bench-namespace.yaml) | Namespace `openfoundry-bench`, RBAC (`bench-runner` SA con `pods/exec` en `cassandra` ns), PVC `bench-artefacts` (5 GiB RWO), `CronJob ontology-bench-k6` con `suspend: true` ejecutando k6 0.55. |
| [`infra/k8s/bench/ontology-bench-credentials.yaml`](../../infra/k8s/bench/ontology-bench-credentials.yaml) | `ExternalSecret` que proyecta desde Vault (`secret/data/openfoundry/bench/ontology-bench-token`) un JWT firmado por identity-federation con `tenant=bench-tenant` y los scopes mínimos. |
| [`infra/k8s/bench/ontology-bench-seed-job.yaml`](../../infra/k8s/bench/ontology-bench-seed-job.yaml) | Job idempotente que inserta 50 000 objetos (5 000 × 10 type_ids `bench-type-T01…T10`, mismo shape que el IT de `libs/cassandra-kernel`) y luego invoca `seed.sh` para harvestear `object-ids.txt`. |

### 1. Bootstrap

```bash
# Crea el namespace, RBAC, PVC y el CronJob k6 (suspendido).
kubectl apply -f infra/k8s/bench/ontology-bench-namespace.yaml

# Proyecta el JWT desde Vault (requiere external-secrets operator).
kubectl apply -f infra/k8s/bench/ontology-bench-credentials.yaml

# Empuja los scripts canónicos del repo a un ConfigMap. Re-ejecuta
# este comando cada vez que cambies ontology-mix.js o seed.sh: los
# manifests intencionalmente NO embeben el contenido para evitar drift.
kubectl -n openfoundry-bench create configmap bench-k6-scripts \
  --from-file=ontology-mix.js=benchmarks/ontology/k6/ontology-mix.js \
  --from-file=seed.sh=benchmarks/ontology/k6/seed.sh \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 2. Poblar el tenant (seed Job)

```bash
kubectl apply -f infra/k8s/bench/ontology-bench-seed-job.yaml
kubectl -n openfoundry-bench wait --for=condition=complete \
  job/ontology-bench-seed --timeout=45m
kubectl -n openfoundry-bench logs -l app.kubernetes.io/name=ontology-bench-seed --tail=-1
```

El Job sale con código 2 si el harvest se queda > 10 % por debajo de
los 50 000 esperados (síntoma de que el tenant fue truncado a media
ejecución). El resumen JSON queda en `/data/results/seed-summary.json`
del PVC.

### 3. Disparar el bench k6

El `CronJob` está `suspend: true` por diseño — operadores lanzan una
ejecución puntual con `kubectl create job --from`:

```bash
kubectl -n openfoundry-bench create job \
  --from=cronjob/ontology-bench-k6 \
  ontology-bench-k6-$(date +%Y%m%d-%H%M)

kubectl -n openfoundry-bench wait --for=condition=complete \
  job/ontology-bench-k6-<timestamp> --timeout=15m
kubectl -n openfoundry-bench logs -l app.kubernetes.io/name=ontology-bench-k6 --tail=-1
```

k6 escribe en el PVC:

* `/data/results/ontology-mix-k6.json` — output JSON nativo de k6.
* `/data/results/ontology-mix-summary.json` — `--summary-export`,
  útil para CI dashboards.

### 4. Recoger artefactos del PVC

El PVC es `ReadWriteOnce`, así que no se puede montar en dos pods a la
vez. Para extraer los JSON al disco local levantamos un pod efímero
(`tar` + `kubectl cp` no funciona contra PVCs no montados):

```bash
kubectl -n openfoundry-bench run bench-fetch \
  --rm -it --restart=Never \
  --overrides='{
    "spec": {
      "containers": [{
        "name":"fetch",
        "image":"busybox:1.37",
        "command":["sh","-c","sleep 600"],
        "volumeMounts":[{"name":"d","mountPath":"/data","readOnly":true}]
      }],
      "volumes":[{"name":"d","persistentVolumeClaim":{"claimName":"bench-artefacts","readOnly":true}}]
    }
  }' \
  --image=busybox:1.37 -- sh

# en otra shell:
kubectl -n openfoundry-bench cp \
  bench-fetch:/data/results/ontology-mix-k6.json \
  benchmarks/results/ontology-mix-k6.json
kubectl -n openfoundry-bench cp \
  bench-fetch:/data/results/ontology-mix-summary.json \
  benchmarks/results/ontology-mix-summary.json
```

### 5. Snapshot de `nodetool tablestats` post-run

El `bench-runner` SA tiene `pods/exec` en el namespace `cassandra`
(via `RoleBinding bench-cassandra-exec`). Lanzamos un pod sidecar
efímero en `openfoundry-bench` que ejerce ese permiso y persiste el
JSON al mismo PVC:

```bash
kubectl -n openfoundry-bench run bench-tablestats \
  --rm -it --restart=Never \
  --serviceaccount=bench-runner \
  --image=bitnami/kubectl:1.31 \
  --overrides='{
    "spec":{
      "serviceAccountName":"bench-runner",
      "containers":[{
        "name":"ts","image":"bitnami/kubectl:1.31",
        "command":["sh","-c","kubectl -n cassandra exec of-cass-prod-dc1-default-sts-0 -c cassandra -- nodetool tablestats -F json ontology_objects ontology_indexes actions_log > /data/results/ontology-mix-tablestats.json"],
        "volumeMounts":[{"name":"d","mountPath":"/data"}]
      }],
      "volumes":[{"name":"d","persistentVolumeClaim":{"claimName":"bench-artefacts"}}]
    }
  }' -- sh
```

Tras lo cual se copia con el mismo patrón del paso 4
(`bench-fetch:/data/results/ontology-mix-tablestats.json` →
`benchmarks/results/`).

### 6. Cleanup

```bash
kubectl -n openfoundry-bench delete job ontology-bench-seed
kubectl -n openfoundry-bench delete job -l app.kubernetes.io/name=ontology-bench-k6
# El PVC se conserva para diff entre runs; bórralo cuando hayas
# ingresado los artefactos en benchmarks/results/.
kubectl -n openfoundry-bench delete pvc bench-artefacts
# Y ejecutar el TRUNCATE documentado en runbooks/iteration-playbook.md
# contra el keyspace `ontology_objects` (tenant_id = 'bench-tenant').
```

> **Aviso G-S1.** Hasta que los tres JSON
> (`ontology-mix-k6.json`, `ontology-mix-summary.json`,
> `ontology-mix-tablestats.json`) vivan bajo `benchmarks/results/`
> y la tabla A.4 del ADR-0012 esté completa, el gate G-S1 sigue
> abierto. La ejecución real se hace en el Prompt 3 — este Prompt
> sólo entrega el harness in-cluster.
