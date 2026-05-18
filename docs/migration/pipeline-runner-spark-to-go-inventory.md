# Pipeline runner Spark → Go: inventory (ADR-0045 Phase 0)

- **Status:** Inventory complete — gate result: **all pipelines re-expressible (no bucket (c))**.
- **Date:** 2026-05-17
- **Owner:** Pipelines working group
- **Parent ADR:** [ADR-0045 — Eliminate `pipeline-runner-spark`, port the pipeline runtime to pure Go](../architecture/adr/ADR-0045-eliminate-pipeline-runner-spark-pure-go-runtime.md)

This document is the **deliverable of Phase 0** of ADR-0045. The ADR
states that Phase 0 must enumerate every production pipeline whose
DAG node config resolves to raw SQL beyond a simple SELECT shape and
classify it into one of three buckets:

- **(a)** re-expressible with the current `pipeline-expression` vocabulary as-is,
- **(b)** requires extending the vocabulary (multi-table join, aggregation, window, …),
- **(c)** blocked — depends on Spark-specific behaviour with no Go analogue.

**ADR-0045 Phase A may only start if bucket (c) is empty.**

## TL;DR

- **6 concrete pipelines** materialised in the repo today, all under
  the `online-retail` PoC. No production-database-resident pipelines
  exist outside this set.
- **Bucket (a):** 3 pipelines — trivial SELECT / WHERE / CAST / scalar functions. Run on the existing FASTER vocabulary unchanged.
- **Bucket (b):** 3 pipelines — `GROUP BY` + aggregates, `WITH … CROSS JOIN` CTE, multi-statement CSV ingest. Re-expressible after extending `pipeline-expression` with an `aggregate` operator (already a stub in `transform_catalog.go`) and replacing the Spark CSV DataSource with a `connector-management-service` ingest step.
- **Bucket (c):** **empty.** No pipeline in the repo depends on Spark-only features that have no Go path. No production pipeline exceeds single-pod working memory (every executor config seen is `instances: 1`).
- **Free-form SQL is the *composer's capability*, not what pipelines actually use.** The composer passes `cfg.SQL` straight to the Spark CLI ([`distributed_runtime.go:126`](../../services/pipeline-build-service/internal/handler/distributed_runtime.go)) with no validation, but every concrete pipeline in the repo uses only a small, classifiable subset of SQL.

**Gate decision: PASS.** Phase A of ADR-0045 (Iceberg → object-database
indexer in Go) may proceed. Phase C must add the `aggregate` operator
to the Go runtime *before* re-platforming the analytical pipelines.

## Methodology

Sources walked (in order of trust):

1. **Concrete pipeline definitions** — `infra/dev/**/*.yaml` `SparkApplication`
   CRs, test-data DAGs in `services/pipeline-build-service/internal/handler/*_test.go`,
   seeds in `services/pipeline-build-service/migrations/`.
2. **Composer surface** — every site that builds the `--inline-sql`
   argument: [`internal/spark/spark.go`](../../services/pipeline-build-service/internal/spark/spark.go),
   [`internal/handler/distributed_runtime.go`](../../services/pipeline-build-service/internal/handler/distributed_runtime.go),
   [`internal/domain/runners/runners.go`](../../services/pipeline-build-service/internal/domain/runners/runners.go).
3. **Declarative catalog** — [`internal/handler/transform_catalog.go`](../../services/pipeline-build-service/internal/handler/transform_catalog.go).
4. **FASTER runtime support surface** — [`internal/handler/lightweight_runtime.go`](../../services/pipeline-build-service/internal/handler/lightweight_runtime.go), `Supports()` function.
5. **Verified-by-grep:** every line reference in §A / §C / §D has been
   confirmed by direct read against the file at the cited line range.

Pipelines that exist only as authoring-time abstractions (the
composer can *in principle* produce arbitrary SQL) are noted in §B
but do not count as concrete pipelines for the bucket classification.

## §A. Concrete pipelines materialised in the repo

