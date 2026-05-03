# Platform Observability

This directory contains observability assets owned by the Kubernetes
platform layer but not packaged as OpenFoundry application charts.

| Path | Purpose |
| --- | --- |
| `prometheus-rules/` | Prometheus rule groups validated with `promtool` |
| `grafana-dashboards/` | JSON dashboards and dashboard inventory |
| `podmonitors/` | Prometheus Operator monitor CRs that are not emitted by an app chart |

Deployable platform manifests and chart values still live in
[`../manifests/observability/`](../manifests/observability/). This
directory is the source catalog for shared rules and dashboards.
