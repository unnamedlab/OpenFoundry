# Archived migrations — `ontology-actions-service`

These DDL files used to live at
`services/ontology-actions-service/migrations/` and were applied at
service startup via `sqlx::migrate!`. They define the legacy
PostgreSQL tables that backed the Action types feature
(`action_types`, `action_executions`, `action_execution_side_effects`,
`action_log`, …).

## Why archived

Per [migration-plan §S1.4.d](../../migration-plan-cassandra-foundry-parity.md):

* All hot, mutable Action state moves to Cassandra
  (`actions_log` keyspace, S1.1.b).
* The Action *type* declarations migrate to the consolidated
  `pg-schemas.ontology_schema` cluster in S1.6.
* The service binary no longer runs `sqlx::migrate!` — its only
  remaining PostgreSQL dependency is the `outbox.events` table,
  whose DDL is owned by `libs/outbox/migrations/`.

These files are kept here verbatim as the **canonical source for the
data-migration tooling** (S1.7) that rewrites the legacy rows into
the new keyspaces. They are also the reference schema for any
incident response that needs to reason about the historical layout.

Do **not** re-introduce them under `services/.../migrations/`. Any
`sqlx::query!` site that still relies on these tables is a known
gap tracked under the per-handler S1.4.b follow-ups in the migration
plan.
