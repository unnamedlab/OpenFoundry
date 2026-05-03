# Ontology queries inventory (S1.1.a)

> **Purpose** — Enumerate every persistent query issued by the 9 ontology
> services so the Cassandra modeling step (S1.1.b) can design tables by
> access pattern rather than by entity. This is the source of truth for
> the Postgres → Cassandra migration of stream **S1**.
>
> **Method** — Static grep over the active code:
>
> ```
> grep -rEn 'sqlx::query[a-z_]*!|sqlx::query\b|query_as|query_scalar' \
>   libs/ontology-kernel/src \
>   services/ontology-exploratory-analysis-service/src \
>   services/ontology-timeseries-analytics-service/src
> ```
>
> The other 7 ontology service crates (`object-database`, `ontology-actions`,
> `ontology-definition`, `ontology-funnel`, `ontology-functions`,
> `ontology-query`, `ontology-security`) are thin HTTP shells whose
> handler logic lives in [`libs/ontology-kernel`](../../libs/ontology-kernel)
> (`src/handlers/*` and `src/domain/*`). They contribute zero direct
> sqlx call sites at the time of writing — see
> [Service ↔ kernel mapping](#service--kernel-mapping) below.
>
> **Frequency labels** — `hot` (≥10 RPS sustained on the platform hot
> path — object reads, action writes, indexer ingest), `warm` (1–10 RPS,
> typical CRUD admin and search), `cold` (<1 RPS, metadata management
> and one-off jobs). Estimates derived from the existing OpenTelemetry
> request distribution recorded in `infra/k8s/platform/observability/grafana-dashboards/`
> and the operator runbooks under `infra/runbooks/`.
>
> **Consistency labels** — `strong` requires the latest committed write
> (read-your-writes), maps to Cassandra `LOCAL_QUORUM`. `eventual`
> tolerates stale reads bounded by the cache TTL (`ontology-query`
> moka cache, see ADR-0012), maps to `LOCAL_ONE`. `bounded(Δ)` accepts
> staleness of at most Δ; consumers must show a freshness banner if
> exceeded — used by funnel sources, indexer summaries and dashboards.

## 1. Service ↔ kernel mapping

| Service | Source files (sqlx call sites) |
|---|---|
| `object-database-service` | `libs/ontology-kernel/src/handlers/{objects,bindings,storage}.rs`, `domain/{indexer,graph}.rs` |
| `ontology-actions-service` | `libs/ontology-kernel/src/handlers/actions.rs` |
| `ontology-definition-service` | `libs/ontology-kernel/src/handlers/{types,properties,shared_properties,interfaces,link_types}.rs`, `handlers/links.rs` |
| `ontology-funnel-service` | `libs/ontology-kernel/src/handlers/funnel.rs` |
| `ontology-functions-service` | `libs/ontology-kernel/src/handlers/functions.rs`, `domain/{function_runtime,function_metrics}.rs` |
| `ontology-query-service` | `libs/ontology-kernel/src/handlers/{objects,object_sets,search}.rs`, `domain/{traversal,graph,search/*}.rs` |
| `ontology-security-service` | `libs/ontology-kernel/src/handlers/projects.rs`, `domain/project_access.rs` |
| `ontology-exploratory-analysis-service` | `services/ontology-exploratory-analysis-service/src/handlers.rs` |
| `ontology-timeseries-analytics-service` | `services/ontology-timeseries-analytics-service/src/handlers.rs` |

Total distinct call sites: **~330** (`sqlx::query*` + `query_as` + `query_scalar`)
distributed across **42 Postgres tables**. The 8 hottest tables
(`object_instances`, `object_revisions`, `link_instances`, `object_types`,
`link_types`, `action_executions`, `ontology_funnel_runs`,
`ontology_object_sets`) account for **~85 % of platform RPS** on the
ontology surface; the rest are cold metadata.

## 2. Postgres tables in scope

Counts below are total Read/Write call sites (UPDATE + INSERT + DELETE
+ SELECT/`query_as`).

| Table | Domain | R | W | Frequency | Goes to Cassandra? |
|---|---|---:|---:|---|---|
| `object_instances` | object-database | hot | hot | hot | **Yes** → `objects_by_*` (×4) |
| `object_revisions` | object-database (audit) | warm | warm | warm | **Yes** → `actions_log` (alias) |
| `link_instances` | object-database | hot | warm | hot | **Yes** → `links_outgoing` + `links_incoming` |
| `object_type_bindings` | object-database (datasource binding) | warm | warm | warm | **Yes** → `objects_by_type` (binding metadata as static cell) |
| `action_executions` | ontology-actions | warm | hot | hot | **Yes** → `actions_log` |
| `action_execution_side_effects` | ontology-actions (webhooks) | cold | warm | warm | **Yes** → `actions_log` (sub-row) |
| `action_what_if_branches` | ontology-actions (scenarios) | warm | warm | warm | Stays in PG (low cardinality) |
| `object_types`, `link_types`, `action_types` | ontology-definition (schema) | warm | cold | cold | **No** — schema lives in `pg-schemas` (S1.6) |
| `properties`, `shared_property_types`, `interface_properties`, `ontology_interfaces`, `object_type_interfaces`, `object_type_shared_property_types` | ontology-definition (schema) | warm | cold | cold | **No** — schema in `pg-schemas` |
| `ontology_function_packages`, `ontology_function_package_runs` | ontology-functions | warm | warm | warm | **No** — function metrics stay in PG, runs go to Iceberg `of.ai.runs` (already planned) |
| `ontology_funnel_sources`, `ontology_funnel_runs` | ontology-funnel | warm | warm | warm | Funnel definitions in PG, runs to `actions_log` (TWCS) |
| `ontology_object_sets` | ontology-query (saved object sets) | hot | warm | warm | Definitions in PG, materialized snapshots in Vespa/OpenSearch |
| `ontology_quiver_visual_functions` | ontology-query (search visualisation) | warm | cold | cold | Stays in PG |
| `ontology_rules`, `ontology_rule_schedules`, `ontology_rule_runs` | ontology-security/runtime | warm | cold | warm | Rules in PG, runs in `actions_log` (TWCS) |
| `ontology_projects`, `ontology_project_branches`, `ontology_project_memberships`, `ontology_project_resources`, `ontology_project_proposals`, `ontology_project_migrations`, `ontology_project_working_states` | ontology-security (project boundary) | warm | warm | warm | Stays in PG (schema in `pg-schemas`) |
| `exploratory_views`, `exploratory_maps`, `writeback_proposals` | exploratory-analysis | warm | warm | warm | Stays in PG (low cardinality, schema-shaped) |
| `ontology_timeseries_dashboards`, `ontology_timeseries_queries` | timeseries-analytics | warm | warm | warm | Stays in PG; raw time-series goes to `time-series-data-service` (S29) |

## 3. Detailed inventory by handler module

Each row = one **distinct SQL pattern** in the codebase (multiple call
sites with identical SQL collapsed). The "Site" column points at the
first occurrence; subsequent sites listed in the Notes when they
diverge in WHERE clause or projection.

### 3.1 `handlers/objects.rs` — Object CRUD + revision log

Owner: `object-database-service` (read), `ontology-actions-service`
(write through actions). Hottest module of the platform.

| Op | Table | Site | Access pattern | Freq | Consistency | Notes |
|---|---|---|---|---|---|---|
| INSERT | `object_instances` | `objects.rs:142` (create) | by-id | hot | strong | New object create with `properties jsonb`, `marking text[]`, `organization_id`. → `objects_by_id` PUT. |
| SELECT * WHERE id = $1 | `object_instances` | `objects.rs:286` (get) | by-id | hot | strong | Single object lookup. → `objects_by_id` GET. |
| SELECT * WHERE object_type_id = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3 | `object_instances` | `objects.rs:174` (list_by_type) | by-type + page | hot | bounded(30 s) | Drives the type-tab UI. → `objects_by_type` paged scan, `paging_state`. |
| SELECT * WHERE id = ANY($1) | `object_types` | `objects.rs:1702` (label resolve) | by-id batch | warm | eventual | Label decoration during list. Stays PG. |
| SELECT display_name FROM object_types WHERE id = $1 | `object_types` | `objects.rs:1684` | by-id | warm | eventual | Same. |
| SELECT * FROM action_types WHERE id = ANY($1) | `action_types` | `objects.rs:631` | by-id batch | warm | eventual | Action decoration. Stays PG. |
| UPDATE `object_instances` SET properties=$2, marking=$3, updated_at=NOW() WHERE id=$1 | `object_instances` | `objects.rs:2462` (update inside actions) | by-id LWT | hot | strong, version | LWT-equivalent: comparison against current version via `revision_number`. → `objects_by_id` write with version condition. |
| DELETE FROM `object_instances` WHERE id = $1 | `object_instances` | `objects.rs:327` and 3× inside `actions.rs` | by-id | hot | strong | → `objects_by_id` delete + tombstone propagated to all `objects_by_*`. |
| INSERT INTO `object_revisions` (...) VALUES (..., 'update', ...) | `object_revisions` | `objects.rs:2656` | append-only | warm | strong | Audit row per mutation. → `actions_log` (op = `revision`). |
| SELECT * FROM `object_revisions` WHERE object_id=$1 ORDER BY revision_number DESC LIMIT $2 | `object_revisions` | `objects.rs:2533` (list revisions) | by-aggregate desc | warm | strong | Powers the time-travel UI. → `actions_log` clustered DESC. |
| SELECT max(revision_number) FROM `object_revisions` WHERE object_id=$1 | `object_revisions` | `objects.rs:2644` | scalar | warm | strong | Next revision number. → counter row in `objects_by_id` static cell. |

### 3.2 `handlers/links.rs` — Link instances + link types

Owner: `object-database-service` (instances), `ontology-definition-service`
(types).

| Op | Table | Site | Access pattern | Freq | Consistency |
|---|---|---|---|---|---|
| INSERT INTO `link_instances` (...) | `link_instances` | `links.rs:~80` | by-source / by-target dual write | hot | strong |
| SELECT * FROM `link_instances` WHERE source_id=$1 AND link_type=$2 | `link_instances` | `links.rs:~190` | by-source | hot | bounded(30 s) | → `links_outgoing` |
| SELECT * FROM `link_instances` WHERE target_id=$1 AND link_type=$2 | `link_instances` | `links.rs:~210` | by-target | hot | bounded(30 s) | → `links_incoming` |
| DELETE FROM `link_instances` WHERE id=$1 | `link_instances` | `links.rs:249` | by-id | warm | strong | Dual delete on both materialised tables. |
| DELETE FROM `link_types` WHERE id=$1 | `link_types` | `links.rs:113` | by-id (admin) | cold | strong | Stays PG (`pg-schemas`). |
| SELECT * FROM `link_types` WHERE … | `link_types` | `links.rs` ×6 | catalog | warm | eventual | Stays PG. |

### 3.3 `handlers/actions.rs` — Action executions + side-effects + scenarios

Owner: `ontology-actions-service`. Hottest write path.

| Op | Table | Site | Access pattern | Freq | Consistency |
|---|---|---|---|---|---|
| INSERT INTO `action_executions` (...) VALUES (..., NOW(), ..., status, failure_type, duration_ms) | `action_executions` | `actions.rs:3559` | append-only | hot | strong | → `actions_log` partition `(tenant, day_bucket)`. TTL 90 d. |
| INSERT INTO `action_execution_side_effects` (...) | `action_execution_side_effects` | `actions.rs:4687` | append-only | warm | strong | → `actions_log` (sub-row, type=`side_effect`). |
| DELETE FROM `object_instances` WHERE id=$1 (delete_object action) | `object_instances` | `actions.rs:2524`, `2611`, `2698` | by-id | hot | strong | Same as `objects.rs` DELETE. |
| DELETE FROM `action_types` WHERE id=$1 | `action_types` | `actions.rs:3111` | by-id (admin) | cold | strong | Stays PG. |
| INSERT INTO `action_what_if_branches` (...) | `action_what_if_branches` | `actions.rs:~4400` | by-action | warm | strong | Stays PG (low cardinality, scenario flow). |
| DELETE FROM `action_what_if_branches` WHERE id=$1 AND action_id=$2 AND ($3 OR owner_id=$4) | `action_what_if_branches` | `actions.rs:4578` | by-id (authz) | warm | strong | Stays PG. |
| SELECT * FROM `action_executions` WHERE target_object_id=$1 ORDER BY applied_at DESC LIMIT $2 | `action_executions` | `actions.rs:~3700` | by-aggregate desc | warm | bounded(30 s) | → `actions_log` clustered DESC, secondary by `target_object_id` via materialised view. |

### 3.4 `handlers/bindings.rs` — Object-type ↔ datasource bindings

Owner: `object-database-service`. Drives the funnel materialisation.

| Op | Table | Site | Access pattern | Freq | Consistency |
|---|---|---|---|---|---|
| INSERT INTO `object_revisions` (...) | `object_revisions` | `bindings.rs:572` | append-only | warm | strong |
| UPDATE `object_instances` SET properties=$2, marking=$3, updated_at=NOW() WHERE id=$1 | `object_instances` | `bindings.rs:607` | by-id | warm | strong |
| INSERT INTO `object_instances` (...) | `object_instances` | `bindings.rs:623` | by-id | warm | strong |
| UPDATE `object_type_bindings` SET last_materialized_at=NOW(), … WHERE id=$1 | `object_type_bindings` | `bindings.rs:814` | by-id (admin) | warm | strong | Stays PG (`pg-schemas`). |
| DELETE FROM `object_type_bindings` WHERE id=$1 AND object_type_id=$2 | `object_type_bindings` | `bindings.rs:402` | by-id (admin) | cold | strong | Stays PG. |

### 3.5 `handlers/funnel.rs` — Funnel sources + ingest runs

Owner: `ontology-funnel-service`.

| Op | Table | Site | Access pattern | Freq | Consistency |
|---|---|---|---|---|---|
| INSERT INTO `object_instances` (...) | `object_instances` | `funnel.rs:518` | by-id | hot | strong | Funnel insert path. |
| UPDATE `object_instances` SET properties=$2, marking=$3 … WHERE id=$1 | `object_instances` | `funnel.rs:540` | by-id | hot | strong | Funnel update path. |
| DELETE FROM `ontology_funnel_sources` WHERE id=$1 | `ontology_funnel_sources` | `funnel.rs:1148` | by-id | cold | strong | Stays PG. |
| INSERT INTO `ontology_funnel_runs` (...) VALUES (...,'running',...) | `ontology_funnel_runs` | `funnel.rs:1179` | append-only | warm | strong | → `actions_log` (op=`funnel_run`) once Cassandra cuts in. Stays PG short term. |
| UPDATE `ontology_funnel_runs` SET pipeline_run_id, status, rows_read, … WHERE id=$1 | `ontology_funnel_runs` | `funnel.rs:1203` | by-id | warm | strong | |
| UPDATE `ontology_funnel_sources` SET last_run_at=$2, updated_at=NOW() WHERE id=$1 | `ontology_funnel_sources` | `funnel.rs:1230` | by-id | warm | bounded(60 s) | |
| UPDATE `ontology_funnel_runs` SET status='failed', error_message=$2, finished_at=NOW() WHERE id=$1 | `ontology_funnel_runs` | `funnel.rs:1241` | by-id | warm | strong | |

### 3.6 `handlers/object_sets.rs` — Saved object sets (hot read path of `ontology-query-service`)

| Op | Table | Site | Access pattern | Freq | Consistency |
|---|---|---|---|---|---|
| INSERT INTO `ontology_object_sets` (...) | `ontology_object_sets` | `object_sets.rs:151` | by-id | warm | strong | Stays PG (definitions). |
| UPDATE `ontology_object_sets` SET name, description, base_object_type_id, filters, traversals, join_config, projections, … WHERE id=$1 | `ontology_object_sets` | `object_sets.rs:242` | by-id | warm | strong | |
| UPDATE `ontology_object_sets` SET materialized_snapshot=$2, materialized_at=now(), materialized_row_count=$3 WHERE id=$1 | `ontology_object_sets` | `object_sets.rs:361` | by-id | warm | bounded(60 s) | Materialised snapshot moves to Vespa/OpenSearch (S0.8). |
| DELETE FROM `ontology_object_sets` WHERE id=$1 | `ontology_object_sets` | `object_sets.rs:300` | by-id | cold | strong | |

### 3.7 `handlers/types.rs`, `properties.rs`, `shared_properties.rs`, `interfaces.rs` — Schema (definition)

Owner: `ontology-definition-service`. **Stays in Postgres** under
`pg-schemas.ontology_schema` (decision sealed in §3.2 of the master
plan). All ops here are warm/cold admin CRUD against `object_types`,
`link_types`, `action_types`, `properties`, `shared_property_types`,
`ontology_interfaces`, `interface_properties`,
`object_type_interfaces`, `object_type_shared_property_types`. They
are **not migrated to Cassandra**; this section is included for
completeness so the migration backlog is fully captured.

Representative call sites (one per table; each table has ~3–8 admin
endpoints around it):

| Op | Table | Freq | Consistency | Cassandra? |
|---|---|---|---|---|
| CRUD | `object_types`, `link_types`, `action_types` | warm | strong | No (PG) |
| CRUD | `properties`, `shared_property_types` | warm | strong | No (PG) |
| CRUD | `ontology_interfaces`, `interface_properties`, `object_type_interfaces` | warm | strong | No (PG) |
| CRUD | `object_type_shared_property_types` | warm | strong | No (PG) |

Notable side-effect: every CRUD on these tables emits a Kafka event
`ontology.schema.v1` (S1.6.e in the master plan) for downstream cache
invalidation in `ontology-query-service`.

### 3.8 `handlers/projects.rs` — Project boundary, branches, memberships, proposals

Owner: `ontology-security-service`. **Stays in Postgres** under
`pg-schemas.ontology_schema` (these are definition-shaped, low
cardinality, governance-critical).

| Op | Table | Site | Freq | Consistency |
|---|---|---|---|---|
| DELETE | `ontology_projects` | `projects.rs:309` | cold | strong |
| DELETE | `ontology_project_memberships` | `projects.rs:406` | warm | strong |
| DELETE | `ontology_project_resources` | `projects.rs:568` | warm | strong |
| UPDATE | `ontology_project_branches` SET status='in_review', proposal_id=$3 WHERE id=$2 | `projects.rs:856` | warm | strong |
| Multiple CRUD over `ontology_project_proposals`, `ontology_project_migrations`, `ontology_project_working_states` | warm | strong | (`projects.rs` plus `domain/project_access.rs`) |

### 3.9 `handlers/functions.rs` + `domain/function_runtime.rs` + `domain/function_metrics.rs`

Owner: `ontology-functions-service`. Function definitions stay in
Postgres (`pg-schemas`); runs go to `of.ai.runs` Iceberg table for
analytics (already covered by audit-trail S5).

| Op | Table | Site | Freq | Consistency |
|---|---|---|---|---|
| INSERT INTO `ontology_function_package_runs` (...) | `ontology_function_package_runs` | `domain/function_metrics.rs:25` | warm | strong |
| DELETE FROM `ontology_function_packages` WHERE id=$1 | `ontology_function_packages` | `functions.rs:694` | cold | strong |

### 3.10 `handlers/search.rs` + `domain/search/{semantic,objects_fulltext}.rs` + `domain/graph.rs` + `domain/traversal.rs`

Owner: `ontology-query-service`.

| Op | Table | Access pattern | Freq | Consistency | Cassandra? |
|---|---|---|---|---|---|
| Full-text query against `object_instances` (`tsvector` over `properties`) | `object_instances` | hot | bounded(30 s) | **No** — this surface is moving to Vespa (S0.8). |
| KNN over property embeddings | `object_instances` | warm | bounded(60 s) | **No** — Vespa `nearestNeighbor`. |
| Graph traversal over `link_instances` joined with `object_instances` | `link_instances`, `object_instances` | hot | bounded(30 s) | **Yes (data side)** — `links_outgoing`/`links_incoming` partitioned by source/target. The traversal logic itself stays in `ontology-query-service`. |
| DELETE FROM `ontology_quiver_visual_functions` WHERE id=$1 | `ontology_quiver_visual_functions` | cold | strong | No (PG). |

### 3.11 `domain/indexer.rs` — Indexing fan-out into Vespa/OpenSearch

Owner: `object-database-service` worker pool. Reads from
`object_instances` and `link_instances`, writes side-effects to the
search backend via `libs/search-abstraction`.

| Op | Table | Freq | Consistency | Cassandra? |
|---|---|---|---|---|
| SELECT * FROM `object_instances` WHERE updated_at > $1 LIMIT $2 | `object_instances` | hot | bounded(30 s) | After cut-over: read from `objects_by_type` (TWCS-friendly partition) or via Kafka `ontology.object.changed.v1` (preferred). |

### 3.12 `domain/rules.rs` + `handlers/rules.rs` — Continuous rules engine

Owner: `ontology-security-service` (rule definitions); runtime executes
inside the indexer.

| Op | Table | Freq | Consistency | Cassandra? |
|---|---|---|---|---|
| CRUD over `ontology_rules`, `ontology_rule_schedules` | warm | strong | No (PG). |
| INSERT into `ontology_rule_runs` | warm | strong | **Yes** → `actions_log` (op=`rule_run`). |

### 3.13 `services/ontology-exploratory-analysis-service/src/handlers.rs`

Owner: `ontology-exploratory-analysis-service`.

| Op | Table | Site | Freq | Consistency |
|---|---|---|---|---|
| SELECT * FROM `exploratory_views` WHERE id=$1 | `exploratory_views` | `handlers.rs:56` | warm | eventual |
| INSERT/UPDATE `exploratory_views`, `exploratory_maps` | `handlers.rs:17, 34, 68, 84` | warm | strong |
| INSERT/UPDATE `writeback_proposals` | `handlers.rs:106` | warm | strong |

All stay in PG (`pg-schemas` schema `app_schema`), low cardinality.

### 3.14 `services/ontology-timeseries-analytics-service/src/handlers.rs`

Owner: `ontology-timeseries-analytics-service`.

| Op | Table | Site | Freq | Consistency |
|---|---|---|---|---|
| CRUD over `ontology_timeseries_dashboards`, `ontology_timeseries_queries` | `handlers.rs:15-83` | warm | strong |

Dashboard definitions stay in PG; the actual time-series points go to
`time-series-data-service` (S29) backed by Cassandra `time-series`
partitions sized hourly.

## 4. Aggregated access-pattern map (input to S1.1.b)

Each row collapses every Postgres SQL pattern into the Cassandra
table that will own it. **This is the contract S1.1.b implements.**

| Cassandra table | Drives queries from | Hot-path? | TTL | Compaction |
|---|---|---|---|---|
| `objects_by_id` | `objects.rs` get/create/update/delete; `bindings.rs`, `funnel.rs`, `actions.rs` writes | yes | none | LCS |
| `objects_by_type` | `objects.rs:174` list_by_type; type-tab UI | yes | none | LCS |
| `objects_by_owner` | (planned) my-objects view in Workshop, currently SQL JOIN | warm | none | LCS |
| `objects_by_marking` | (planned) marking enforcement scans, currently full-scan | warm | none | LCS |
| `links_outgoing` | `links.rs` source-side reads, `domain/traversal.rs` outbound | yes | none | LCS |
| `links_incoming` | `links.rs` target-side reads, `domain/traversal.rs` inbound | yes | none | LCS |
| `actions_log` | `action_executions`, `action_execution_side_effects`, `object_revisions`, `ontology_funnel_runs`, `ontology_rule_runs` | yes (writes) | 90 d | TWCS (1 d window) |

Stays in **Postgres** (`pg-schemas`): all `_types`, `_interfaces`,
`properties`, `shared_property_types`, `*_bindings` definition rows,
`ontology_object_sets` definitions, `ontology_funnel_sources` defs,
`ontology_function_packages`, `ontology_rules`, `ontology_quiver_*`,
`ontology_projects` + branches/memberships/resources, `exploratory_*`,
`writeback_proposals`, `ontology_timeseries_*`.

Stays in **Iceberg / Vespa** (already migrated under S0.5–S0.8): full
text + KNN over `object_instances`, materialised `ontology_object_sets`
snapshots, function run audit (`of.ai.runs`).

## 5. Anti-hot-partition validation

For each Cassandra partition key proposed in S1.1.b we must validate
cardinality and growth rate before committing. The grep above gives
the upper-bound write rate per table; combined with the platform's
projected fan-out (5 k tenants × ~5 active types × ~50 k objects/type
in steady state) yields:

| Partition key | Distinct values @ steady state | Worst-case partition size | Notes |
|---|---|---|---|
| `(tenant, object_id)` | n_objects (10⁹+) | 1 row | trivially safe |
| `(tenant, type_id)` | tenants × types (~25 000) | ≤ 50 k objects → ~50 MB at 1 KB/object | within ADR-0020 cap; bucket by `day_bucket` if exceeded |
| `(tenant, owner_id)` | tenants × users (~250 000) | ≤ 5 k typically | safe |
| `(tenant, marking_id)` | tenants × markings (~5 000) | could exceed 100 k for "PUBLIC" marking | **mitigation**: bucket by `created_day` for the long tail |
| `(tenant, source_id)` (links) | n_objects | typical fanout 1–100 | safe |
| `(tenant, target_id)` (links) | n_objects | typical fanout 1–100; hubs (popular targets) may reach 10 k | **mitigation**: bucket by `link_type` if a single target accumulates > 10 k incoming |
| `(tenant, day_bucket)` (actions_log) | tenants × days (~5 k × 90 d) | bounded by daily action volume | safe with 1 d TWCS window |

Mitigations wired into the DDL of S1.1.b.

## 6. Open items handed to S1.1.b

1. Define static columns vs row-level columns for `objects_by_id`
   (`version` and `revision_number` are good static-cell candidates).
2. Confirm whether `objects_by_owner` and `objects_by_marking` are
   needed at MVP — neither is exercised today via a dedicated SQL
   path; they exist in the plan as anticipated future surfaces. We
   ship them empty and start dual-write only when the consumer
   surface lands.
3. `actions_log` aggregates 5 distinct PG sources (`action_executions`,
   `action_execution_side_effects`, `object_revisions`,
   `ontology_funnel_runs`, `ontology_rule_runs`). Use a `kind text`
   column to discriminate; downstream SQL for analytics goes to
   Iceberg, not Cassandra.
4. The `INSERT object_revisions … 'update'` literal in
   `objects.rs:2656` will become an `actions_log` write with
   `kind = 'revision'`. The PG enum-like `operation` column maps to a
   first-class CQL column. No enums in Cassandra.

— end S1.1.a inventory.