| # | Pipeline ID | Source | Node kind | Bucket | SQL shape (verbatim or paraphrased) |
|---|---|---|---|---|---|
| 1 | `online-retail-clean` (transactions-clean) | [`infra/dev/poc-pipeline-nodes.yaml:4-82`](../../infra/dev/poc-pipeline-nodes.yaml) | TRANSFORM | **(a)** | `SELECT CONCAT(invoice,'_',stockcode) AS transaction_id, …, CAST(quantity*price AS DOUBLE) AS revenue FROM lakekeeper.default.online_retail_raw WHERE quantity > 0 AND price > 0 AND customer_id IS NOT NULL` |
| 2 | `online-retail-returns` | [`infra/dev/poc-pipeline-nodes.yaml:87-172`](../../infra/dev/poc-pipeline-nodes.yaml) | TRANSFORM | **(a)** | Same projection as #1 but `WHERE quantity < 0` |
| 3 | `online-retail-cust` (customer-metrics) | [`infra/dev/poc-pipeline-nodes.yaml:177-262`](../../infra/dev/poc-pipeline-nodes.yaml) | ANALYTICAL | **(b)** | `SELECT customer_id, SUM(revenue) AS total_revenue, COUNT(DISTINCT invoice) AS num_orders, COUNT(DISTINCT country) AS num_countries FROM lakekeeper.default.transactions_clean GROUP BY customer_id` |
| 4 | `online-retail-anomalies` | [`infra/dev/poc-pipeline-nodes.yaml:267-359`](../../infra/dev/poc-pipeline-nodes.yaml) (CTE body confirmed at lines 290–307) | ANALYTICAL | **(b)** | `WITH stats AS (SELECT AVG(revenue) AS mean_rev, STDDEV(revenue) AS std_rev FROM lakekeeper.default.transactions_clean) SELECT t.*, CAST(((t.revenue - s.mean_rev)/NULLIF(s.std_rev,0)) AS DOUBLE) AS revenue_zscore, CAST(ABS(...) > 3.0 AS BOOLEAN) AS is_anomaly, CAST('pending' AS STRING) AS review_status FROM lakekeeper.default.transactions_clean t CROSS JOIN stats s` |
| 5 | `online-retail-ingest` | [`infra/dev/spark-ingest-online-retail.yaml:37-51`](../../infra/dev/spark-ingest-online-retail.yaml) | EXTRACT (ingest) | **(b)** | `CREATE OR REPLACE TEMPORARY VIEW raw_csv USING csv OPTIONS (path 's3a://…', header 'true'); SELECT invoice, stockcode, description, CAST(quantity AS INT), CAST(invoice_date AS TIMESTAMP), … FROM raw_csv` |
| 6 | `spark-smoke` | [`infra/dev/spark-smoke.yaml:41-42`](../../infra/dev/spark-smoke.yaml) | SMOKE | **(a)** | `SELECT 1 AS one` |
| 7 | test fixtures | [`services/pipeline-build-service/internal/handler/distributed_runtime_test.go:36,119`](../../services/pipeline-build-service/internal/handler/distributed_runtime_test.go) | TRANSFORM | **(a)** | `SELECT * FROM trails` and `SELECT * FROM input_table` |

**Negative finding.** No `INSERT INTO`, no `UPDATE`, no `MERGE INTO`, no
Iceberg `CALL` procedures, no Spark UDFs (Scala or Python), no window
functions, no Iceberg time-travel reads, no `MERGE INTO` upserts, no
`OPTIMIZE`/`EXPIRE SNAPSHOTS`, and no SparkML pipelines appear in any
materialised pipeline in the repo. Searched across all of
`infra/dev/`, `services/pipeline-build-service/**`, `tools/online-retail/`,
and `docs/poc-online-retail/`.

**Production-DB-resident pipelines.** None. The
`services/pipeline-build-service/migrations/` directory contains schema
DDL only — no seeded pipeline rows. Any production pipeline created
through the authoring UI lives in `pipeline_authoring.published_dag`
at runtime; this inventory cannot enumerate it from a clean checkout.
Operators running this migration in a live environment **must**
re-run the bucket classification against `pipeline_authoring.published_dag`
JSON before signing off Phase A.

## §B. SQL capabilities the composer *can* emit (vs. *does* emit)

The composer extracts SQL at [`distributed_runtime.go:126`](../../services/pipeline-build-service/internal/handler/distributed_runtime.go):

```go
InlineSQL: firstNonEmpty(cfg.SQL, cfg.Statement),
```

This is a **raw passthrough** — whatever JSON the node config carries
in `sql` or `statement` becomes the `--inline-sql` argument that
`PipelineRunner.scala` hands to `spark.sql(...)`. There is **no SQL
parser, no allowlist, no semantic validation** in the composer path.
In principle the composer can therefore emit any SQL Spark 3.5 +
Iceberg 1.5 accept: window functions, CTEs, UDFs, `MERGE INTO`,
`CREATE OR REPLACE TEMPORARY VIEW`, Spark-specific functions like
`from_utc_timestamp`, multi-statement scripts separated by `;`, hints
(`/*+ … */`), the lot.

**However**, what the composer *does* emit (in every concrete
pipeline in §A) is constrained to:

