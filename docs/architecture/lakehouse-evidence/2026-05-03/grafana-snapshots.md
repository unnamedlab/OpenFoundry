# S5 Grafana Snapshots And Prometheus Queries - 2026-05-03

Status: BLOCKED - Grafana/Prometheus not accessible in current Kubernetes context
Outcome: NOT CLOSED

This file must contain Grafana snapshots, exported panel images or Prometheus
query outputs from the real S5 run. Do not use screenshots from a different
date or environment. The current execution could not capture dashboards or
Prometheus outputs because the observability stack is not deployed here.

## Execution Metadata

Local timestamp: 2026-05-03T13:26:32+0200
Kubernetes context: default

Latest collection attempt:

- Local timestamp: 2026-05-03T13:29:50+0200
- UTC timestamp: 2026-05-03T11:30:12Z
- Environment: Kubernetes context `default`
- Requested observation range: 2026-05-03T12:59:50+0200 to 2026-05-03T13:29:50+0200
- Evidence policy: empty dashboards, no-data panels and dashboards from other
  dates/environments are not valid closure evidence.

## S5 Observability Evidence Collection - 2026-05-03T13:29:50+0200

Requested signals:

- Kafka lag.
- Sink commits.
- Iceberg append latency.
- Trino query latency.
- DLQ.
- Restart recovery.
- WORM guardrail.

Collection result:

  BLOCKED. No links, screenshots or Prometheus query outputs are attached as
  closure evidence because the current cluster does not expose Grafana,
  Prometheus, Kafka, Trino, Lakekeeper, Spark Operator or S5 sink namespaces.
  Capturing a no-data dashboard would be misleading, so no empty dashboard is
  used as evidence.

Context command:

```bash
kubectl config current-context
```

Exit code: 0

```text
default
```

Timestamp command:

```bash
date +%Y-%m-%dT%H:%M:%S%z
```

Exit code: 0

```text
2026-05-03T13:29:50+0200
```

Expected S5/observability namespace discovery:

```bash
kubectl get ns kafka observability monitoring grafana trino lakekeeper of-apps-ops of-data-engine of-ml-aip of-ontology
```

Exit code: 1

```text
Error from server (NotFound): namespaces "kafka" not found
Error from server (NotFound): namespaces "observability" not found
Error from server (NotFound): namespaces "monitoring" not found
Error from server (NotFound): namespaces "grafana" not found
Error from server (NotFound): namespaces "trino" not found
Error from server (NotFound): namespaces "lakekeeper" not found
Error from server (NotFound): namespaces "of-apps-ops" not found
Error from server (NotFound): namespaces "of-data-engine" not found
Error from server (NotFound): namespaces "of-ml-aip" not found
Error from server (NotFound): namespaces "of-ontology" not found
```

Service discovery:

```bash
kubectl get svc -A
```

Exit code: 0

```text
NAMESPACE            NAME                                                       TYPE           CLUSTER-IP      EXTERNAL-IP                                 PORT(S)                      AGE
cert-manager         cert-manager                                               ClusterIP      10.43.9.75      <none>                                      9402/TCP                     4h30m
cert-manager         cert-manager-cainjector                                    ClusterIP      10.43.80.76     <none>                                      9402/TCP                     4h30m
cert-manager         cert-manager-webhook                                       ClusterIP      10.43.35.75     <none>                                      443/TCP,9402/TCP             4h30m
cnpg-system          cnpg-webhook-service                                       ClusterIP      10.43.220.91    <none>                                      443/TCP                      4h28m
default              kubernetes                                                 ClusterIP      10.43.0.1       <none>                                      443/TCP                      2d13h
k8ssandra-operator   k8ssandra-operator-cass-operator-metrics-webhook-service   ClusterIP      10.43.153.179   <none>                                      8443/TCP                     4h29m
k8ssandra-operator   k8ssandra-operator-cass-operator-webhook-service           ClusterIP      10.43.167.242   <none>                                      443/TCP                      4h29m
k8ssandra-operator   k8ssandra-operator-webhook-service                         ClusterIP      10.43.196.5     <none>                                      443/TCP                      4h29m
kube-system          kube-dns                                                   ClusterIP      10.43.0.10      <none>                                      53/UDP,53/TCP,9153/TCP       2d13h
kube-system          metrics-server                                             ClusterIP      10.43.156.125   <none>                                      443/TCP                      2d13h
kube-system          traefik                                                    LoadBalancer   10.43.45.21     192.168.105.2,192.168.105.3,192.168.105.4   80:30272/TCP,443:30231/TCP   2d13h
openfoundry          authorization-policy-service                               ClusterIP      10.43.236.31    <none>                                      8080/TCP                     4h19m
openfoundry          edge-gateway-service                                       ClusterIP      10.43.135.136   <none>                                      8080/TCP                     4h19m
openfoundry          identity-federation-service                                ClusterIP      10.43.119.215   <none>                                      8080/TCP                     4h19m
openfoundry          tenancy-organizations-service                              ClusterIP      10.43.245.83    <none>                                      8080/TCP                     4h19m
```

