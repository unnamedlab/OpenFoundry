//! S3.4 — OAuth client configuration lives in the consolidated
//! `pg-schemas` cluster, schema `auth_schema`.
//!
//! This module pins the cluster, schema and table names so handlers
//! don't drift to ad-hoc identifiers. The legacy migration in
//! [`services/oauth-integration-service/migrations/20260427010100_oauth_applications_and_integrations.sql`](../../migrations/20260427010100_oauth_applications_and_integrations.sql)
//! is the authoritative DDL until S6.1 moves it to the consolidated
//! cluster bootstrap.

/// Logical CNPG cluster name (post-S6.1).
pub const PG_CLUSTER: &str = "pg-schemas";

/// Postgres schema for OAuth client configuration.
pub const SCHEMA: &str = "auth_schema";

/// Table for inbound OAuth clients (3rd-party apps that authenticate
/// against `identity-federation-service`).
pub const TABLE_OAUTH_CLIENTS: &str = "oauth_clients";

/// Table for outbound OAuth integrations (we authenticate against
/// 3rd-party providers — Slack, GitHub, Okta, …).
pub const TABLE_OAUTH_EXTERNAL_INTEGRATIONS: &str = "oauth_external_integrations";

/// Table holding hashed client secrets. Secrets are
/// `argon2id`-hashed; only the `secret_hint` (last 4 chars) is
/// human-visible.
pub const TABLE_OAUTH_APPLICATION_CREDENTIALS: &str = "oauth_application_credentials";

/// Recommended `search_path` for the sqlx pool.
pub const fn search_path() -> &'static str {
    SCHEMA
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn schema_constants_are_pinned() {
        assert_eq!(PG_CLUSTER, "pg-schemas");
        assert_eq!(SCHEMA, "auth_schema");
        assert_eq!(TABLE_OAUTH_CLIENTS, "oauth_clients");
        assert_eq!(TABLE_OAUTH_EXTERNAL_INTEGRATIONS, "oauth_external_integrations");
        assert_eq!(
            TABLE_OAUTH_APPLICATION_CREDENTIALS,
            "oauth_application_credentials"
        );
        assert_eq!(search_path(), SCHEMA);
    }
}
