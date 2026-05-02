# Mimir — long-term metrics storage (S5.4)

Grafana Mimir running in **monolithic mode** (3 replicas) with object
storage on Ceph RGW bucket `openfoundry-metrics-long`. Prometheus
remote-writes ingest series; the nightly Spark job in
[infra/k8s/spark-jobs/metrics-aggregation-service-daily.yaml](../../spark-jobs/metrics-aggregation-service-daily.yaml)
reads Mimir's S3 blocks and materialises
`of_metrics_long.service_metrics_daily` into Iceberg.

## Topology

| Role | Replicas | CPU req/lim | Mem req/lim |
|------|----------|-------------|-------------|
| `mimir` (monolithic) | 3 | 1 / 4 | 4Gi / 16Gi |

Monolithic mode keeps the moving parts low while we are pre-prod; we
split into ingester / store-gateway / querier microservices once
ingest exceeds 100k samples/s sustained (current target: 25k samples/s).

## Object storage layout

```
s3://openfoundry-metrics-long/
├── blocks/                # TSDB blocks (default 2h chunks)
├── alertmanager/          # Alertmanager state
├── ruler/                 # Recording / alerting rules
└── compactor/             # Compactor state
```

## Retention

* **Raw 2h blocks:** 90 days (compactor downsamples after 24 h).
* **Downsampled 5m blocks:** 1 year.
* **Downsampled 1h blocks:** 5 years.
* **Iceberg `service_metrics_daily`:** forever (Iceberg is the SoR for
  long-term operational analytics).

## Wiring with Prometheus

Each cluster Prometheus carries a `remote_write` block:

```yaml
remote_write:
  - url: http://mimir-distributor.observability:8080/api/v1/push
    queue_config:
      capacity: 30000
      max_shards: 30
      max_samples_per_send: 5000
    metadata_config:
      send: true
```

The S5.4 Spark job authenticates against the same bucket (separate IAM
read-only role) — never writes back to Mimir's prefix.

## Apache-2.0

Grafana Mimir is AGPL-3.0 since v3.0; we pin **v2.13.x** which is the
last Apache-2.0 release. ADR-0030 owns the upgrade decision (waiting on
upstream license clarification).
