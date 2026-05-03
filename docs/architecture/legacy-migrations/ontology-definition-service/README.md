# Archived migrations — `ontology-definition-service`

These 17 DDL files used to live at
`services/ontology-definition-service/migrations/`. Together they
capture both the declarative schema-of-types surface and several
legacy runtime/governance tables that historically lived beside it.

## Why archived

Per [migration-plan §S1.6](../../migration-plan-cassandra-foundry-parity.md):

* Schema-of-types is **declarative**. It stays in Postgres but moves
  to the consolidated `pg-schemas` cluster, schema `ontology_schema`.
* The declarative subset is **collapsed** into a single
  idempotent script applied via pre-upgrade Helm jobs:
  [`services/ontology-definition-service/migrations-pg/0001_ontology_schema_consolidated.sql`](../../../../services/ontology-definition-service/migrations-pg/0001_ontology_schema_consolidated.sql).
* Runtime legacy tables such as `object_instances`, `link_instances`
  and `*_runs` stay archived here until their runtime owners complete
  the Cassandra / dedicated-service cut-over.
* Every change to a definition emits an `ontology.schema.v1` event on
  JetStream so SDK generation, the data-asset catalog and the search
  indexer can refresh their caches without polling Postgres.

These files are kept here verbatim as:

1. The canonical source for the data-migration tooling that backfills
   `pg-schemas.ontology_schema` from the legacy single-schema cluster.
2. Reference documentation for incident response that needs to reason
   about the historical schema-evolution layout.

Do **not** re-introduce them under `services/.../migrations/`. New
schema changes go into a fresh, **numbered** file under
`services/ontology-definition-service/migrations-pg/` (e.g.
`0002_<description>.sql`) and ship through the Helm `pre-upgrade` job.
