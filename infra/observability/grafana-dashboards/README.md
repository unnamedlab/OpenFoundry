# OpenFoundry Grafana dashboards

This directory holds the dashboard inventory for the components covered
by [ADR-0012 — Data-plane SLOs, SLIs and error budgets][adr-0012].

For T17 we deliberately **prefer official upstream dashboards** over
hand-written JSON: every component listed below already publishes a
maintained dashboard on [grafana.com/grafana/dashboards][gcom] that is
updated against the same metric names our PrometheusRules
(`../prometheus-rules/`) target. Forking those dashboards into this
repo would force us to track every upstream rename of a panel or
metric — work that delivers nothing to OpenFoundry users.

The handful of dashboards that are **specific to OpenFoundry** are
shipped as JSON in this directory. The first wave (this directory)
covers the **on-call critical path** — the ADR-0012 §3 freeze decision
hinges on the *Data Plane SLO Overview* dashboard, and the two SLIs
that have the largest uninstrumented surface today
(DataFusion/Iceberg scans, NATS control events) get a dedicated
per-SLI dashboard so a regression is debuggable without writing
ad-hoc PromQL. The remaining per-SLO dashboards listed in the table
below stay marked **TBD** until their backing histograms are emitted
in production.

## Dashboard inventory

### Per-component (upstream)

| Component | Dashboard | Grafana.com ID | Datasource | Notes |
|---|---|---|---|---|
| Kafka (Strimzi)   | *Strimzi Kafka Exporter Overview*               | **[721][gc-721]**   | Prometheus | Pairs with `kafka.yaml`; covers ISR, under-replicated, consumer lag. |
| Kafka (Strimzi)   | *Strimzi Operator Overview*                     | **[14842][gc-14842]** | Prometheus | Cluster Operator + KafkaRoller view. |
| ClickHouse        | *ClickHouse — Quick overview*                   | **[14192][gc-14192]** | Prometheus | Built against the ClickHouse `/metrics` endpoint. |
| ClickHouse        | *ClickHouse Keeper*                             | **[20783][gc-20783]** | Prometheus | Pairs with `clickhouse.yaml` Keeper rules. |
| Vespa             | *Vespa Detailed Monitoring Dashboard*           | **[18308][gc-18308]** | Prometheus | Vespa-team-maintained dashboard for the metrics-proxy consumer used by `infra/k8s/helm/open-foundry/charts/vespa/templates/servicemonitor.yaml`. |
| CloudNativePG     | *CloudNativePG*                                 | **[20417][gc-20417]** | Prometheus | The dashboard published in the cnpg upstream repository. |
| Apache Flink      | *Flink Dashboard*                               | **[14911][gc-14911]** | Prometheus | Job uptime, checkpoints, savepoints — pairs with `flink.yaml`. |
| NATS / JetStream  | *NATS Server Dashboard*                         | **[2279][gc-2279]**   | Prometheus | Built for the official prometheus-nats-exporter. |
| NATS / JetStream  | *JetStream Dashboard*                           | **[14862][gc-14862]** | Prometheus | Per-stream / per-consumer view; pairs with `nats.yaml`. |
| Lakekeeper        | *Lakekeeper service overview* — **TBD**         | n/a                 | Prometheus | No upstream dashboard; will be added here once the SLI route labels stabilise. |

### Per-SLO (OpenFoundry-specific)

These map 1:1 to the dashboards listed in ADR-0012 §4. Three are
shipped as JSON in this directory (closing T17); the rest stay **TBD**
until the corresponding histograms land in production. The proposed
UIDs are reserved.

| Dashboard | UID | Backing SLI from ADR-0012 | File |
|---|---|---|---|
| Data Plane SLO Overview              | `dp-slo-overview`  | aggregates the six SLIs | [`dp-slo-overview.json`](./dp-slo-overview.json) |
| Flight SQL — point query SLO         | `dp-slo-flightsql` | §2.1 | **TBD** |
| DataFusion / Iceberg scan SLO        | `dp-slo-datafusion`| §2.2 | [`dp-slo-datafusion.json`](./dp-slo-datafusion.json) |
| Kafka producer ack SLO               | `dp-slo-kafka`     | §2.3 | **TBD** |
| ClickHouse dashboard query SLO       | `dp-slo-clickhouse`| §2.4 | **TBD** |
| Vespa hybrid query SLO               | `dp-slo-vespa`     | §2.5 | **TBD** |
| NATS control event SLO               | `dp-slo-nats`      | §2.6 | [`dp-slo-nats.json`](./dp-slo-nats.json) |

Every shipped dashboard:

* Declares `__inputs[].name = DS_PROMETHEUS` and references the
  datasource as `${DS_PROMETHEUS}` so it can be imported into any
  Grafana that has a Prometheus datasource without patching JSON.
* Renders the panels mandated by ADR-0012 §4 — p50 / p99 / p99.9
  latencies, 30-day SLO compliance, request rate, multi-window
  burn-rate (1h / 6h with the 14.4× / 6× page thresholds drawn) and
  budget remaining.
* Targets the exact metric names and labels listed in ADR-0012 §2,
  matching what the rules under
  [`../prometheus-rules/`](../prometheus-rules/) alert on.

## Importing an upstream dashboard

```bash
# Example: import Strimzi Kafka exporter (ID 721) into a running Grafana.
DASH_ID=721
curl -fsSL "https://grafana.com/api/dashboards/${DASH_ID}/revisions/latest/download" \
  -o /tmp/kafka-strimzi.json

curl -fsSL -u admin:admin \
  -H "Content-Type: application/json" \
  -X POST http://grafana:3000/api/dashboards/import \
  -d "$(jq -n --slurpfile d /tmp/kafka-strimzi.json \
      '{dashboard: $d[0], overwrite: true, inputs: [
         {name:"DS_PROMETHEUS", type:"datasource",
          pluginId:"prometheus", value:"Prometheus"}]}')"
```

When provisioning via the kube-prometheus-stack Helm chart, the same
dashboards can be loaded declaratively through
`grafana.dashboards.<provider>` entries that point at the `gnetId` of
each row above.

## Validation

For every JSON file added to this directory:

```bash
jq . infra/observability/grafana-dashboards/*.json > /dev/null
```

[adr-0012]: ../../../docs/architecture/adr/ADR-0012-data-plane-slos.md
[gcom]: https://grafana.com/grafana/dashboards/
[gc-721]:   https://grafana.com/grafana/dashboards/721
[gc-14842]: https://grafana.com/grafana/dashboards/14842
[gc-14192]: https://grafana.com/grafana/dashboards/14192
[gc-20783]: https://grafana.com/grafana/dashboards/20783
[gc-18308]: https://grafana.com/grafana/dashboards/18308
[gc-20417]: https://grafana.com/grafana/dashboards/20417
[gc-14911]: https://grafana.com/grafana/dashboards/14911
[gc-2279]:  https://grafana.com/grafana/dashboards/2279
[gc-14862]: https://grafana.com/grafana/dashboards/14862
