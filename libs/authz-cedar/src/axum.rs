//! Axum extractor `AuthzGuard<A, R>` integrating Cedar with
//! `auth-middleware`.
//!
//! Wiring:
//!
//! 1. Insert an [`AuthzEngine`] into router state via
//!    `Router::layer(Extension(engine))`. Services that need Cedar
//!    must already be running the `auth_layer` from `auth-middleware`
//!    so `Claims` is in request extensions.
//! 2. Define a marker type implementing [`AuthzAction`] for each
//!    Cedar action you want to enforce, e.g.:
//!
//!    ```ignore
//!    pub struct ReadDataset;
//!    impl AuthzAction for ReadDataset {
//!        fn action_uid() -> EntityUid { uid("Action", "read") }
//!    }
//!    ```
//! 3. Define resource extractors implementing [`AuthzResource`] +
//!    [`FromRequestParts`] (typically a thin wrapper around
//!    `axum::extract::Path<DatasetRid>`).
//! 4. Take `AuthzGuard<ReadDataset, DatasetResource>` in the handler.
//!    The extractor returns `403 Forbidden` if the engine denies and
//!    `500 Internal Server Error` if Cedar errors out.

use std::{marker::PhantomData, str::FromStr, sync::Arc};

use auth_middleware::Claims;
use axum::{
    extract::FromRequestParts,
    http::{StatusCode, request::Parts},
    response::{IntoResponse, Response},
};
use cedar_policy::{
    Context, Entities, Entity, EntityId, EntityTypeName, EntityUid, RestrictedExpression,
};

use crate::engine::{AuthorizeOutcome, AuthzEngine};

/// Marker trait for a Cedar action.
///
/// Implementors are zero-sized types that yield the same `EntityUid`
/// every call (canonically via `EntityUid::from_type_name_and_id`).
pub trait AuthzAction: Send + Sync + 'static {
    fn action_uid() -> EntityUid;
}

/// Resource extracted from the request.
///
/// Implementors are responsible for materialising the Cedar `Entity`
/// (so that policies can inspect attributes such as `markings`) **and**
/// any related entities the policy walks (e.g. inherited markings).
pub trait AuthzResource: Send + Sync + 'static {
    fn resource_uid(&self) -> EntityUid;
    fn resource_entities(&self) -> Vec<Entity>;
}

/// Result of a successful guard check.
pub struct AuthzGuard<A, R> {
    pub claims: Claims,
    pub resource: R,
    pub outcome: AuthorizeOutcome,
    _action: PhantomData<fn() -> A>,
}

impl<S, A, R> FromRequestParts<S> for AuthzGuard<A, R>
where
    S: Send + Sync,
    A: AuthzAction,
    R: AuthzResource + FromRequestParts<S>,
    R::Rejection: IntoResponse,
{
    type Rejection = Response;

    async fn from_request_parts(parts: &mut Parts, state: &S) -> Result<Self, Self::Rejection> {
        let claims = parts
            .extensions
            .get::<Claims>()
            .cloned()
            .ok_or_else(|| (StatusCode::UNAUTHORIZED, "missing Claims").into_response())?;
        let engine = parts
            .extensions
            .get::<AuthzEngine>()
            .cloned()
            .ok_or_else(|| {
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "AuthzEngine not configured",
                )
                    .into_response()
            })?;

        let resource = R::from_request_parts(parts, state)
            .await
            .map_err(IntoResponse::into_response)?;

        let principal_entity = principal_entity_from_claims(&claims);
        let principal_uid = principal_entity.uid();
        let resource_uid = resource.resource_uid();
        let mut entity_set = vec![principal_entity];
        entity_set.extend(resource.resource_entities());

        let entities = Entities::from_entities(entity_set, Some(&engine.store().schema()))
            .map_err(|e| {
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    format!("entity hydration failed: {e}"),
                )
                    .into_response()
            })?;

        let outcome = engine
            .authorize(
                principal_uid,
                A::action_uid(),
                resource_uid,
                Context::empty(),
                &entities,
            )
            .await
            .map_err(|e| {
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    format!("authz error: {e}"),
                )
                    .into_response()
            })?;

        if !outcome.is_allow() {
            return Err((StatusCode::FORBIDDEN, "forbidden").into_response());
        }

        Ok(AuthzGuard {
            claims,
            resource,
            outcome,
            _action: PhantomData,
        })
    }
}

