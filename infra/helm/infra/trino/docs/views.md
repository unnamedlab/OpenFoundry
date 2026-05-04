# Trino views — substrate

This directory pins the **DDL** of the Trino views the platform serves
on top of Iceberg. The Trino cluster itself lands in **S5.6**; until
then these files exist so:

* The DDL is reviewed and merged alongside the producers/consumers
  that emit the underlying tables (no naming drift between Iceberg
  table columns and Trino view projections).
* When the chart lands the bootstrap Job applies the directory
  verbatim (`for f in *.sql; do trino --execute @${f}; done`).

## Contents

| File | Purpose | Tracked by |
|------|---------|------------|
| `of_ai.sql` | Views over `of_ai.{prompts,responses,evaluations,traces}` for model-evaluation queries. | S5.3.c |

Future view files (`of_lineage.sql`, `of_audit.sql`, `of_metrics.sql`)
land alongside their respective producers.

## Conventions

* **Schema:** all views live in `of_<domain>` namespace and are named
  `v_<purpose>` (lowercase, snake_case).
* **Read-only:** never `CREATE TABLE AS SELECT` — that would hide an
  Iceberg materialisation; use a real Iceberg table if persistence is
  needed.
* **Partition pruning:** every view filters or projects `at` (or the
  table's partition source) so Trino can skip files.
* **Strict types:** cast JSON-extracted scores to `DOUBLE`; never let
  Trino infer numerical types from JSON paths.
* **Idempotent:** every file uses `CREATE OR REPLACE VIEW` so re-apply
  is safe.
