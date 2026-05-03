# `identity-federation-service` Postgres migrations

Only the **control-plane** Postgres surface belongs in this folder:
users, roles/permissions/policies, MFA metadata and other long-lived
identity configuration.

Runtime auth state no longer belongs in Postgres:

- `scoped_sessions`
- `refresh_tokens`
- legacy OAuth state tables

Those paths are Cassandra-owned under `auth_runtime.*` via
[`SessionsAdapter`](../src/sessions_cassandra.rs). Cleanup and staged
DROP scripts, plus the archived Postgres DDL they replace, live in
[`docs/architecture/legacy-migrations/identity-federation-service/`](../../../docs/architecture/legacy-migrations/identity-federation-service/).

Do not add new ephemeral session/token tables to this migration set.
