# Archived `identity-federation-service` session migrations

This folder collects the **legacy Postgres state** for sessions,
refresh tokens and OAuth state that was authoritative before
Stream **S3** (see `docs/architecture/migration-plan-cassandra-foundry-parity.md`).

## What changed

- **Authoritative store** for ephemeral identity state is now the
  Cassandra keyspace `auth_runtime` with three tables:
  `user_session`, `refresh_token`, `oauth_state`, plus the lookup/list
  companions `scoped_session_by_user`, `scoped_session_by_id` and
  `refresh_token_by_id` (DDL in
  [`services/identity-federation-service/src/sessions_cassandra.rs`](../../../services/identity-federation-service/src/sessions_cassandra.rs),
  applied via `cassandra_kernel::migrate::apply`).
- JWKS signing keys remain in `pg-schemas.auth_schema.jwks_keys`
  (rare rotation, custody in Vault transit per S3.1.b).
- Postgres `refresh_tokens`, `scoped_sessions` and any oauth-state
  table are now legacy-only and staged for DROP after cutover sign-off.
  The last runtime migration that created `scoped_sessions` now lives
  alongside this README as
  `20260425193000_scoped_sessions_security.sql`, and the original
  `refresh_tokens` DDL now lives in `20260419000001_refresh_tokens.sql`.
- The remaining Postgres surface for auth is limited to long-lived
  control-plane data such as users, roles/permissions/policies, MFA
  metadata and JWKS mirrors.

## Cutover gate

The DROP migration (`drop_session_tables.sql.disabled`) **MUST
NOT** run before:

1. every active session has been replayed into Cassandra
   `auth_runtime.user_session` or naturally expired;
2. all callers of `domain::sessions` write paths have switched to
   `sessions_cassandra::SessionsAdapter`;
3. JWKS rotation has run successfully at least once against the
   Vault-managed key (S3.1.c);
4. the audit-event consumer reports zero `audit.identity.v1`
   events sourced from the Postgres path for at least 7 days;
5. the failover drill (S3.5) has been signed off.

## Pointers

- New Cassandra adapter: [`sessions_cassandra::SessionsAdapter`](../../../services/identity-federation-service/src/sessions_cassandra.rs).
- ASVS L2 inventory: [`identity-asvs-inventory.md`](../../runbooks/identity-asvs-inventory.md).
- Pen-test runbook: [`identity-pen-test-runbook.md`](../../runbooks/identity-pen-test-runbook.md).
- Plan reference: `docs/architecture/migration-plan-cassandra-foundry-parity.md` §S3.

> **Do not resurrect** the legacy Postgres session path. No runtime
> writer should target those tables; new write paths must go through
> the Cassandra adapter.