Deployment discovery:

```bash
kubectl get deploy -A
```

Exit code: 0

```text
NAMESPACE            NAME                               READY   UP-TO-DATE   AVAILABLE   AGE
cert-manager         cert-manager                       1/1     1            1           4h30m
cert-manager         cert-manager-cainjector            1/1     1            1           4h30m
cert-manager         cert-manager-webhook               1/1     1            1           4h30m
cnpg-system          cnpg-cloudnative-pg                1/1     1            1           4h28m
k8ssandra-operator   k8ssandra-operator                 1/1     1            1           4h29m
k8ssandra-operator   k8ssandra-operator-cass-operator   1/1     1            1           4h29m
kube-system          coredns                            1/1     1            1           2d13h
kube-system          local-path-provisioner             1/1     1            1           2d13h
kube-system          metrics-server                     1/1     1            1           2d13h
kube-system          traefik                            1/1     1            1           2d13h
openfoundry          authorization-policy-service       0/1     1            0           4h19m
openfoundry          edge-gateway-service               0/1     1            0           4h19m
openfoundry          identity-federation-service        0/1     1            0           4h19m
openfoundry          tenancy-organizations-service      0/1     1            0           4h19m
```

CRD discovery:

```bash
kubectl get prometheusrule -A
kubectl get servicemonitor -A
kubectl get kafkas -A
kubectl get kafkatopics -A
kubectl get sparkapplications -A
```

Exit codes: all 1

```text
error: the server doesn't have a resource type "prometheusrule"
error: the server doesn't have a resource type "servicemonitor"
error: the server doesn't have a resource type "kafkas"
error: the server doesn't have a resource type "kafkatopics"
error: the server doesn't have a resource type "sparkapplications"
```

Evidence links or captures for this attempt:

| Signal | Link/capture | Status | Reason |
| --- | --- | --- | --- |
| Kafka lag | none | BLOCKED | Kafka namespace/CRDs absent; no consumer group lag can be read. |
| Sink commits | none | BLOCKED | Prometheus/Grafana absent; S5 sink deployments absent. |
| Iceberg append latency | none | BLOCKED | No sink metrics backend and no Iceberg/Lakekeeper runtime. |
| Trino query latency | none | BLOCKED | Trino namespace absent; no query path or metrics. |
| DLQ | none | BLOCKED | Kafka absent; DLQ topics cannot be counted. |
| Restart recovery | none | BLOCKED | S5 deployments absent; restart drill cannot produce recovery panels. |
| WORM guardrail | none | BLOCKED | SparkApplication and PrometheusRule CRDs absent; alert state cannot be queried. |

Required Prometheus queries for real S5 evidence:

