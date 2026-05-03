# `oauth-integration-service` Postgres migrations

This folder is reserved for **long-lived OAuth configuration** only:

- registered applications
- inbound OAuth clients
- external integrations
- application credentials / client-secret metadata

Ephemeral OAuth runtime state does **not** belong in Postgres:

- `oauth_pending_auth`
- `oauth_token_exchange`

Those tables are Cassandra-owned under `auth_runtime.*` via
[`PendingAuthAdapter`](../src/pending_auth_cassandra.rs). Archived
Postgres DDL, cleanup notes and the staged DROP script live in
[`docs/architecture/legacy-migrations/oauth-integration-service/`](../../../docs/architecture/legacy-migrations/oauth-integration-service/).

Do not add new pending-auth, token-exchange or PKCE runtime tables to
this migration set.
