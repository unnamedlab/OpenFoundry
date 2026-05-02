# Archived migrations — `ontology-query-service`

These DDL files used to live at
`services/ontology-query-service/migrations/`. They define the legacy
PostgreSQL **read projections** that backed the query API
(`obj_current_projection`, `link_adjacency_projection`,
`search_document_projection`, `knn_vector_projection`,
`object_view_projection`).

## Why archived

Per [migration-plan §S1.5](../../migration-plan-cassandra-foundry-parity.md):

* All hot-path reads now go through Cassandra
  (`ontology_objects.*`, `ontology_indexes.*`) fronted by an
  in-process moka cache (S1.5.a) and invalidated via the
  `ontology.write.v1` event bus (S1.5.b).
* The vector / lexical projections move to the search abstraction
  (`libs/search-abstraction`, ADR-0024) — Vespa or OpenSearch
  depending on `SEARCH_BACKEND`.
* `ontology-query-service` no longer applies `sqlx::migrate!` and
  does not depend on `sqlx`.

These files are kept here verbatim as the **canonical source for
the data-migration tooling** (S1.7) that backfills the new keyspaces
from the legacy projections, and as the reference schema for
incident response that needs to reason about the historical layout.

Do **not** re-introduce them under `services/.../migrations/`.
