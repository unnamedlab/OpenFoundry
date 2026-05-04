# OpenFoundry Grafana dashboards

This directory holds the dashboard inventory for the components covered
by [ADR-0012 — Data-plane SLOs, SLIs and error budgets][adr-0012].

For T17 we deliberately **prefer official upstream dashboards** over
hand-written JSON for the **per-component health** view: every
component listed below already publishes a maintained dashboard on
[grafana.com/grafana/dashboards][gcom] that is updated against the
same metric names our PrometheusRules (`../prometheus-rules/`)
target. Forking those dashboards into this repo would force us to
track every upstream rename of a panel or metric — work that
delivers nothing to OpenFoundry users.

What we **do ship as JSON** in this directory is the OpenFoundry-specific
material that upstream cannot provide:

1. The **per-SLO dashboards** that map 1:1 to the SLIs in
   ADR-0012 §2 — they bind a fixed PromQL expression, the SLO bound
   from §1, and the multi-window burn-rate page thresholds from §3
   into a single view that on-call uses to decide a freeze.
2. **Operator/fleet overviews** for the components whose alerts
   (`../prometheus-rules/`) target metric names not yet covered by
   any upstream dashboard at the time of writing — currently
   Lakekeeper (no upstream), CNPG (fleet view across one cluster
   per bounded context, beyond the per-instance upstream
   dashboard 20417) and Flink (uptime / checkpoints / savepoint
   age tied to the T15 maintenance schedule).

Per-SLO dashboards remain marked **TBD** only when the backing
histogram is not yet emitted by the producing service in production;
the rest are now shipped as JSON.

## Dashboard inventory

### Per-component (upstream)

| Component | Dashboard | Grafana.com ID | Datasource | Notes |
|---|---|---|---|---|
| Kafka (Strimzi)   | *Strimzi Kafka Exporter Overview*               | **[721][gc-721]**   | Prometheus | Pairs with `kafka.yaml`; covers ISR, under-replicated, consumer lag. |
| Kafka (Strimzi)   | *Strimzi Operator Overview*                     | **[14842][gc-14842]** | Prometheus | Cluster Operator + KafkaRoller view. |
| Vespa             | *Vespa Detailed Monitoring Dashboard*           | **[18308][gc-18308]** | Prometheus | Vespa-team-maintained dashboard for the metrics-proxy consumer used by `infra/k8s/helm/open-foundry/charts/vespa/templates/servicemonitor.yaml`. |
| CloudNativePG     | *CloudNativePG*                                 | **[20417][gc-20417]** | Prometheus | The dashboard published in the cnpg upstream repository. |
| Apache Flink      | *Flink Dashboard*                               | **[14911][gc-14911]** | Prometheus | Job uptime, checkpoints, savepoints — pairs with `flink.yaml`. |
| NATS / JetStream  | *NATS Server Dashboard*                         | **[2279][gc-2279]**   | Prometheus | Built for the official prometheus-nats-exporter. |
| NATS / JetStream  | *JetStream Dashboard*                           | **[14862][gc-14862]** | Prometheus | Per-stream / per-consumer view; pairs with `nats.yaml`. |
| Lakekeeper        | *Lakekeeper service overview*                   | n/a                 | Prometheus | OpenFoundry-specific — see [`lakekeeper-overview.json`](./lakekeeper-overview.json). RED metrics + sqlx pool, pairs with `lakekeeper.yaml`. |
| CloudNativePG     | *CNPG cluster fleet*                            | n/a                 | Prometheus | OpenFoundry-specific fleet view across the per-bounded-context clusters in `infra/k8s/cnpg/clusters/` — see [`cnpg-overview.json`](./cnpg-overview.json). Pairs with `cnpg.yaml`. |
| Apache Flink      | *Flink jobs overview*                           | n/a                 | Prometheus | OpenFoundry-specific — see [`flink-overview.json`](./flink-overview.json). Uptime, checkpoints, savepoint age (T15 maintenance), pairs with `flink.yaml`. |

### Per-SLO (OpenFoundry-specific)

These map 1:1 to the dashboards listed in ADR-0012 §4. All seven are
now shipped as JSON in this directory (closing T17). Each one only
yields useful values when the producing service is emitting the
backing histogram with the labels listed in ADR-0012 §2; until then
the panels render `N/A`, but importing the dashboard is still the
right move so on-call sees the wiring the moment metrics start
flowing.

| Dashboard | UID | Backing SLI from ADR-0012 | File |
|---|---|---|---|
| Data Plane SLO Overview              | `dp-slo-overview`  | aggregates the six SLIs | [`dp-slo-overview.json`](./dp-slo-overview.json) |
| Flight SQL — point query SLO         | `dp-slo-flightsql` | §2.1 | [`dp-slo-flightsql.json`](./dp-slo-flightsql.json) |
| DataFusion / Iceberg scan SLO        | `dp-slo-datafusion`| §2.2 | [`dp-slo-datafusion.json`](./dp-slo-datafusion.json) |
| Kafka producer ack SLO               | `dp-slo-kafka`     | §2.3 | [`dp-slo-kafka.json`](./dp-slo-kafka.json) |
| Vespa hybrid query SLO               | `dp-slo-vespa`     | §2.5 | [`dp-slo-vespa.json`](./dp-slo-vespa.json) |
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
