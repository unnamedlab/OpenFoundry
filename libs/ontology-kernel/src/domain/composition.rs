//! Pure composition helpers that operate on `&dyn *Store` trait objects.
//!
//! These functions extract the **business logic** (validation, idempotency,
//! event-time stamping, authorisation hooks) out of axum handlers so it can
//! be:
//!
//! 1. Unit-tested against `mockall` mocks or in-memory stores without spinning
//!    up Postgres or Cassandra.
//! 2. Reused unchanged by every `ontology-*` service when handlers are migrated
//!    in S1.4–S1.7 of `docs/architecture/migration-plan-cassandra-foundry-parity.md`.
//!
//! The functions here MUST stay pure with respect to I/O — they only touch
//! the trait objects and the parameters they receive. No `AppState`, no
//! `axum`, no `sqlx` types may leak in.

use storage_abstraction::repositories::{
    Link, LinkStore, LinkTypeId, ObjectId, RepoError, TenantId,
};

/// Reasons a link composition request can be rejected before touching the store.
#[derive(Debug, thiserror::Error)]
pub enum CompositionError {
    #[error("tenant id must not be empty")]
    EmptyTenant,
    #[error("link_type must not be empty")]
    EmptyLinkType,
    #[error("from/to object ids must not be empty")]
    EmptyEndpoint,
    #[error("self-loops are not permitted (from == to)")]
    SelfLoop,
    #[error(transparent)]
    Repo(#[from] RepoError),
}

/// Validate inputs and persist a link instance. Idempotent: per the
/// `LinkStore` contract, a second `put` of the same logical link is a no-op
/// (links are immutable in the kernel).
///
/// Returns `Ok(true)` if the link was newly inserted, `Ok(false)` if the
/// store treated the call as a no-op (already present).
pub async fn create_link(
    store: &dyn LinkStore,
    tenant: TenantId,
    link_type: LinkTypeId,
    from: ObjectId,
    to: ObjectId,
    payload: serde_json::Value,
    created_at_ms: i64,
) -> Result<bool, CompositionError> {
    if tenant.0.trim().is_empty() {
        return Err(CompositionError::EmptyTenant);
    }
    if link_type.0.trim().is_empty() {
        return Err(CompositionError::EmptyLinkType);
    }
    if from.0.trim().is_empty() || to.0.trim().is_empty() {
        return Err(CompositionError::EmptyEndpoint);
    }
    if from == to {
        return Err(CompositionError::SelfLoop);
    }

    // Detect duplicates by checking the outgoing index first; this lets us
    // report `Ok(false)` precisely (the trait `put` only signals errors).
    let existing = store
        .list_outgoing(
            &tenant,
            &link_type,
            &from,
            storage_abstraction::repositories::Page {
                size: 1024,
                token: None,
            },
            storage_abstraction::repositories::ReadConsistency::Eventual,
        )
        .await?;
    let already = existing.items.iter().any(|l| l.to == to);

    let link = Link {
        tenant,
        link_type,
        from,
        to,
        payload: Some(payload),
        created_at_ms,
    };
    store.put(link).await?;
    Ok(!already)
}

/// Validate inputs and delete a link instance. Returns `Ok(true)` if a row
/// was actually deleted, `Ok(false)` if the link did not exist.
pub async fn delete_link(
    store: &dyn LinkStore,
    tenant: TenantId,
    link_type: LinkTypeId,
    from: ObjectId,
    to: ObjectId,
) -> Result<bool, CompositionError> {
    if tenant.0.trim().is_empty() {
        return Err(CompositionError::EmptyTenant);
    }
    if link_type.0.trim().is_empty() {
        return Err(CompositionError::EmptyLinkType);
    }
    if from.0.trim().is_empty() || to.0.trim().is_empty() {
        return Err(CompositionError::EmptyEndpoint);
    }
    Ok(store.delete(&tenant, &link_type, &from, &to).await?)
}
