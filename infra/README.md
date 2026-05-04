# OpenFoundry — `infra/`

Single-source-of-truth for everything that runs OpenFoundry: Kubernetes
via Helm, Docker Compose for local dev, runbooks, scripts, Terraform.

## Layout

```
infra/
├── helm/             ← Kubernetes via Helm — single entry point
│   ├── helmfile.yaml.gotmpl   # one command: `helmfile -e dev apply`
│   ├── apps/                  # OpenFoundry application charts
│   ├── operators/             # third-party operators (cnpg, strimzi, …)
│   ├── infra/                 # third-party infra clusters / CRs
│   ├── _shared/               # shared library chart (templates only)
│   ├── profiles/              # cross-release values overlays
│   └── docs/                  # DSN contract, migration notes
│
├── compose/          ← Docker Compose dev environment (alternative to k8s)
│
├── observability/    ← Prometheus rules + Grafana dashboards (consumed by helm/)
│
├── runbooks/         ← Operational playbooks (Markdown)
│
├── scripts/          ← One-off helper scripts (backups, dev-stack, smoke tests)
│
├── terraform/        ← Cloud infra (DNS, Ceph, CDN). Orthogonal to k8s.
│
└── test-tools/       ← Load benchmarks + chaos experiments
```

## Quick reference

| I want to … | Where to look |
| --- | --- |
| Deploy everything to k8s | `cd infra/helm && helmfile -e dev apply` |
| Run with Docker Compose | `cd infra/compose && docker compose up` |
| Add a new OpenFoundry service | `infra/helm/apps/of-<release>/` |
| Add a new third-party operator | `infra/helm/operators/<name>/` |
| Add a new third-party cluster CR | `infra/helm/infra/<name>/` |
| Find a Postgres DSN convention | `infra/helm/docs/DATABASE_URL.md` |
| Investigate a runtime incident | `infra/runbooks/` |
| Understand observability rules | `infra/observability/` |

## Helm: install order (enforced by `needs:` in the helmfile)

```
Layer 1 — operators/   (cert-manager, cnpg, k8ssandra, strimzi, rook, flink)
Layer 2 — infra/       (postgres clusters, cassandra cluster, kafka cluster,
                        ceph cluster, temporal, lakekeeper, debezium,
                        flink-jobs, vespa, trino, spark-{operator,jobs}, mimir,
                        observability, local-registry)
Layer 3 — apps/        (of-platform, then of-data-engine | of-ontology |
                        of-ml-aip | of-apps-ops, then of-web)
```

`helmfile -e dev apply` runs every layer in order; profile gates skip
heavy releases on the dev profile (Vespa, Trino, Spark, Mimir, Rook,
Flink stay disabled).

No Flux. No ArgoCD. Plain Helm + Helmfile.
