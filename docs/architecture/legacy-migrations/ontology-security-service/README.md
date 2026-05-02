# Archived migrations — `ontology-security-service`

These DDL files used to live at
`services/ontology-security-service/migrations/`. They define the
**compiled-policy-bundle** subsystem that drives security-aware query
pushdown across the platform.

## Tables

* `policy_bundle` — versioned snapshot of all access rules that apply
  to a given (workspace, project, cell) scope. Re-compiled whenever
  permissions change.
* `policy_visibility_projection` — pre-expanded per-object visibility
  rows used by `ontology-query-service` to apply marking filters
  without a synchronous call to the security service.

## S1.7 split (hot vs declarative)

* **Declarative → `pg-schemas.ontology_schema`**: `policy_bundle`
  definitions and version history (low-cardinality, high-read,
  rebuilt rarely; sits next to the type system because bundles are
  derived from type-level rules).
* **Hot → Cassandra**: `policy_visibility_projection` is the
  per-object expansion. At platform scale (`O~10⁹` objects) this is
  a denormalisation hot-path keyed by `(scope_id, object_id)`. Future
  keyspace `ontology_security.visibility_by_object`
  (PK = `(scope_id, object_type_id), object_id`).

The split is documented in
[`docs/architecture/migration-plan-cassandra-foundry-parity.md` §S1.7](../../migration-plan-cassandra-foundry-parity.md).

## Why archived

* Re-compile of bundles becomes idempotent on the consolidated
  `pg-schemas` cluster; the projection move to Cassandra is per-S1.7
  the dominant scaling lever for query planning latency.
* Service binary no longer applies `sqlx::migrate!`; the consolidated
  schema apply for `pg-schemas` is the canonical mechanism.

These files are kept verbatim as the canonical source for the
data-migration tooling that backfills `ontology_security.visibility_*`
and as schema reference for incident response.

Do **not** re-introduce them under `services/.../migrations/`.