| Feature | Used in concrete pipelines? | Notes |
|---|---|---|
| `SELECT` + column projection | Yes — every pipeline | Trivial in Go |
| `WHERE` (scalar predicates) | Yes — #1, #2 | Maps to `filter` operator |
| `CAST(... AS ...)` | Yes — #1, #2, #4, #5 | Maps to a typed `cast` step |
| `CONCAT`, `ABS`, `NULLIF` | Yes — #1, #2, #4 | Scalar function call (extensible in `pipeline-expression`) |
| `SUM`, `COUNT`, `COUNT DISTINCT`, `AVG`, `STDDEV` | Yes — #3, #4 | **Requires the `aggregate` operator.** Today only stubbed (§D). |
| `GROUP BY` | Yes — #3 | Inseparable from `aggregate` above |
| `WITH … AS (…)` (CTE) | Yes — #4 | Re-express as a separate DAG node materialising the stats, consumed downstream |
| `CROSS JOIN` | Yes — #4 | The existing FASTER `join` operator supports `cross` (verified in §D) |
| `CREATE OR REPLACE TEMPORARY VIEW … USING csv` | Yes — #5 | Spark DataSource API — not portable. Replace with an upstream ingest step into `connector-management-service` followed by a regular Iceberg read |
| Window functions (`OVER`) | **No** | Composer *could* emit them; no concrete pipeline does |
| `MERGE INTO`, `INSERT INTO`, `UPDATE` | **No** | Iceberg snapshot semantics handled by `writeTo(...).createOrReplace()` only |
| Scala / Java / Python UDFs | **No** | Not registered anywhere in the repo |
| Spark hints, `/*+ */` | **No** | None observed |
| Multi-statement scripts | Yes — #5 only | Two statements: `CREATE TEMP VIEW; SELECT …`. Replaced by Phase 0 ingest decomposition |

**Conclusion:** the *theoretical* SQL surface is unbounded; the
*actual* SQL surface is bounded and small. ADR-0045's break of the
free-form SQL contract is therefore non-disruptive **provided**
authoring-time validation rejects SQL features outside the supported
operator vocabulary (this becomes a Phase C requirement, not a
Phase A blocker).

## §C. Node types and declarative transformation catalog

### Node logic kinds (from [`runners/runners.go:81-85`](../../services/pipeline-build-service/internal/domain/runners/runners.go))

`SYNC`, `TRANSFORM`, `HEALTH_CHECK`, `ANALYTICAL`, `EXPORT`.

