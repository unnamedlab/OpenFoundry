//! S3.3 — Policy definitions live in the consolidated `pg-policy`
//! cluster (see [Stream S6](../../../../docs/architecture/migration-plan-cassandra-foundry-parity.md)).
//!
//! This module pins the cluster, schema and table names so handlers
//! don't drift to ad-hoc identifiers. The actual SQL DDL is owned by
//! the `pg-policy` bootstrap (see
//! [`infra/k8s/platform/manifests/cnpg/clusters/pg-policy.yaml`](../../../../infra/k8s/platform/manifests/cnpg/clusters/pg-policy.yaml)
//! once S6.1 lands).

/// Logical CNPG cluster name. Connection string is composed by Helm
/// (`envSecrets.DATABASE_URL`); handlers should never hard-code it.
pub const PG_CLUSTER: &str = "pg-policy";

/// Postgres schema for session-governance policy definitions.
pub const SCHEMA: &str = "session_governance_policy";

/// Table holding declarative session policy rules
/// (e.g. *"contractors expire after 4 h"*, *"prod-admin requires
/// MFA in last 5 min"*). Authoritative storage; revocation list in
/// Cassandra is the runtime cache.
pub const TABLE_SESSION_POLICY: &str = "session_policy";

/// Table holding restricted-view bindings (which session policy
/// applies to which scope).
pub const TABLE_RESTRICTED_VIEW: &str = "restricted_view";

/// Recommended `search_path` to set on the sqlx pool. The handler
/// boot path should append `?options=-c%20search_path%3D<schema>`
/// to the `DATABASE_URL`.
pub const fn search_path() -> &'static str {
    SCHEMA
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn schema_constants_are_pinned() {
        assert_eq!(PG_CLUSTER, "pg-policy");
        assert_eq!(SCHEMA, "session_governance_policy");
        assert_eq!(TABLE_SESSION_POLICY, "session_policy");
        assert_eq!(TABLE_RESTRICTED_VIEW, "restricted_view");
        assert_eq!(search_path(), SCHEMA);
    }
}
