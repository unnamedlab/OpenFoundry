//! `authz-cedar` — embedded Cedar policy engine for OpenFoundry.
//!
//! Per [ADR-0027](../../docs/architecture/adr/ADR-0027-cedar-policy-engine.md)
//! every service evaluates Cedar policies *in-process* (no network hop
//! to a remote PDP). The schema is bundled at compile time via
//! [`SCHEMA_SRC`]; policies are loaded from `pg-policy.cedar_policies`
//! at startup and held in an `Arc<RwLock<PolicySet>>` that hot-reloads
//! on the `authz.policy.changed` NATS event.
//!
//! Two surfaces are exposed:
//!
//! * [`PolicyStore`] — pure in-memory policy set + schema. Knows how to
//!   parse, validate and replace its inner [`PolicySet`]. Safe to use
//!   from tests and dev tooling without touching Postgres.
//! * [`pg::PgPolicyStore`] — Postgres-backed loader (feature `postgres`)
//!   that reads the latest version of every active row from
//!   `pg-policy.cedar_policies` and feeds them into a [`PolicyStore`].
//!
//! Policy evaluation itself is a thin wrapper around
//! [`cedar_policy::Authorizer::is_authorized`]; see [`PolicyStore::is_authorized`].

use std::sync::Arc;

use cedar_policy::{
    Authorizer, Context, Decision, Entities, EntityUid, Policy, PolicyId, PolicySet, Request,
    Response, Schema, ValidationMode, Validator,
};
use thiserror::Error;
use tokio::sync::RwLock;

/// Source of the Cedar schema bundled with this crate.
///
/// Exposed so that callers (e.g. service startup code) can pre-validate
/// policies they author against the same schema the engine uses.
pub const SCHEMA_SRC: &str = include_str!("../cedar_schema.cedarschema");

/// Errors surfaced by the Cedar policy store.
#[derive(Debug, Error)]
pub enum PolicyStoreError {
    /// The bundled schema failed to parse. Indicates a build-time bug
    /// — the schema is shipped with the crate.
    #[error("invalid bundled cedar schema: {0}")]
    Schema(String),

    /// A policy text could not be parsed.
    #[error("invalid policy `{id}`: {source}")]
    PolicyParse {
        id: String,
        #[source]
        source: cedar_policy::ParseErrors,
    },

    /// A parsed policy set failed schema-aware validation.
    #[error("policy validation failed:\n{0}")]
    Validation(String),

    /// Backing store I/O failure (e.g. Postgres unavailable).
    #[error("backing store error: {0}")]
    Backend(#[source] anyhow::Error),
}

/// A loaded policy with the metadata needed to drive hot-reload.
///
/// Mirrors the columns of `pg-policy.cedar_policies`:
/// `(id TEXT PRIMARY KEY, version INT NOT NULL, source TEXT NOT NULL,
///   description TEXT, active BOOL NOT NULL, updated_at TIMESTAMPTZ)`.
#[derive(Debug, Clone)]
pub struct PolicyRecord {
    pub id: String,
    pub version: i32,
    pub source: String,
    pub description: Option<String>,
}

/// In-memory Cedar [`PolicySet`] + bundled schema, behind an `RwLock`.
///
/// Cloning is cheap: every [`PolicyStore`] handle shares the same
/// `Arc<RwLock<PolicySet>>` so `replace_policies` is observed
/// immediately by every reader.
#[derive(Clone)]
pub struct PolicyStore {
    schema: Arc<Schema>,
    policies: Arc<RwLock<PolicySet>>,
    authorizer: Arc<Authorizer>,
}

impl PolicyStore {
    /// Build an empty store using the bundled schema.
    ///
    /// Useful for tests and as the initial value before the first
    /// successful load from Postgres.
    pub fn empty() -> Result<Self, PolicyStoreError> {
        let schema = Schema::from_cedarschema_str(SCHEMA_SRC)
            .map(|(s, _warnings)| s)
            .map_err(|e| PolicyStoreError::Schema(e.to_string()))?;
        Ok(Self {
            schema: Arc::new(schema),
            policies: Arc::new(RwLock::new(PolicySet::new())),
            authorizer: Arc::new(Authorizer::new()),
        })
    }

    /// Build a store and immediately load the supplied records.
    pub async fn with_policies(records: &[PolicyRecord]) -> Result<Self, PolicyStoreError> {
        let store = Self::empty()?;
        store.replace_policies(records).await?;
        Ok(store)
    }