The Spark path (§A pipelines #1–#5) is used by `TRANSFORM`,
`ANALYTICAL`, and the EXTRACT-flavoured ingest in #5. `SYNC`,
`HEALTH_CHECK`, and `EXPORT` do not go through `spark-submit`.

### Transform catalog status (from [`transform_catalog.go`](../../services/pipeline-build-service/internal/handler/transform_catalog.go))

**Status = `available` (implemented in FASTER runtime):**

`select`, `drop`, `rename`, `cast`, `filter`, `normalize_columns`,
`join` (inner / left / right / outer / cross), `union`,
`haversine_distance`, `geo_intersection_join`, `geo_distance_join`,
`geo_nearest_neighbor_join`, `gpx_parse`, `python_transform`,
`llm_node`, `output_mapping`.

**Status = `planned` (in catalog UI but not executable):**

- `aggregate` (alias `group_by`) — declared at [`transform_catalog.go:528`](../../services/pipeline-build-service/internal/handler/transform_catalog.go) with `"planned"` + `"catalog_only"`. Verified.
- `formula`, `derive_column`, `normalize_units`, `sort`, `explode`,
  `json_extract`, `csv_parse`.

**Implication for ADR-0045:** the `aggregate` gap is already
explicitly tracked inside the build service as a known-missing
operator. Phase C does not invent a new abstraction — it promotes
`aggregate` from `planned` to `available`.

## §D. FASTER runtime vocabulary

The FASTER (lightweight) runtime is the surviving execution path
after ADR-0045 Phase C collapses DISTRIBUTED into it. Its current
operator surface (from
[`lightweight_runtime.go:197`](../../services/pipeline-build-service/internal/handler/lightweight_runtime.go)):

```go
case "input", "filter", "select", "drop", "rename", "passthrough",
     "output", "sql", "function", "gpx_parse":
    return true
```

| Operator | Available? | Where | Used in §A? | Gap for ADR-0045 Phase A/B? |
|---|---|---|---|---|
| `input` | Yes | `lightweight_runtime.go:217-221` | All pipelines | — |
| `filter` | Yes | `lightweight_runtime.go:361-386` | #1, #2 | — |
| `select` | Yes | `lightweight_runtime.go:387-411` | All | — |
| `drop` / `rename` / `cast` | Yes (inside `transform_stack`) | `runTransformStack` (501-557) | #1, #2, #4, #5 | — |
| `join` (inner / left / right / outer / **cross**) | Yes (as a sub-op of structured `sql` node) | `runJoin` (680-726) | #4 (via CROSS JOIN) | — |
| `union` | Yes | `runUnion` (728-774) | None observed | — |
| `function` (catalog call: haversine, gpx, …) | Yes | `runFunction` (558-574) | None observed | — |
| `gpx_parse` | Yes | top-level | None observed | — |
| **`aggregate` (GROUP BY / SUM / COUNT / AVG / STDDEV)** | **No** | `transform_catalog.go:528` declares it `planned/catalog_only` | **#3, #4** | **Required before Phase C ships #3, #4 on Go runtime** |
| **Window functions** (`OVER`) | **No** | — | None observed | Defer until a pipeline needs it |
| `csv_parse` (DataSource-equivalent) | No (`planned`) | — | **#5** | Side-step in Phase 0 by routing ingest through `connector-management-service` |
| `MERGE INTO` / upserts | No | — | None observed | Out of scope |
| Scala / Java UDFs | No | — | None observed | Out of scope |

**Important nuance on `join` and `union`.** They are not listed in
`Supports()` at the top level — they reach the runtime through the
structured-`sql` dispatcher (`runStructuredSQL` at
`lightweight_runtime.go:460-499`), which fans out to `runJoin` and
`runUnion`. So FASTER does support them, but only when authored as a
structured-SQL config, not as standalone node kinds. Phase C should
either (a) lift them to first-class node kinds in the catalog, or
(b) keep authoring them as sub-ops of a `sql` node. Either way the
runtime work is already done.

## §E. Bucket classification and Phase 0 gate

| Pipeline | Bucket | Why | Phase that re-platforms it |
|---|---|---|---|
| `online-retail-clean` | **(a)** | `select` + `filter` + `cast` + scalar fns. Today's FASTER vocabulary already covers it. | Phase C (mechanical rewrite of node config to `transform_stack`) |
| `online-retail-returns` | **(a)** | Same as above with `WHERE quantity < 0`. | Phase C |
| `online-retail-cust` | **(b)** | `GROUP BY customer_id` + `SUM` / `COUNT DISTINCT`. Needs `aggregate` operator (already a `planned` catalog entry). | Phase C, after `aggregate` is promoted to `available` |
| `online-retail-anomalies` | **(b)** | CTE that computes global stats + `CROSS JOIN`. Decomposes cleanly into: node-1 = aggregate(stats) → small Iceberg table → node-2 = stats × transactions via `join (cross)`. | Phase C, after `aggregate` |
| `online-retail-ingest` | **(b)** | Spark `CREATE TEMPORARY VIEW USING csv OPTIONS (…)` is the DataSource API. Replace by `connector-management-service` SYNC into raw Iceberg table + a regular Iceberg read in the new Go runtime. | Phase 0/A — the ingest move is independent of Phase C and can be staged first |
| `spark-smoke` | **(a)** | `SELECT 1`. Smoke-only; trivially re-expressible. | Phase C |
| Test fixtures (`SELECT * FROM trails`) | **(a)** | Pure passthrough. | Phase C |

**Bucket (c): empty.**

No pipeline depends on a Spark-exclusive runtime feature with no Go
analogue. The closest near-miss is the Spark CSV DataSource in
`online-retail-ingest`, but that is a Phase-0 architectural
re-routing (move CSV ingest out of the transform path entirely),
not a missing operator in the Go runtime.

**Scale gate.** Every `SparkApplication` CR in `infra/dev/` declares
`executor.instances: 1` (or `cores: 1, memory: ≤ 3g`). The only
`executor_instances > 1` value in the repo is in a unit test
(`distributed_runtime_test.go:36` — `instances: 3`), not a
production pipeline. **Single-pod Go execution does not block any
observed pipeline.** Operators must re-confirm this against
`pipeline_authoring.published_dag` in a live environment before
Phase C ships.

## §F. Gate decision and follow-ups

**Phase 0 gate: PASS.** ADR-0045 may proceed to Phase A.

**Pre-requisites that Phase A / B / C inherit:**

1. **Phase C must promote `aggregate` from `planned` → `available`** in
   `transform_catalog.go` and implement the corresponding evaluator
   in `lightweight_runtime.go` *before* re-platforming pipelines #3
   and #4. The catalog entry and form fields already exist
   ([`transform_catalog.go:528-540`](../../services/pipeline-build-service/internal/handler/transform_catalog.go));
   only the runtime path is missing.
2. **Phase 0/A should also re-route the CSV ingest** in
   `online-retail-ingest`: move the `s3a://…/online_retail.csv` →
   `online_retail_raw` Iceberg ingest into `connector-management-service`
   (or its closest sibling), and let pipeline #5 become a regular
   Iceberg read in the new Go runtime. This unblocks Phase C without
   waiting on a Go DataSource-API equivalent.
3. **Phase C must add an authoring-time SQL allowlist.** Because the
   composer's `--inline-sql` is a raw passthrough today, any pipeline
   created via the authoring UI could in principle ship SQL outside
   the supported operator surface. The new operator-plan endpoint
   (ADR-0045, Decision §3) implicitly enforces this — but the
   migration must reject any legacy node config carrying
   `sql`/`statement` strings that cannot be parsed into the operator
   vocabulary, with a clear `422 PIPELINE_SQL_NOT_PORTABLE` error
   and a pointer to this document.
4. **Live-environment re-confirmation.** Before Phase C cuts over,
   run a one-off audit script over `pipeline_authoring.published_dag`
   in production and update §A / §E with any pipeline this checkout
   cannot see. If a bucket (c) pipeline turns up, ADR-0045 returns to
   Proposed for amendment.

## §G. Status as of Phase C.6

ADR-0045 has shipped through Phase C in six sub-PRs. The status of
the originally-flagged pre-requisites:

| Pre-req | Status |
|---|---|
| 1. Promote `aggregate` to available | ✅ Phase C.3 (PR #70) — runtime path in `internal/handler/aggregate_runtime.go`; catalog entry now `available/lightweight_table`. |
| 2. Re-route CSV ingest via connector-management-service | ⏳ Still pending. `infra/dev/spark-ingest-online-retail.yaml` was deleted in Phase C.6 with a clear pointer; the actual CSV → Iceberg path needs a separate PR against `connector-management-service`. |
| 3. Authoring-time SQL allowlist | ✅ Phase C.4.b (PR #74) — `internal/planner.ErrFreeFormSQLNotPortable` is returned when any node config carries `sql`/`statement`, with a link to this document. |
| 4. Live-environment re-confirmation | ⏳ Tied to the smoke gate. The dev k3s cluster needs to be healthy for the YAMLs in `infra/dev/poc-pipeline-nodes.yaml` to be applied; that smoke is the final gate before Phase D removes the Scala module. |

Sub-PR map:

| Phase | PR | Scope |
|---|---|---|
| C.1 | #67 | `libs/pipeline-plan` — typed operator-plan schema |
| C.2 | #69 | `libs/pipeline-runtime` — interpreter |
| C.3 | #70 | Promote `aggregate` to `available` in the FASTER runtime |
| C.4.a | #73 | Dispatcher rename (`spark` → `dispatch`) + Job manifest |
| C.4.b | #74 | `internal/planner` composer (NodeConfig → Plan) |
| C.5 | #75 | `services/pipeline-runner` rewrite + Iceberg providers |
| C.6 | this PR | `infra/dev/poc-pipeline-nodes.yaml` migration to Job + ConfigMap shape |

The two PoC pipelines flagged in §E that remain deferred:

- `online-retail-anomalies` — needs CROSS JOIN, which `libs/pipeline-plan`
  v1 does not ship (the `join` operator is deferred to v2 per Phase 0
  §D and ADR-0045 § Migration plan / Phase C). When `join` lands the
  pipeline becomes a two-stage decomposition; for now there is no
  YAML in `infra/dev/` for it.
- `online-retail-ingest` — Spark DataSource API. The migration target
  is `connector-management-service`, not the runner; tracked
  separately, not in this ADR's scope.

## Appendix — verified citations

Every line reference in this document was confirmed by direct file
read at the cited line range on 2026-05-17:

- `services/pipeline-build-service/internal/handler/lightweight_runtime.go:195-203` (Supports list).
- `services/pipeline-build-service/internal/handler/transform_catalog.go:525-540` (aggregate planned/catalog_only).
- `services/pipeline-build-service/internal/handler/distributed_runtime.go:120-135` (firstNonEmpty(cfg.SQL, cfg.Statement)).
- `infra/dev/poc-pipeline-nodes.yaml:290-307` (WITH stats AS … CROSS JOIN stats).
- File / line ranges for §A pipelines #1–#6 were cross-checked against
  the YAML structure on the same date.

If any cited line range no longer matches due to subsequent edits,
this document is out of date and must be regenerated before the next
ADR-0045 phase ships.
