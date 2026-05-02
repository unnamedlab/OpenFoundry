# Archived migrations — `ontology-timeseries-analytics-service`

These DDL files used to live at
`services/ontology-timeseries-analytics-service/migrations/`. They
define dashboard and saved-query metadata for the time-series
analytics surface.

## Tables

* `ontology_timeseries_dashboards` — dashboard JSON payload (panels,
  layout, refresh intervals).
* `ontology_timeseries_queries` — saved query definitions parented to
  a dashboard.

## S1.7 split (hot vs declarative)

**Both tables are declarative** — definition-shaped, low-cardinality,
written by humans through the authoring UI. They move whole to
`pg-schemas.ontology_schema`. There is no Cassandra component for this
service: the **runtime** time-series data lives in
`time-series-data-service` (P29) and is read through that service.

## Why archived

Schema collapses into the consolidated `pg-schemas` apply (same
mechanism as S1.6). Service binary no longer invokes
`sqlx::migrate!`.
