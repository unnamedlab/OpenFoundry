# CQL schema — `ontology-actions-service`

> **Layout**
>
> ```
> cql/
>   actions_log/
>     000_keyspace.cql
>     001_actions_log.cql
>     002_actions_by_object.cql
> ```
>
> Files are numbered to define **apply order**, consumed by the
> [`cql_migrate!`](../../../../libs/cassandra-kernel/src/migrate.rs)
> macro.
>
> All statements are idempotent (`IF NOT EXISTS`).
>
> See [`docs/architecture/ontology-cassandra-tables.md`](../../../docs/architecture/ontology-cassandra-tables.md)
> §7 for the design rationale.
