# Archived `oauth-integration-service` runtime migrations

This folder tracks the **legacy Postgres runtime surface** that should
no longer exist for OAuth pending-auth and token-exchange state.

## What changed

- **Authoritative store** for ephemeral OAuth runtime state is now the
  Cassandra keyspace `auth_runtime`:
  `oauth_pending_auth`, `oauth_token_exchange` (DDL in
  [`services/oauth-integration-service/src/pending_auth_cassandra.rs`](../../../services/oauth-integration-service/src/pending_auth_cassandra.rs),
  applied via `cassandra_kernel::migrate::apply`).
- The archived Postgres DDL that used to back those runtime tables now
  lives alongside this README in
  `20260427000000_oauth_runtime_state.sql`.
- The remaining Postgres surface for this service is limited to
  long-lived client/application/integration configuration from
  [`services/oauth-integration-service/migrations/`](../../../services/oauth-integration-service/migrations/).

## Cutover gate

The DROP migration (`drop_oauth_runtime_tables.sql.disabled`) **MUST
NOT** run before:

1. every caller of pending-auth/token-exchange runtime paths uses
   `PendingAuthAdapter`;
2. no production runtime writes land in Postgres for these tables;
3. at least 7 days of telemetry confirm Cassandra-only runtime state
   for OAuth flows;
4. the auth failover drill sign-off covering Cassandra-backed OAuth
   state remains valid.

## Pointers

- New Cassandra adapter:
  [`pending_auth_cassandra::PendingAuthAdapter`](../../../services/oauth-integration-service/src/pending_auth_cassandra.rs).
- Plan reference:
  [`migration-plan-cassandra-foundry-parity.md`](../../migration-plan-cassandra-foundry-parity.md) §S3.

> **Do not resurrect** Postgres-backed pending auth or token exchange.
> New runtime paths must go through the Cassandra adapter.