/// Build the canonical `User` entity from a JWT [`Claims`].
///
/// * UID type:   `User`
/// * UID id:     `Claims::sub` (UUID v7)
/// * Attributes:
///     - `tenant`     ← `org_id` (or `""` if absent)
///     - `roles`      ← `Claims::roles`
///     - `clearances` ← `session_scope.allowed_markings` resolved to
///       `Marking::"<id>"` UIDs.
///
/// `Marking` entities are emitted alongside the user so policies can
/// reference them by attribute (`principal.clearances.contains(...)`).
pub fn principal_entity_from_claims(claims: &Claims) -> Entity {
    let user_uid = uid("User", &claims.sub.to_string());

    let tenant = claims
        .org_id
        .map(|o| o.to_string())
        .unwrap_or_else(String::new);

    let clearances: Vec<RestrictedExpression> = claims
        .session_scope
        .as_ref()
        .map(|s| s.allowed_markings.as_slice())
        .unwrap_or(&[])
        .iter()
        .map(|m| RestrictedExpression::new_entity_uid(uid("Marking", m)))
        .collect();

    let roles: Vec<RestrictedExpression> = claims
        .roles
        .iter()
        .map(|r| RestrictedExpression::new_string(r.clone()))
        .collect();

    let attrs = [
        (
            "tenant".to_string(),
            RestrictedExpression::new_string(tenant),
        ),
        (
            "clearances".to_string(),
            RestrictedExpression::new_set(clearances),
        ),
        ("roles".to_string(), RestrictedExpression::new_set(roles)),
    ]
    .into_iter()
    .collect();

    Entity::new(user_uid, attrs, std::collections::HashSet::new())
        .expect("static User attrs are always valid")
}

/// Helper to build an `EntityUid` from `(type_name, id)` strings.
///
/// Panics on malformed input — both arguments are statically known in
/// every call site we have today.
pub fn uid(type_name: &str, id: &str) -> EntityUid {
    let type_name = EntityTypeName::from_str(type_name).expect("static entity type name is valid");
    let id = EntityId::from_str(id).expect("entity id from string is infallible");
    EntityUid::from_type_name_and_id(type_name, id)
}

#[cfg(test)]
mod tests {
    use super::*;
    use auth_middleware::claims::SessionScope;
    use chrono::Utc;
    use uuid::Uuid;

    fn sample_claims() -> Claims {
        Claims {
            sub: Uuid::now_v7(),
            iat: Utc::now().timestamp(),
            exp: Utc::now().timestamp() + 3600,
            iss: None,
            aud: None,
            jti: Uuid::now_v7(),
            email: "u@example.com".into(),
            name: "U".into(),
            roles: vec!["analyst".into()],
            permissions: vec![],
            org_id: Some(Uuid::now_v7()),
            attributes: serde_json::json!({}),
            auth_methods: vec![],
            token_use: None,
            api_key_id: None,
            session_kind: None,
            session_scope: Some(SessionScope {
                allowed_markings: vec!["pii".into(), "confidential".into()],
                ..Default::default()
            }),
        }
    }

    #[test]
    fn principal_entity_from_claims_round_trip() {
        let claims = sample_claims();
        let entity = principal_entity_from_claims(&claims);
        assert!(entity.uid().to_string().starts_with("User::"));
    }

    #[test]
    fn uid_helper_builds_valid_entity_uid() {
        let u = uid("Dataset", "ri.foundry.main.dataset.abc");
        assert!(u.to_string().contains("Dataset"));
    }
}

// Re-exported for ergonomic AppState wiring: callers typically write
// `Extension(Arc::new(engine))` but Axum's Extension already does the
// Arc internally when given an owned `AuthzEngine`. The alias makes
// the type explicit at use sites.
pub type SharedAuthzEngine = Arc<AuthzEngine>;
