# Lakekeeper region B — read-only follower (S7.1.b)

This folder is the region B overlay for the Lakekeeper Iceberg REST
catalog. It pairs with:

* [`infra/runbooks/ceph-multisite-bootstrap.md`](../../../runbooks/ceph-multisite-bootstrap.md) (S7.1.a)
* [`infra/k8s/cnpg/clusters/pg-lakekeeper.yaml`](../../cnpg/clusters/pg-lakekeeper.yaml)
  → its replica counterpart in region B (S7.4.a) `pg-lakekeeper-replica`.

## Files

| File                                           | Purpose                                                               |
| ---------------------------------------------- | --------------------------------------------------------------------- |
| [`values-region-b.yaml`](values-region-b.yaml) | Helm overlay forcing Lakekeeper into read-only mode in region B.      |
| [`iceberg-replication-smoke.yaml`](iceberg-replication-smoke.yaml) | One-shot Spark Job that asserts cross-region lag < 60 s (S7.1.c).     |

## Read-only enforcement (defence in depth)

| Layer            | Mechanism                                                             |
| ---------------- | --------------------------------------------------------------------- |
| Catalog REST API | `authz.backend: read_only_allowall` rejects mutating endpoints (403). |
| Postgres metadata| CNPG replica cluster `pg-lakekeeper-replica` rejects writes.          |
| Object storage   | Ceph RGW secondary zone `openfoundry-zone-b` is read-only by design.  |

If the chart version pinned by `catalog.image.tag` does not expose
`read_only_allowall`, replace `authz.backend` with `opa` and ship a
Rego policy denying every `*:write*` / `*:create*` / `*:drop*` action;
the README in the parent folder documents the OPA option.

## SLO

* RPO ≤ 60 s on the bucket (parquet + manifest sync via Ceph multisite).
* RPO ≤ 5 s on the catalog metadata (CNPG streaming async, S7.4.a).
* RTO is N/A — promotion to read-write requires the failover runbook
  ([`dr-failover.md`](../../../runbooks/dr-failover.md), S7.5.a).

## Smoke test

The Job in [`iceberg-replication-smoke.yaml`](iceberg-replication-smoke.yaml)
writes a `smoke_replication.heartbeat_<ts>` table in region A, then
polls region B until the row is readable, asserting < 60 s lag. Wire to
a `CronJob` (every 5 min) in any environment that wants continuous lag
SLO observability.