    /// Bundled schema (parsed once at construction).
    pub fn schema(&self) -> Arc<Schema> {
        Arc::clone(&self.schema)
    }

    /// Atomically replace the active policy set.
    ///
    /// Each record is parsed individually so that a single bad row
    /// reports a precise [`PolicyStoreError::PolicyParse`]. The
    /// resulting [`PolicySet`] is then schema-validated in
    /// [`ValidationMode::Strict`]; only on success do we swap the
    /// internal `RwLock` content. Concurrent readers therefore never
    /// observe a partially-applied or invalid state.
    pub async fn replace_policies(
        &self,
        records: &[PolicyRecord],
    ) -> Result<(), PolicyStoreError> {
        let mut next = PolicySet::new();
        for record in records {
            let policy_id = PolicyId::new(&record.id);
            let policy = Policy::parse(Some(policy_id), &record.source)
                .map_err(|source| PolicyStoreError::PolicyParse {
                    id: record.id.clone(),
                    source,
                })?;
            next.add(policy)
                .map_err(|e| PolicyStoreError::Validation(e.to_string()))?;
        }

        let validator = Validator::new((*self.schema).clone());
        let result = validator.validate(&next, ValidationMode::Strict);
        if !result.validation_passed() {
            return Err(PolicyStoreError::Validation(
                result
                    .validation_errors()
                    .map(|e| e.to_string())
                    .collect::<Vec<_>>()
                    .join("\n"),
            ));
        }

        let mut guard = self.policies.write().await;
        *guard = next;
        Ok(())
    }

    /// Number of currently loaded policies — exposed for `/metrics`.
    pub async fn len(&self) -> usize {
        self.policies.read().await.policies().count()
    }

    /// `true` if no policies are loaded.
    pub async fn is_empty(&self) -> bool {
        self.len().await == 0
    }

    /// Evaluate `Authorizer::is_authorized` against the current policy
    /// set. Cloning the inner [`PolicySet`] is cheap (its policies are
    /// reference-counted internally), so we hold the read lock only
    /// long enough to clone.
    pub async fn is_authorized(
        &self,
        request: &Request,
        entities: &Entities,
    ) -> Response {
        let policies = self.policies.read().await.clone();
        self.authorizer.is_authorized(request, &policies, entities)
    }

    /// Convenience wrapper that reduces a `Response` to a `bool`.
    pub async fn is_allowed(
        &self,
        principal: EntityUid,
        action: EntityUid,
        resource: EntityUid,
        context: Context,
        entities: &Entities,
    ) -> Result<bool, PolicyStoreError> {
        let request = Request::new(principal, action, resource, context, Some(&self.schema))
            .map_err(|e| PolicyStoreError::Validation(e.to_string()))?;
        let response = self.is_authorized(&request, entities).await;
        Ok(matches!(response.decision(), Decision::Allow))
    }
}

#[cfg(feature = "postgres")]
pub mod pg;

#[cfg(feature = "nats")]
pub mod nats;

#[cfg(feature = "axum")]
pub mod axum;

pub mod audit;
pub mod engine;

pub use audit::{AuditSinkHandle, AuthzAuditEvent, AuthzAuditSink, NoopAuditSink, TracingAuditSink};
pub use engine::{AuthorizeOutcome, AuthzEngine};

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn empty_store_loads_bundled_schema() {
        let store = PolicyStore::empty().expect("schema parses");
        assert!(store.is_empty().await);
    }

    #[tokio::test]
    async fn replace_policies_validates_against_schema() {
        let store = PolicyStore::empty().expect("schema parses");
        // Permit any user to read any dataset they have clearance for.
        let policy = PolicyRecord {
            id: "default-allow-clearance".into(),
            version: 1,
            description: None,
            source: r#"
                permit(
                  principal,
                  action == Action::"read",
                  resource is Dataset
                ) when {
                  resource.markings.containsAll(principal.clearances) ||
                  principal.clearances.containsAll(resource.markings)
                };
            "#
            .into(),
        };
        store.replace_policies(&[policy]).await.expect("valid");
        assert_eq!(store.len().await, 1);
    }

    #[tokio::test]
    async fn invalid_policy_text_is_rejected() {
        let store = PolicyStore::empty().expect("schema parses");
        let bad = PolicyRecord {
            id: "broken".into(),
            version: 1,
            description: None,
            source: "this is not cedar".into(),
        };
        let err = store
            .replace_policies(&[bad])
            .await
            .expect_err("must fail");
        assert!(matches!(err, PolicyStoreError::PolicyParse { .. }));
    }
}