```promql
# Kafka lag / consumer lag
ontology_indexer_kafka_lag_records
audit_sink_lag_seconds
ai_sink_lag_seconds
histogram_quantile(0.99, sum by (le) (rate(ontology_indexer_lag_seconds_bucket[5m])))

# Sink commits
increase(audit_sink_commits_total[30m])
increase(ai_sink_commits_total[30m])

# Sink throughput
increase(audit_sink_records_total[30m])
increase(ai_sink_records_total[30m])
increase(ontology_indexer_records_total[30m])

# Iceberg append latency
# BLOCKING GAP: repo search did not find stable metric names for Iceberg append latency.
# Add or expose per-sink append latency histograms before claiming this panel.

# Trino query latency
# BLOCKING GAP: repo search did not identify repo-owned Trino query latency metric names.
# Use deployed Trino JMX/Prometheus metric names from the real stack and paste outputs here.

# DLQ / dead-letter / backlog
increase(streaming_dead_letter_total[30m])
max by (slot_name) (cnpg_pg_replication_slots_pg_wal_lsn_diff{slot_name="debezium_outbox_pg_policy"})
sum by (connector) (kafka_connect_connector_task_status{state!="running"})
max(pg_class_pg_table_rows{relname="events", schemaname="outbox"})

# Restart recovery
changes(kube_pod_container_status_restarts_total{namespace=~"of-apps-ops|of-data-engine|of-ml-aip|of-ontology",container=~"audit-sink|lineage-service|ai-sink|ontology-indexer|reindex"}[30m])

# WORM guardrail
ALERTS{alertname="SparkApplicationTargetsWormNamespace"}
spark_application_arguments_count{argument=~".*of_audit.*"}
```

Required Kafka commands for real S5 evidence:

```bash
kubectl -n kafka exec openfoundry-kafka-0 -c kafka -- sh -lc '
for group in audit-sink lineage-service ai-sink ontology-indexer reindex; do
  echo "===== ${group} CONSUMER GROUP LAG BY PARTITION ====="
  bin/kafka-consumer-groups.sh \
    --bootstrap-server openfoundry-kafka-bootstrap.kafka.svc:9092 \
    --describe \
    --group "${group}" || true
done
'

kubectl -n kafka exec openfoundry-kafka-0 -c kafka -- sh -lc '
for topic in audit.events.v1.dlq lineage.events.v1.dlq ai.events.v1.dlq ontology.object.changed.v1.dlq ontology.action.applied.v1.dlq ontology.reindex.v1.dlq __dlq.outbox-pg-policy.v1; do
  echo "===== ${topic} DLQ LOG-END ====="
  bin/kafka-run-class.sh kafka.tools.GetOffsetShell \
    --broker-list openfoundry-kafka-bootstrap.kafka.svc:9092 \
    --topic "${topic}" \
    --time -1 || true
done
'
```

No-close rule:

  Do not close S5-OPS from this artifact. All requested observability signals
  are missing runtime evidence in the current environment, and no empty
  dashboard has been accepted as proof.

## Required Panels

- Sink lag for `audit-sink`, `lineage-service`, `ai-sink`, `ontology-indexer` and `reindex`.
- Consumer group lag by topic and partition for all S5 groups.
- DLQ counts for S5 event topics and `__dlq.outbox-pg-policy.v1`.
- Retry/dead-letter/error metrics for the relevant runtime paths.
- Records consumed or processed by each sink/indexer.
- Iceberg commit counters for `audit-sink`, `lineage-service` and `ai-sink`.
- Alert state for Debezium, ontology-indexer lag/decode errors and Spark WORM guardrail.

## Grafana/Prometheus Discovery

Find expected observability namespaces:

```bash
kubectl get ns kafka observability monitoring grafana trino lakekeeper of-apps-ops of-data-engine of-ml-aip of-ontology
```

Exit code: 1

```text
Error from server (NotFound): namespaces "kafka" not found
Error from server (NotFound): namespaces "observability" not found
Error from server (NotFound): namespaces "monitoring" not found
Error from server (NotFound): namespaces "grafana" not found
Error from server (NotFound): namespaces "trino" not found
Error from server (NotFound): namespaces "lakekeeper" not found
Error from server (NotFound): namespaces "of-apps-ops" not found
Error from server (NotFound): namespaces "of-data-engine" not found
Error from server (NotFound): namespaces "of-ml-aip" not found
Error from server (NotFound): namespaces "of-ontology" not found
```

Find Grafana service:

```bash
kubectl get svc -A
```

Exit code: 0

