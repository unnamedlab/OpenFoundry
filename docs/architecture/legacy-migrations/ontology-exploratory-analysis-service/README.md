# Archived migrations — `ontology-exploratory-analysis-service`

These DDL files used to live at
`services/ontology-exploratory-analysis-service/migrations/`. They
define the visual-exploration plane: saved views, map widgets and
proposed writebacks awaiting curation.

## Tables

* `exploratory_views` — saved view definitions (object type + filter
  spec + layout). Definition-shaped; rarely written.
* `exploratory_maps` — saved map widget configurations bound to a
  view. Definition-shaped.
* `writeback_proposals` — proposed in-flight mutations awaiting
  approval before being applied via `ontology-actions-service`.
  Operational, append-mostly.

## S1.7 split (hot vs declarative)

* **Declarative → `pg-schemas.ontology_schema`**: `exploratory_views`,
  `exploratory_maps`. Both are low-cardinality user artefacts.
* **Hot → Cassandra `actions_log` keyspace** (already provisioned in
  S1.1.b): `writeback_proposals` becomes a queue-shaped CF keyed by
  `(tenant_id, status), proposed_at DESC, proposal_id`. The
  approve/reject path then routes through
  `ontology-actions-service`'s writeback helper.

## Why archived

The service shell was substrate-only; the per-handler migration to
`Arc<dyn ObjectStore>` + writeback queue lands handler-by-handler in
follow-ups, mirroring the S1.4.b / S1.5.f deferrals.
