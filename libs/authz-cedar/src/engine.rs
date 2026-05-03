//! Top-level engine orchestrating policy evaluation, audit emission
//! and entity hydration.
//!
//! Composition:
//!
//! ```text
//!   AuthzEngine
//!     ├── PolicyStore        (Arc<RwLock<PolicySet>> + bundled schema)
//!     ├── AuditSinkHandle    (Arc<dyn AuthzAuditSink>)
//!     └── (entities are caller-supplied per request)
//! ```
//!
//! `authorize` is the canonical entry point. The Axum extractor
//! (`crate::axum::AuthzGuard`) and the test-suite both exercise this
//! method; the underlying `PolicyStore::is_authorized` is exposed for
//! callers that need raw Cedar `Response`s.

use std::sync::Arc;

use cedar_policy::{Context, Decision, Entities, EntityUid, Request};

use crate::{
    PolicyStore, PolicyStoreError,
    audit::{AuditSinkHandle, AuthzAuditEvent, NoopAuditSink},
};

/// Combined handle: policy store + audit sink.
#[derive(Clone)]
pub struct AuthzEngine {
    store: PolicyStore,
    audit: AuditSinkHandle,
}

impl AuthzEngine {
    /// Build an engine from a [`PolicyStore`] and an audit sink.
    pub fn new(store: PolicyStore, audit: AuditSinkHandle) -> Self {
        Self { store, audit }
    }

    /// Convenience constructor with a [`NoopAuditSink`] — the canonical
    /// choice for unit tests.
    pub fn with_noop_audit(store: PolicyStore) -> Self {
        Self::new(store, Arc::new(NoopAuditSink))
    }

    /// Underlying [`PolicyStore`] handle.
    pub fn store(&self) -> &PolicyStore {
        &self.store
    }

    /// Audit sink handle (cloning is cheap).
    pub fn audit(&self) -> AuditSinkHandle {
        Arc::clone(&self.audit)
    }

    /// Canonical authorization API.
    ///
    /// Returns the raw [`Decision`] together with the diagnostic
    /// information Cedar surfaces (matching policy ids and any
    /// evaluation errors). The audit event is **emitted before the
    /// method returns** but on a detached task, so it never stalls
    /// the request hot path. Callers that need the audit emission to
    /// be synchronous must call the sink directly.
    pub async fn authorize(
        &self,
        principal: EntityUid,
        action: EntityUid,
        resource: EntityUid,
        context: Context,
        entities: &Entities,
    ) -> Result<AuthorizeOutcome, PolicyStoreError> {
        let request = Request::new(
            principal.clone(),
            action.clone(),
            resource.clone(),
            context,
            Some(&self.store.schema()),
        )
        .map_err(|e| PolicyStoreError::Validation(e.to_string()))?;
        let response = self.store.is_authorized(&request, entities).await;

        let decision = response.decision();
        let policy_ids: Vec<String> = response
            .diagnostics()
            .reason()
            .map(|p| p.to_string())
            .collect();
        let diagnostics: Vec<String> = response
            .diagnostics()
            .errors()
            .map(|e| e.to_string())
            .collect();

        let event = AuthzAuditEvent {
            timestamp: chrono::Utc::now(),
            principal: principal.to_string(),
            action: action.to_string(),
            resource: resource.to_string(),
            decision: match decision {
                Decision::Allow => "allow".into(),
                Decision::Deny => "deny".into(),
            },
            tenant: None,
            policy_ids: policy_ids.clone(),
            diagnostics: diagnostics.clone(),
        };
        let sink = Arc::clone(&self.audit);
        tokio::spawn(async move { sink.emit(event).await });

        Ok(AuthorizeOutcome {
            decision,
            policy_ids,
            diagnostics,
        })
    }
}

/// Result returned by [`AuthzEngine::authorize`].
#[derive(Debug, Clone)]
pub struct AuthorizeOutcome {
    pub decision: Decision,
    pub policy_ids: Vec<String>,
    pub diagnostics: Vec<String>,
}

impl AuthorizeOutcome {
    pub fn is_allow(&self) -> bool {
        matches!(self.decision, Decision::Allow)
    }
}
