# CQL schema — `object-database-service`

This directory holds the **per-table CQL DDL** that
`object-database-service` is the source-of-truth owner of.

> **Layout**
>
> ```
> cql/
>   ontology_objects/
>     000_keyspace.cql
>     001_objects_by_id.cql
>     002_objects_by_type.cql
>     003_objects_by_owner.cql
>     004_objects_by_marking.cql
>   ontology_indexes/
>     000_keyspace.cql
>     001_links_outgoing.cql
>     002_links_incoming.cql
> ```
>
> Files are numbered to define **apply order**. The numeric prefix
> (`NNN_`) is the migration version consumed by the
> [`cql_migrate!`](../../../../libs/cassandra-kernel/src/migrate.rs)
> macro from `libs/cassandra-kernel`.
>
> All statements are **idempotent** (`IF NOT EXISTS`) — Cassandra has
> no reversible migrations; rollback is "create the inverse statement
> in the next file".
>
> See [`docs/architecture/ontology-cassandra-tables.md`](../../../docs/architecture/ontology-cassandra-tables.md)
> for the design rationale and access patterns.
