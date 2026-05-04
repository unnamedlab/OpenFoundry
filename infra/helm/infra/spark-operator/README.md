# Spark Operator on K8s (S5.7)

Apache Spark Operator (Apache-2.0, [kubeflow/spark-operator](https://github.com/kubeflow/spark-operator))
running in `spark-operator` namespace. Workloads land as
`SparkApplication` CRs under [../spark-jobs/](../spark-jobs/).

## Why Apache, not Bitnami

Bitnami's Spark image is now under a non-Apache license tier; we pin
the Apache `apache/spark-py:3.5.x` image bundled with the operator.

## Boundaries

* **Allowed:** rewrite data files, compact, optimise on
  `of_lineage.*`, `of_ai.*`, `of_metrics_long.*`.
* **Forbidden:** ANY operation against `of_audit.*`. The audit
  namespace is WORM (S5.1.c). The `expire-snapshots` job below
  enumerates an explicit allowlist and refuses anything else.

## Telemetry

Operator + jobs scrape Prometheus on `:8090`. Alerts in
[../observability/spark-operator-rules.yaml](../observability/spark-operator-rules.yaml).
