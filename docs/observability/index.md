# Observability

This section covers runtime visibility, health, auditability, and
operational diagnosis for OpenFoundry's data plane and supporting
services. The latency, error and saturation targets that drive the
content below are defined in
[ADR-0012 — Data-plane SLOs, SLIs and error budgets][adr-0012]; this
page is the **operational index** of the artefacts that implement
those targets (Prometheus rules, Grafana dashboards, runbooks).

## OpenFoundry mapping

- `/health` conventions across services
- `services/audit-service`
- tracing dependencies in the Rust workspace
- smoke and benchmark suites
- runbooks under `infra/runbooks`
- alerting rules under `infra/observability/prometheus-rules/`
- dashboard inventory under `infra/observability/grafana-dashboards/`

## Key concerns

- health and readiness
- logs, traces, and metrics
- runtime smoke validation
- incident diagnosis and recovery support
- SLO compliance and error-budget tracking (per ADR-0012)

## SLI / SLO coverage by component

The table below cross-references each component to (a) the SLI/SLO it
must meet, (b) the Prometheus rule file that alerts on a breach, and
(c) the Grafana dashboard used to investigate it.

| Component       | Primary SLI                                     | SLO target                       | Prometheus rules                                                       | Grafana dashboard                                                  | Source            |
|---|---|---|---|---|---|
| Kafka (Strimzi) | Producer ack p99 / ISR / consumer lag           | p99 ack < 25 ms (ADR-0012 §2.3)  | [`prometheus-rules/kafka.yaml`](../../infra/observability/prometheus-rules/kafka.yaml)           | Strimzi Kafka Exporter (Grafana.com #721, #14842)                  | `infra/k8s/strimzi/`     |
| ClickHouse      | Dashboard query p99 / replica drift / Keeper    | p99 query < 200 ms (ADR-0012 §2.4) | [`prometheus-rules/clickhouse.yaml`](../../infra/observability/prometheus-rules/clickhouse.yaml) | ClickHouse Quick Overview (#14192) + Keeper (#20783)               | `infra/k8s/clickhouse/`  |
| Vespa           | Hybrid top-50 query p99 / content availability  | p99 query < 80 ms (ADR-0012 §2.5)  | [`prometheus-rules/vespa.yaml`](../../infra/observability/prometheus-rules/vespa.yaml)           | Vespa Detailed Monitoring (#18308)                                 | `infra/k8s/vespa/`       |
| Lakekeeper      | 5xx rate / catalog request p99 / DB pool        | p99 < 500 ms, 5xx < 1 % (upstream guidance) | [`prometheus-rules/lakekeeper.yaml`](../../infra/observability/prometheus-rules/lakekeeper.yaml) | TBD (OpenFoundry-specific)                                         | `infra/k8s/lakekeeper/`  |
| CloudNativePG   | Replica lag / failover / WAL archive            | Replica lag < 1 GiB, WAL archive failures = 0 (cnpg upstream) | [`prometheus-rules/cnpg.yaml`](../../infra/observability/prometheus-rules/cnpg.yaml) | CloudNativePG (#20417)                                            | `infra/k8s/cnpg/`        |
| Apache Flink    | Job uptime / checkpoint failures / savepoint age| Job up = 1, < 3 failed checkpoints / 30 m, savepoint < 24 h (T15 maintenance) | [`prometheus-rules/flink.yaml`](../../infra/observability/prometheus-rules/flink.yaml) | Flink Dashboard (#14911)                                           | `infra/k8s/flink/`       |
| NATS / JetStream| Node availability / JetStream consumer lag      | NATS control event p99 < 5 ms (ADR-0012 §2.6) | [`prometheus-rules/nats.yaml`](../../infra/observability/prometheus-rules/nats.yaml) | NATS Server (#2279) + JetStream (#14862)                          | `infra/docker-compose.yml` |

The full table of latency SLOs, error budgets and freeze policy is in
[ADR-0012 §1–§3][adr-0012]. The ADR is the authority; this page is the
operational index.

## Artefacts

### Prometheus rules

Standard Prometheus rules format (top-level `groups:` key). One file
per component, plus a `README.md` describing the format and the deploy
options:

- [`infra/observability/prometheus-rules/`](../../infra/observability/prometheus-rules/)

Validate locally:

```bash
promtool check rules infra/observability/prometheus-rules/*.yaml
```

### Grafana dashboards

For T17 we reference the **maintained upstream dashboards** on
[grafana.com/grafana/dashboards](https://grafana.com/grafana/dashboards/)
rather than vendor copies into the repo. The OpenFoundry-specific
SLO dashboards listed in ADR-0012 §4 are committed as JSON in the same
directory once their backing histograms are live in production. Full
list with Grafana.com IDs and import instructions:

- [`infra/observability/grafana-dashboards/`](../../infra/observability/grafana-dashboards/)

## Deployment

OpenFoundry currently exposes its monitoring story through ADR-0012
(Prometheus + Grafana + Loki + Tempo). The repository ships the rules
and dashboard inventory; the Prometheus / Grafana stack itself lives
outside the application umbrella chart for now (only `vespa` is a
subchart of `infra/k8s/helm/open-foundry/charts/`). Two supported
deploy paths:

1. **Operator-based (recommended).** Wrap each rules file into a
   `PrometheusRule` CRD as documented in
   [`infra/observability/prometheus-rules/README.md`](../../infra/observability/prometheus-rules/README.md)
   and provision the upstream dashboards via the kube-prometheus-stack
   Helm values (`grafana.dashboards.<provider>` with the `gnetId` of
   each row in the table above).

2. **Raw manifests.** `kubectl create configmap` per rules file, then
   reference the ConfigMaps from a stand-alone Prometheus instance.
   Dashboards can be imported through the Grafana HTTP API
   (`POST /api/dashboards/import`) using the snippet in the
   dashboards README.

When a `monitoring` subchart is added under
`infra/k8s/helm/open-foundry/charts/`, both of these paths should be
absorbed into it as templates; the rules files in this directory are
intentionally framework-agnostic to make that future migration a
mechanical wrap operation.

## Monitoring stack status

The `infra/docker-compose.monitoring.yml` stub previously referenced
from ADR-0012 was empty and has been removed to avoid giving a false
signal of an existing monitoring stack. With the artefacts under
`infra/observability/` this page is no longer a stub: the alerting
rules and dashboard inventory described above are the deliverable for
T17. The Prometheus / Grafana / Loki / Tempo runtime stack itself
will be reintroduced as a `monitoring` subchart of
`infra/k8s/helm/open-foundry/charts/` in a follow-up; the artefacts
here are designed to plug into that stack without modification.

## References

- [ADR-0012 — Data-plane SLOs, SLIs and error budgets][adr-0012]
- [`infra/observability/prometheus-rules/README.md`](../../infra/observability/prometheus-rules/README.md)
- [`infra/observability/grafana-dashboards/README.md`](../../infra/observability/grafana-dashboards/README.md)
- [`docs/operations/deployment-modes.md`](../operations/deployment-modes.md)

[adr-0012]: ../architecture/adr/ADR-0012-data-plane-slos.md
