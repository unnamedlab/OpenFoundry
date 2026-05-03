# Platform Manifests

This directory contains Kubernetes substrate that is owned by the platform
layer but is not an OpenFoundry application chart.

| Directory | Owns |
| --- | --- |
| `cassandra/` | k8ssandra operator values, Cassandra CRs, keyspaces, Medusa/Reaper support |
| `cnpg/` | CloudNativePG operator values, cluster CRs, bootstrap SQL, poolers |
| `debezium/` | Kafka Connect, Debezium connectors, users, monitors, alert rules |
| `flink/` | Flink operator values, CDC/Iceberg deployments, maintenance jobs |
| `lakekeeper/` | Lakekeeper chart values, namespace, region-B read-only overlay |
| `local-registry/` | Dev-only in-cluster Docker registry for k3s/lima clusters (push from host, pull from cluster) |
| `observability/` | Platform-local observability rules and Mimir base values |
| `rook/` | Rook-Ceph desired topology, object stores, buckets, multisite CRs |
| `spark-jobs/` | SparkApplication/CronJob workload definitions |
| `spark-operator/` | Spark Operator base values and operator notes |
| `strimzi/` | Kafka, topics, ACLs, Apicurio, MirrorMaker2 desired CRs |
| `temporal/` | Temporal base values, bootstrap keyspaces, UI ingress, ServiceMonitor |
| `trino/` | Trino base values, connectors, analytical view bootstrap SQL |

Profile overlays consumed directly by the platform Helmfile live in
[`../values/`](../values/). Vendored third-party charts live in
[`../charts/`](../charts/).