```text
NAMESPACE            NAME                                                       TYPE           CLUSTER-IP      EXTERNAL-IP                                 PORT(S)                      AGE
cert-manager         cert-manager                                               ClusterIP      10.43.9.75      <none>                                      9402/TCP                     4h27m
cert-manager         cert-manager-cainjector                                    ClusterIP      10.43.80.76     <none>                                      9402/TCP                     4h27m
cert-manager         cert-manager-webhook                                       ClusterIP      10.43.35.75     <none>                                      443/TCP,9402/TCP             4h27m
cnpg-system          cnpg-webhook-service                                       ClusterIP      10.43.220.91    <none>                                      443/TCP                      4h25m
default              kubernetes                                                 ClusterIP      10.43.0.1       <none>                                      443/TCP                      2d13h
k8ssandra-operator   k8ssandra-operator-cass-operator-metrics-webhook-service   ClusterIP      10.43.153.179   <none>                                      8443/TCP                     4h26m
k8ssandra-operator   k8ssandra-operator-cass-operator-webhook-service           ClusterIP      10.43.167.242   <none>                                      443/TCP                      4h26m
k8ssandra-operator   k8ssandra-operator-webhook-service                         ClusterIP      10.43.196.5     <none>                                      443/TCP                      4h26m
kube-system          kube-dns                                                   ClusterIP      10.43.0.10      <none>                                      53/UDP,53/TCP,9153/TCP       2d13h
kube-system          metrics-server                                             ClusterIP      10.43.156.125   <none>                                      443/TCP                      2d13h
kube-system          traefik                                                    LoadBalancer   10.43.45.21     192.168.105.2,192.168.105.3,192.168.105.4   80:30272/TCP,443:30231/TCP   2d13h
openfoundry          authorization-policy-service                               ClusterIP      10.43.236.31    <none>                                      8080/TCP                     4h16m
openfoundry          edge-gateway-service                                       ClusterIP      10.43.135.136   <none>                                      8080/TCP                     4h16m
openfoundry          identity-federation-service                                ClusterIP      10.43.119.215   <none>                                      8080/TCP                     4h16m
openfoundry          tenancy-organizations-service                              ClusterIP      10.43.245.83    <none>                                      8080/TCP                     4h16m
```

Direct Grafana service lookup:

```bash
kubectl get svc grafana -n observability
```

Exit code: 1

```text
Error from server (NotFound): namespaces "observability" not found
```

Direct Prometheus service lookup:

```bash
kubectl get svc prometheus-operated -n observability
```

Exit code: 1

```text
Error from server (NotFound): namespaces "observability" not found
```

Alternate Prometheus service lookup:

```bash
kubectl get svc prometheus-k8s -n monitoring
```

Exit code: 1

```text
Error from server (NotFound): namespaces "monitoring" not found
```

PrometheusRule CRD discovery:

```bash
kubectl get prometheusrule -A
```

Exit code: 1

```text
error: the server doesn't have a resource type "prometheusrule"
```

ServiceMonitor CRD discovery:

```bash
kubectl get servicemonitor -A
```

Exit code: 1

```text
error: the server doesn't have a resource type "servicemonitor"
```

PodMonitor CRD discovery:

```bash
kubectl get podmonitor -A
```

Exit code: 1

```text
error: the server doesn't have a resource type "podmonitor"
```

## Snapshot Links Or Exported Images

- Sink lag: BLOCKED - no Grafana service found.
- Consumer lag by partition: BLOCKED - no Grafana service and no Kafka found.
- DLQ counts: BLOCKED - no Grafana service and no Kafka found.
- Retry/dead-letter/error metrics: BLOCKED - no Prometheus service found.
- Records consumed: BLOCKED - no Prometheus/Grafana service found.
- Iceberg commits: BLOCKED - no Prometheus/Grafana service found.
- Alert state: BLOCKED - PrometheusRule CRD is absent.

## Prometheus Queries To Run In The Real S5 Environment

Lag:

```promql
audit_sink_lag_seconds
ai_sink_lag_seconds
ontology_indexer_kafka_lag_records
histogram_quantile(0.99, sum by (le) (rate(ontology_indexer_lag_seconds_bucket[5m])))
```

Lineage metric caveat:

