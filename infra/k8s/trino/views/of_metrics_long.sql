-- S5.4.a — view over the long-term metrics warehouse.
-- The base table is materialised by
-- infra/k8s/spark-jobs/metrics-aggregation-service-daily.yaml.
-- Schema is intentionally narrow: one row per (service, day, metric)
-- so that BI dashboards can pivot freely without reaching back into
-- Mimir.

CREATE SCHEMA IF NOT EXISTS iceberg.of_metrics_long
WITH (location = 's3://openfoundry-iceberg/of_metrics_long/');

CREATE TABLE IF NOT EXISTS iceberg.of_metrics_long.service_metrics_daily (
    service        VARCHAR    NOT NULL,
    metric_name    VARCHAR    NOT NULL,
    day            DATE       NOT NULL,
    p50            DOUBLE,
    p95            DOUBLE,
    p99            DOUBLE,
    avg            DOUBLE,
    sum            DOUBLE,
    n_samples      BIGINT,
    at             TIMESTAMP(6) WITH TIME ZONE NOT NULL  -- materialisation time
)
WITH (
    partitioning = ARRAY['day(at)'],
    format = 'PARQUET',
    format_version = 2,
    sorted_by = ARRAY['service', 'metric_name']
);

-- Convenience view for common BI joins (Grafana, Superset).
CREATE OR REPLACE VIEW iceberg.of_metrics_long.v_service_latency_daily AS
SELECT service,
       day,
       p50,
       p95,
       p99,
       n_samples
FROM   iceberg.of_metrics_long.service_metrics_daily
WHERE  metric_name LIKE 'http_request_duration_seconds%';

CREATE OR REPLACE VIEW iceberg.of_metrics_long.v_service_error_rate_daily AS
SELECT service,
       day,
       sum AS error_count,
       n_samples
FROM   iceberg.of_metrics_long.service_metrics_daily
WHERE  metric_name = 'http_requests_total_errors';
