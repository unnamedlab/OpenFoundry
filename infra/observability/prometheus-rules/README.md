# OpenFoundry Prometheus rules

This directory holds one rules file per data-plane component covered by
[ADR-0012 — Data-plane SLOs, SLIs and error budgets][adr-0012]. The
rules implement the **alerting half** of T17 (the dashboard inventory
lives next door under `../grafana-dashboards/`).

## File layout

| File | Component | Source code under `infra/k8s/` |
|---|---|---|
| `kafka.yaml`      | Strimzi-managed Apache Kafka     | `strimzi/`        |
| `clickhouse.yaml` | ClickHouse + ClickHouse Keeper   | `clickhouse/`     |
| `vespa.yaml`      | Vespa.ai search                  | `vespa/`, `helm/open-foundry/charts/vespa/` |
| `lakekeeper.yaml` | Lakekeeper (Iceberg REST catalog)| `lakekeeper/`     |
| `cnpg.yaml`       | CloudNativePG                    | `cnpg/`           |
| `flink.yaml`      | Apache Flink + maintenance jobs  | `flink/`, `flink/maintenance/` |
| `nats.yaml`       | NATS / JetStream control bus     | (compose: `infra/docker-compose.yml`) |

## Format

Each file uses the **standard Prometheus rules format** (top-level
`groups:` key). This means it can be:

1. Validated locally with `promtool check rules` (see "Validation"
   below).
2. Embedded **as-is** into the `spec:` of a `PrometheusRule` CRD
   (`monitoring.coreos.com/v1`) when deploying through the
   prometheus-operator. A minimal wrapper looks like:

   ```yaml
   apiVersion: monitoring.coreos.com/v1
   kind: PrometheusRule
   metadata:
     name: openfoundry-kafka
     namespace: monitoring
     labels:
       prometheus: kube-prometheus
       role: alert-rules
   spec:
     # Paste the contents of kafka.yaml here (groups: ...)
   ```

   This split (plain rules in repo, CRD wrapping at deploy time) is
   intentional: it keeps the rules framework-agnostic and lets the
   same files be consumed by a non-operator Prometheus deployment
   (for example a future air-gapped install) without modification.

## SLI / SLO source

Every alert threshold in this directory either:

* Quotes a number that comes directly from the upstream monitoring
  documentation of the component (e.g. ClickHouse
  `ReplicasMaxAbsoluteDelay > 60`, CNPG WAL lag > 1 GiB), **or**
* References a target latency from
  [ADR-0012][adr-0012] — for example the Vespa hybrid top-50 p99 < 80 ms
  rule, which mirrors SLI 2.5 of that ADR.

In both cases the rationale is recorded in the file header so the
threshold can be revisited without re-reading the upstream docs.

## Validation

```bash
# Syntax + PromQL parse check for every rule file.
promtool check rules infra/observability/prometheus-rules/*.yaml
```

Run this locally before opening a PR that touches any of these files.

## Deployment

OpenFoundry currently has only one application subchart under
`infra/k8s/helm/open-foundry/charts/` (the `vespa` subchart). There is
no `monitoring` umbrella subchart yet; when one is added, these files
should be templated into it as `PrometheusRule` resources via the
wrapper shown above. Until then, deploy them directly as raw manifests:

```bash
# Manual deployment (one command per file):
for f in infra/observability/prometheus-rules/*.yaml; do
  name=$(basename "$f" .yaml)
  kubectl -n monitoring create configmap "openfoundry-rules-${name}" \
    --from-file=rules.yaml="$f" --dry-run=client -o yaml |
    kubectl apply -f -
done
```

…and reference the resulting ConfigMaps from the Prometheus instance
via `additionalRuleConfigMaps`, **or** wrap each file into a
`PrometheusRule` CRD as shown above and `kubectl apply -f` it.

[adr-0012]: ../../../docs/architecture/adr/ADR-0012-data-plane-slos.md
