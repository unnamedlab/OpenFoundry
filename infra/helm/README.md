# `infra/helm/` — single Helm tree

Everything OpenFoundry runs on Kubernetes is defined here, orchestrated
by **one** [`helmfile.yaml.gotmpl`](helmfile.yaml.gotmpl). No Flux, no
ArgoCD, no separate "platform" vs "apps" Helmfiles.

## Tree

```
helm/
├── helmfile.yaml.gotmpl    # entrypoint — `helmfile -e dev apply`
├── _shared/                # library chart consumed by every app chart
├── apps/                   # ━━━ OpenFoundry application ━━━
│   ├── of-platform/        # gateway, identity, authz, tenancy, workers
│   ├── of-data-engine/     # datasets, pipelines, lineage, connectors
│   ├── of-ontology/        # object DB, query, indexer, definitions
│   ├── of-ml-aip/          # LLM gateway, agents, model lifecycle
│   ├── of-apps-ops/        # apps, marketplace, audit, notebook, geo, …
│   └── of-web/             # SvelteKit SPA frontend
├── operators/              # ━━━ Third-party operators (upstream charts) ━━━
│   ├── cert-manager/  cnpg/  k8ssandra/  strimzi/  rook-ceph/  flink/
├── infra/                  # ━━━ Third-party infra (CRs / clusters) ━━━
│   ├── postgres-clusters/  # 4 CNPG Postgres Cluster CRs + bootstrap-SQL
│   ├── cassandra-cluster/  # K8ssandra Cluster CR + keyspaces Job
│   ├── kafka-cluster/      # Strimzi Kafka + Topics + ACLs + Apicurio
│   ├── ceph-cluster/       # Rook Ceph Cluster + ObjectStore + Bucket
│   ├── temporal/           # upstream temporal chart + UI ingress
│   ├── lakekeeper/         # upstream lakekeeper chart + region-B
│   ├── debezium/           # KafkaConnect + outbox connectors
│   ├── flink-jobs/         # FlinkDeployment + Iceberg maintenance
│   ├── vespa/              # vendored Vespa chart + app package
│   ├── trino/              # vendored Trino chart + connectors + views
│   ├── spark-operator/     # vendored Spark Operator
│   ├── spark-jobs/         # SparkApplication CRs (Iceberg compaction, …)
│   ├── mimir/              # vendored Mimir chart
│   ├── observability/      # shared PrometheusRules + ServiceMonitors
│   └── local-registry/     # dev-only in-cluster Docker registry
├── profiles/               # cross-release overlays per environment
│   └── values-{dev,staging,prod,airgap,multicloud,sovereign-eu,apollo}.yaml
└── docs/
    ├── DATABASE_URL.md     # Postgres DSN contract
    └── MIGRATION.md        # how chart migrations work (Helm hooks)
```

## What lives where, in one sentence

* `apps/`       — code we wrote.
* `operators/`  — operators someone else wrote (Strimzi, CNPG, …).
* `infra/`      — clusters/CRs we tell those operators to provision.
* `_shared/`    — library helpers consumed by `apps/`.
* `profiles/`   — environment-wide values (dev/staging/prod/postures).

## Install / upgrade

```sh
cd infra/helm

# render only — no cluster contact
helmfile -e dev template

# diff against current cluster state
helmfile -e dev diff

# apply
helmfile -e dev apply

# tear down
helmfile -e dev destroy
```

Environments: `dev`, `staging`, `prod`. Postures (layered on top of
`prod`): `airgap`, `multicloud`, `sovereign-eu`, `apollo`.

## Install order

The helmfile groups releases in three layers and uses `needs:` to enforce
order between them:

1. **Operators** — cert-manager, cnpg, k8ssandra-operator,
   strimzi-operator, rook-ceph-operator, flink-operator.
2. **Infrastructure clusters / CRs** — postgres-clusters,
   cassandra-cluster, kafka-cluster, ceph-cluster, temporal, lakekeeper,
   debezium, flink-jobs, vespa, trino, spark-operator, spark-jobs, mimir,
   observability, local-registry.
3. **Application** — of-platform first; then of-data-engine, of-ontology,
   of-ml-aip, of-apps-ops in parallel; of-web last.

Within a layer, releases install concurrently. Across layers, helmfile
waits for every dependency to be `deployed` (per `needs:`).

## Profile gates (dev-friendly defaults)

The dev profile keeps heavy releases off:

| Release | dev | staging | prod |
| --- | :---: | :---: | :---: |
| Vespa, Trino, Spark, Mimir, Rook-Ceph, Flink | off | partial | on |
| Cassandra, Kafka, Postgres, Temporal, Lakekeeper, Debezium | on | on | on |
| local-registry | on | off | off |

## Adding a new release

* New OpenFoundry service → add to `apps/<release>/values.yaml` and the
  service catalogue (no new chart needed; the 5 release-aligned charts
  already template every service).
* New third-party operator → create `operators/<name>/Chart.yaml` with
  the upstream dependency, and add a release entry to the helmfile.
* New third-party cluster CR → create `infra/<name>/templates/...yaml`
  with the CR, plus `Chart.yaml` and `values.yaml`. Reference the
  operator it depends on via `needs:` in the helmfile.

## What got removed in this refactor

* `infra/k8s/platform/manifests/` — duplicate of root-level dirs.
* `infra/k8s/helm/open-foundry/` — legacy umbrella chart (ADR-0031).
* `infra/k8s/clickhouse/` — ClickHouse stack (no longer used).
* Flux v2 `HelmRelease` for Temporal — replaced by an in-tree wrapper
  chart that depends on the upstream `temporal/temporal` chart.
* The split between `platform/helmfile.yaml.gotmpl` and
  `helm/helmfile.yaml.gotmpl` — collapsed into a single file.
