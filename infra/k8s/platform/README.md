# OpenFoundry Platform Layer

This directory owns third-party Kubernetes releases that should not be
upgraded as part of the OpenFoundry application charts.

## Layout

| Path | Purpose |
| --- | --- |
| `charts/` | Vendored third-party Helm charts pinned in git |
| `values/` | Per-profile overlays consumed by `helmfile.yaml.gotmpl` |
| `manifests/` | Operator CRs, upstream chart values, bootstrap Jobs, and raw Kubernetes manifests |
| `observability/` | Shared Prometheus rules, Grafana dashboards, and monitor CRs |
| `packages/` | Runtime packages bundled by charts, currently the Vespa application package |

## Releases

| Release | Namespace | Status |
| --- | --- | --- |
| `vespa` | `OPENFOUNDRY_NAMESPACE` / `HELM_NAMESPACE` / `openfoundry` | Active for `staging` and `prod` |
| `trino` | `TRINO_NAMESPACE` / `trino` | Active for `prod` |
| `spark-operator` | `SPARK_OPERATOR_NAMESPACE` / `spark-operator` | Active for `prod` |
| `mimir` | `MIMIR_NAMESPACE` / `observability` | Active for `prod` |

## Usage

```sh
cd infra/k8s/platform
helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor"
helmfile -e prod apply
```

The extra `--api-versions` flag is only needed for offline rendering when
the current kube context does not expose Prometheus Operator CRDs. A real
cluster install expects `PodMonitor`/`ServiceMonitor` CRDs to exist.

Vespa keeps the historical resource fullname `of-ontology-vespa` so the
ontology indexer can continue using `http://of-ontology-vespa:8080` and
StatefulSet PVC names do not change during the split.

Third-party charts are vendored under `charts/` and pinned by their
`Chart.yaml` metadata:

- Trino `0.30.0` / app `459`
- Spark Operator `2.0.2`
- Mimir Distributed `5.4.1` / app `2.13.0`

Base values for Trino, Spark Operator and Mimir live next to their
supporting platform manifests:

- `manifests/trino/values.yaml`
- `manifests/spark-operator/values.yaml`
- `manifests/observability/mimir/values.yaml`

For existing clusters, upgrade the application Helmfile with
`of-ontology.vespa.enabled=false` before installing the platform Vespa
release, otherwise Kubernetes will reject duplicate resource names owned
by the old Helm release.
