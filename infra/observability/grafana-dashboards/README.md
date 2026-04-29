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

The handful of dashboards that are **specific to OpenFoundry** (the
ADR-0012 SLO overview, the Iceberg-via-DataFusion scan latency
dashboard, the NATS control-event end-to-end view) will be added as
JSON in this directory as the corresponding histograms become
available in production. Each is marked **TBD** below.

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

### Per-SLO (OpenFoundry-specific) — TBD

These map 1:1 to the dashboards listed in ADR-0012 §4. They will be
committed as `*.json` next to this README as the underlying histograms
become available in production. The proposed UIDs are reserved.

| Dashboard | UID | Backing SLI from ADR-0012 |
|---|---|---|
| Data Plane SLO Overview              | `dp-slo-overview`  | aggregates the six SLIs |
| Flight SQL — point query SLO         | `dp-slo-flightsql` | §2.1 |
| DataFusion / Iceberg scan SLO        | `dp-slo-datafusion`| §2.2 |
| Kafka producer ack SLO               | `dp-slo-kafka`     | §2.3 |
| ClickHouse dashboard query SLO       | `dp-slo-clickhouse`| §2.4 |
| Vespa hybrid query SLO               | `dp-slo-vespa`     | §2.5 |
| NATS control event SLO               | `dp-slo-nats`      | §2.6 |

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