```text
Repo search did not find lineage-service Prometheus metric constants for
lineage_service_records_total, lineage_service_commits_total or
lineage_service_lag_seconds. Full S5-OPS sign-off requires equivalent deployed
metrics or an explicit instrumentation fix.
```

Records and commits:

```promql
increase(audit_sink_records_total[30m])
increase(audit_sink_commits_total[30m])
increase(ai_sink_records_total[30m])
increase(ai_sink_commits_total[30m])
increase(ontology_indexer_records_total[30m])
```

Errors, DLQ and retry/backlog:

```promql
sum by (topic) (rate(ontology_indexer_records_total{outcome="decode_error"}[5m]))
increase(streaming_dead_letter_total[30m])
max by (slot_name) (cnpg_pg_replication_slots_pg_wal_lsn_diff{slot_name="debezium_outbox_pg_policy"})
sum by (connector) (kafka_connect_connector_task_status{state!="running"})
max(pg_class_pg_table_rows{relname="events", schemaname="outbox"})
```

Alert state:

```promql
ALERTS{alertname=~"DebeziumOutboxReplicationSlotLag|DebeziumConnectTaskNotRunning|OutboxTableNotDraining|OntologyIndexerLagSLOBurn|OntologyIndexerLagSLOPageRate|OntologyIndexerDecodeErrors|SparkApplicationTargetsWormNamespace"}
```

## Repo-Backed Alert Definitions Found

Debezium:

- `DebeziumOutboxReplicationSlotLag`
- `DebeziumConnectTaskNotRunning`
- `OutboxTableNotDraining`

Source:

```text
infra/k8s/platform/manifests/debezium/prometheus-rules.yaml
```

Ontology indexer:

- `OntologyIndexerLagSLOBurn`
- `OntologyIndexerLagSLOPageRate`
- `OntologyIndexerDecodeErrors`

Source:

```text
infra/k8s/platform/manifests/observability/prometheus-rules-indexer.yaml
```

Spark WORM guardrail:

- `SparkApplicationTargetsWormNamespace`

Source:

```text
infra/k8s/platform/manifests/observability/spark-operator-rules.yaml
```

Expression:

```promql
count by (application_name) (
  spark_application_arguments_count{argument=~".*of_audit.*"}
) > 0
```

## Commands To Capture Grafana Or Prometheus Evidence

Grafana:

```bash
kubectl -n observability port-forward svc/grafana 3000:80
```

Prometheus:

```bash
kubectl -n observability port-forward svc/prometheus-operated 9090:9090
```

Query example:

```bash
curl -G 'http://127.0.0.1:9090/api/v1/query' --data-urlencode 'query=audit_sink_lag_seconds'
```

Kafka consumer lag by partition:

```bash
kubectl -n kafka exec openfoundry-kafka-0 -c kafka -- sh -lc '
for group in audit-sink lineage-service ai-sink ontology-indexer reindex; do
  echo "===== ${group} CONSUMER GROUP LAG BY PARTITION ====="
  bin/kafka-consumer-groups.sh \
    --bootstrap-server openfoundry-kafka-bootstrap.kafka.svc:9092 \
    --describe \
    --group "${group}" || true
done
'
```

Kafka DLQ counts:

```bash
kubectl -n kafka exec openfoundry-kafka-0 -c kafka -- sh -lc '
for topic in audit.events.v1.dlq lineage.events.v1.dlq ai.events.v1.dlq ontology.object.changed.v1.dlq ontology.action.applied.v1.dlq ontology.reindex.v1.dlq __dlq.outbox-pg-policy.v1; do
  echo "===== ${topic} DLQ LOG-END ====="
  bin/kafka-run-class.sh kafka.tools.GetOffsetShell \
    --broker-list openfoundry-kafka-bootstrap.kafka.svc:9092 \
    --topic "${topic}" \
    --time -1 || true
done
'
```

## Operator Conclusion

BLOCKED. No Grafana snapshots or Prometheus outputs were captured because the
current cluster has no observability stack, no Kafka runtime and no S5
consumer deployments. Do not mark this artifact PASS until the real S5
environment provides dashboard snapshots or Prometheus outputs showing healthy
lag, zero/triaged DLQ counts, retry/error state and relevant alerts.
