//! Cedar wiring for the media-sets service (H3).
//!
//! Three jobs:
//!
//! 1. **Bundled default policies.** ADR-0027 stores the canonical
//!    policy set in `pg-policy.cedar_policies`, but every service ships
//!    a *seed* set so the engine boots with sensible defaults and so
//!    integration tests don't depend on Postgres. The seven policies
//!    here mirror the ABAC contract documented in
//!    `docs_original_palantir_foundry/.../Configure granular policies
//!    for media items.md`.
//!
//! 2. **Entity hydration.** Each handler loads the relevant DB rows
//!    and feeds them through [`build_media_set_entity`] /
//!    [`build_media_item_entity`] before invoking the engine.
//!    `MediaItem`'s effective markings are computed as
//!    `union(item.markings, parent_set.markings)` so the granular
//!    override in the docs is honoured automatically.
//!
//! 3. **Presigned URL claims.** [`mint_presign_claim`] mints a tiny
//!    JWT (5 min default) the operator embeds in the URL; the
//!    edge-gateway (or any downstream proxy) calls
//!    [`verify_presign_claim`] before letting the GET reach the
//!    object store. Without an in-process clearance check, the
//!    presign path returns `MediaError::Forbidden` and no URL is
//!    issued at all — the JWT is the second line of defense.

use std::collections::HashSet;

use auth_middleware::Claims;
use authz_cedar::AuthzEngine;
use authz_cedar::axum::{principal_entity_from_claims, uid};
use cedar_policy::{Context, Entities, Entity, EntityUid, RestrictedExpression};
use chrono::Utc;
use jsonwebtoken::{
    DecodingKey, EncodingKey, Header, Validation, decode, encode, Algorithm,
};
use serde::{Deserialize, Serialize};

use crate::domain::error::MediaError;
use crate::models::{MediaItem, MediaSet};

// ---------------------------------------------------------------------------
// Bundled default policy set (Cedar 4 syntax). Loaded at boot through
// `PolicyStore::with_policies`. Production overrides this set by
// installing rows into `pg-policy.cedar_policies` and emitting
// `authz.policy.changed` (ADR-0027 hot-reload path).
// ---------------------------------------------------------------------------

pub struct PolicySeed {
    pub id: &'static str,
    pub source: &'static str,
}

pub const MEDIA_DEFAULT_POLICIES: &[PolicySeed] = &[
    PolicySeed {
        id: "media-set-view",
        source: r#"
            permit (
              principal,
              action == Action::"media_set::view",
              resource is MediaSet
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#,
    },
    PolicySeed {
        id: "media-set-manage",
        source: r#"
            permit (
              principal,
              action == Action::"media_set::manage",
              resource is MediaSet
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
              principal.clearances.containsAll(resource.markings)
            };
        "#,
    },
    PolicySeed {
        id: "media-set-delete",
        source: r#"
            permit (
              principal,
              action == Action::"media_set::delete",
              resource is MediaSet
            ) when {
              principal.tenant == resource.tenant &&
              principal.roles.contains("admin") &&
              principal.clearances.containsAll(resource.markings)
            };
        "#,
    },
    PolicySeed {
        id: "media-item-read",
        source: r#"
            permit (
              principal,
              action == Action::"media_item::read",
              resource is MediaItem
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#,
    },
    PolicySeed {
        id: "media-item-write",
        source: r#"
            permit (
              principal,
              action == Action::"media_item::write",
              resource is MediaItem
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
              principal.clearances.containsAll(resource.markings)
            };
        "#,
    },
    PolicySeed {
        id: "media-item-delete",
        source: r#"
            permit (
              principal,
              action == Action::"media_item::delete",
              resource is MediaItem
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
              principal.clearances.containsAll(resource.markings)
            };
        "#,
    },
];

/// Convert the seed list into [`authz_cedar::PolicyRecord`]s ready for
/// `PolicyStore::with_policies`.
pub fn default_policy_records() -> Vec<authz_cedar::PolicyRecord> {
    MEDIA_DEFAULT_POLICIES
        .iter()
        .map(|seed| authz_cedar::PolicyRecord {
            id: seed.id.to_string(),
            version: 1,
            description: None,
            source: seed.source.to_string(),
        })
        .collect()
}

// ---------------------------------------------------------------------------
// Action UIDs — single allocation point so handlers stay terse.
// ---------------------------------------------------------------------------

pub fn action_view() -> EntityUid {
    uid("Action", "media_set::view")
}
pub fn action_manage() -> EntityUid {
    uid("Action", "media_set::manage")
}
pub fn action_delete_set() -> EntityUid {
    uid("Action", "media_set::delete")
}
pub fn action_item_read() -> EntityUid {
    uid("Action", "media_item::read")
}
pub fn action_item_write() -> EntityUid {
    uid("Action", "media_item::write")
}
pub fn action_item_delete() -> EntityUid {
    uid("Action", "media_item::delete")
}

// ---------------------------------------------------------------------------
// Entity hydration
// ---------------------------------------------------------------------------

/// Build the Cedar `MediaSet` entity from a loaded row + its tenant.
///
/// The set's markings are emitted as `Marking::"<name>"` UIDs. Marking
/// entities themselves are appended to the entity set so policies that
/// dereference `principal.clearances` (which are also `Marking::"…"`
/// UIDs) line up with the resource side.
pub fn build_media_set_entity(set: &MediaSet, tenant: &str) -> Entity {
    let resource_uid = uid("MediaSet", &set.rid);
    let markings: Vec<RestrictedExpression> = set
        .markings
        .iter()
        .map(|m| RestrictedExpression::new_entity_uid(uid("Marking", &m.to_ascii_lowercase())))
        .collect();
    let attrs = [
        ("rid".into(), RestrictedExpression::new_string(set.rid.clone())),
        (
            "tenant".into(),
            RestrictedExpression::new_string(tenant.to_string()),
        ),
        (
            "project_rid".into(),
            RestrictedExpression::new_string(set.project_rid.clone()),
        ),
        (
            "transaction_policy".into(),
            RestrictedExpression::new_string(set.transaction_policy.clone()),
        ),
        ("virtual".into(), RestrictedExpression::new_bool(set.virtual_)),
        (
            "markings".into(),
            RestrictedExpression::new_set(markings),
        ),
    ]
    .into_iter()
    .collect();

    Entity::new(resource_uid, attrs, HashSet::new())
        .expect("static MediaSet attrs are always valid")
}

/// Build the Cedar `MediaItem` entity, **unioning** the parent set's
/// markings into the item's effective set.
///
/// This implements Foundry's "default inheritance" rule:
/// > By default, users must have access to all markings on the backing
/// > media set to view any media reference properties on this object.
///
/// Per-item markings tighten the envelope further (the granular
/// override the docs show with the SECRET-on-PII example).
pub fn build_media_item_entity(item: &MediaItem, parent_set: &MediaSet, tenant: &str) -> Entity {
    let mut effective: HashSet<String> = parent_set
        .markings
        .iter()
        .map(|m| m.to_ascii_lowercase())
        .collect();
    for m in &item.markings {
        effective.insert(m.to_ascii_lowercase());
    }
    let item_uid = uid("MediaItem", &item.rid);
    let parent_uid = uid("MediaSet", &item.media_set_rid);
    let mut parents = HashSet::new();
    parents.insert(parent_uid);

    let markings: Vec<RestrictedExpression> = effective
        .iter()
        .map(|m| RestrictedExpression::new_entity_uid(uid("Marking", m)))
        .collect();

    let attrs = [
        (
            "media_set_rid".into(),
            RestrictedExpression::new_string(item.media_set_rid.clone()),
        ),
        (
            "tenant".into(),
            RestrictedExpression::new_string(tenant.to_string()),
        ),
        (
            "mime_type".into(),
            RestrictedExpression::new_string(item.mime_type.clone()),
        ),
        (
            "size_bytes".into(),
            RestrictedExpression::new_long(item.size_bytes),
        ),
        (
            "markings".into(),
            RestrictedExpression::new_set(markings),
        ),
    ]
    .into_iter()
    .collect();

    Entity::new(item_uid, attrs, parents).expect("static MediaItem attrs are always valid")
}

/// Synthetic `Marking` entity. The Cedar engine needs the entity to
/// exist in the entity set so attribute references resolve, but the
/// only attribute the policies inspect today is `name`.
fn build_marking_entity(name: &str) -> Entity {
    let attrs = [(
        "name".into(),
        RestrictedExpression::new_string(name.to_string()),
    )]
    .into_iter()
    .collect();
    Entity::new(uid("Marking", name), attrs, HashSet::new())
        .expect("static Marking attrs are always valid")
}

/// Compose the full entity set the engine needs for a MediaSet check.
fn entities_for_set(
    principal: Entity,
    set_entity: Entity,
    set_markings: &[String],
    extra_clearances: &[String],
) -> Result<Entities, MediaError> {
    let mut entities = vec![principal, set_entity];
    let mut seen = HashSet::new();
    for m in set_markings.iter().chain(extra_clearances.iter()) {
        let lower = m.to_ascii_lowercase();
        if seen.insert(lower.clone()) {
            entities.push(build_marking_entity(&lower));
        }
    }
    Entities::from_entities(entities, None)
        .map_err(|e| MediaError::Authz(format!("entity hydration failed: {e}")))
}

/// Compose the full entity set for a MediaItem check (item + parent + markings).
fn entities_for_item(
    principal: Entity,
    set_entity: Entity,
    item_entity: Entity,
    set_markings: &[String],
    item_markings: &[String],
    extra_clearances: &[String],
) -> Result<Entities, MediaError> {
    let mut entities = vec![principal, set_entity, item_entity];
    let mut seen = HashSet::new();
    for m in set_markings
        .iter()
        .chain(item_markings.iter())
        .chain(extra_clearances.iter())
    {
        let lower = m.to_ascii_lowercase();
        if seen.insert(lower.clone()) {
            entities.push(build_marking_entity(&lower));
        }
    }
    Entities::from_entities(entities, None)
        .map_err(|e| MediaError::Authz(format!("entity hydration failed: {e}")))
}

// ---------------------------------------------------------------------------
// Cedar checks
// ---------------------------------------------------------------------------

fn caller_clearances(claims: &Claims) -> Vec<String> {
    claims
        .session_scope
        .as_ref()
        .map(|s| s.allowed_markings.clone())
        .unwrap_or_default()
}

/// Run a Cedar check over a media set. Returns `Ok(())` on `Allow`,
/// `Err(MediaError::Forbidden { ... })` on `Deny`. The error carries
/// the specific markings the caller is missing so the handler can
/// surface a precise 403 message ("missing clearance: SECRET").
pub async fn check_media_set(
    engine: &AuthzEngine,
    claims: &Claims,
    action: EntityUid,
    set: &MediaSet,
) -> Result<(), MediaError> {
    let principal = principal_entity_from_claims(claims);
    let principal_uid = principal.uid();
    let tenant = claims
        .org_id
        .map(|o| o.to_string())
        .unwrap_or_default();
    let resource = build_media_set_entity(set, &tenant);
    let resource_uid = resource.uid();
    let clearances = caller_clearances(claims);
    let entities = entities_for_set(principal, resource, &set.markings, &clearances)?;
    let outcome = engine
        .authorize(principal_uid, action, resource_uid, Context::empty(), &entities)
        .await
        .map_err(|e| MediaError::Authz(e.to_string()))?;
    if outcome.is_allow() {
        return Ok(());
    }
    Err(forbidden_with_missing(&set.markings, &clearances, claims))
}

/// Run a Cedar check over a media item. Internally also enforces the
/// parent-set `media_set::view` so a viewer-blocked set cascades to
/// every item without needing per-item rows.
pub async fn check_media_item(
    engine: &AuthzEngine,
    claims: &Claims,
    action: EntityUid,
    item: &MediaItem,
    parent_set: &MediaSet,
) -> Result<(), MediaError> {
    // Parent-set view first — the spec wants `(User can view it.media_set)
    // && user.clearances containsAll it.markings`. The first half is the
    // view check on the set; the second half is the standard item check
    // (item.markings already include the union of parent + own per
    // `build_media_item_entity`).
    check_media_set(engine, claims, action_view(), parent_set).await?;

    let principal = principal_entity_from_claims(claims);
    let principal_uid = principal.uid();
    let tenant = claims
        .org_id
        .map(|o| o.to_string())
        .unwrap_or_default();
    let set_entity = build_media_set_entity(parent_set, &tenant);
    let item_entity = build_media_item_entity(item, parent_set, &tenant);
    let item_uid = item_entity.uid();
    let clearances = caller_clearances(claims);
    let entities = entities_for_item(
        principal,
        set_entity,
        item_entity,
        &parent_set.markings,
        &item.markings,
        &clearances,
    )?;
    let outcome = engine
        .authorize(principal_uid, action, item_uid, Context::empty(), &entities)
        .await
        .map_err(|e| MediaError::Authz(e.to_string()))?;
    if outcome.is_allow() {
        return Ok(());
    }
    let mut effective = parent_set.markings.clone();
    effective.extend(item.markings.iter().cloned());
    Err(forbidden_with_missing(&effective, &clearances, claims))
}

fn forbidden_with_missing(
    required: &[String],
    clearances: &[String],
    claims: &Claims,
) -> MediaError {
    if claims.has_role("admin") {
        return MediaError::Forbidden("denied by policy".into());
    }
    let owned: HashSet<String> = clearances
        .iter()
        .map(|c| c.to_ascii_lowercase())
        .collect();
    let mut missing: Vec<String> = required
        .iter()
        .map(|r| r.to_ascii_lowercase())
        .filter(|r| !owned.contains(r))
        .collect();
    missing.sort();
    if missing.is_empty() {
        MediaError::Forbidden("denied by policy".into())
    } else {
        MediaError::Forbidden(format!(
            "missing clearance: {}",
            missing.join(", ").to_uppercase()
        ))
    }
}

// ---------------------------------------------------------------------------
// Presigned URL claim
// ---------------------------------------------------------------------------

/// Claim payload embedded in the presigned download URL.
///
/// Keep this narrow: anything that grows here is a thing the gateway
/// has to validate end-to-end, and the failure modes get fuzzy fast.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PresignClaim {
    /// `Claims::sub` of the user the URL was issued to.
    pub sub: String,
    /// Item RID the URL targets — the gateway re-hashes this against
    /// the URL path before letting the request through.
    pub item_rid: String,
    /// Snapshot of the item's effective markings at issue time. The
    /// gateway uses this to detect "was the user allowed when we
    /// issued this URL?" without having to re-do the Cedar walk.
    pub markings: Vec<String>,
    /// Issued-at + expiry (epoch seconds). Default TTL is 5 minutes.
    pub iat: i64,
    pub exp: i64,
}

pub const PRESIGN_CLAIM_DEFAULT_TTL_SECS: i64 = 300;

/// Mint a short-lived JWT carrying the presign claim, signed with
/// HS256 against the same secret the rest of the platform uses (kept
/// in `AppState::presign_secret`, populated from the ambient
/// `JWT_SECRET` env var so identity-federation-service and the
/// edge-gateway can validate the same tokens).
pub fn mint_presign_claim(
    presign_secret: &[u8],
    sub: String,
    item_rid: String,
    effective_markings: Vec<String>,
    ttl_secs: Option<i64>,
) -> Result<String, MediaError> {
    let now = Utc::now().timestamp();
    let claim = PresignClaim {
        sub,
        item_rid,
        markings: effective_markings,
        iat: now,
        exp: now + ttl_secs.unwrap_or(PRESIGN_CLAIM_DEFAULT_TTL_SECS),
    };
    encode(
        &Header::new(Algorithm::HS256),
        &claim,
        &EncodingKey::from_secret(presign_secret),
    )
    .map_err(|e| MediaError::Authz(format!("presign claim encode: {e}")))
}

/// Verify a presign claim — the gateway calls this before letting the
/// GET reach the storage backend. Returns the decoded claim on
/// success; surfaces `MediaError::Forbidden` on any failure (expired,
/// item mismatch, signature error).
pub fn verify_presign_claim(
    presign_secret: &[u8],
    token: &str,
    expected_item_rid: &str,
) -> Result<PresignClaim, MediaError> {
    let mut validation = Validation::new(Algorithm::HS256);
    validation.validate_exp = true;
    validation.leeway = 5;
    validation.required_spec_claims.clear();
    let data = decode::<PresignClaim>(token, &DecodingKey::from_secret(presign_secret), &validation)
        .map_err(|e| MediaError::Forbidden(format!("invalid presign claim: {e}")))?;
    if data.claims.item_rid != expected_item_rid {
        return Err(MediaError::Forbidden(format!(
            "presign claim targets `{}`, not `{}`",
            data.claims.item_rid, expected_item_rid
        )));
    }
    Ok(data.claims)
}

#[cfg(test)]
mod tests {
    use super::*;
    use authz_cedar::{PolicyStore, audit::NoopAuditSink};
    use chrono::Utc;
    use core_models::MediaSetSchema;
    use serde_json::Value;
    use std::sync::Arc;
    use uuid::Uuid;

    fn sample_set(markings: Vec<&str>) -> MediaSet {
        MediaSet {
            rid: "ri.foundry.main.media_set.test-1".into(),
            project_rid: "ri.foundry.main.project.test".into(),
            name: "test".into(),
            schema: MediaSetSchema::Image.as_str().into(),
            allowed_mime_types: vec![],
            transaction_policy: "TRANSACTIONLESS".into(),
            retention_seconds: 0,
            virtual_: false,
            source_rid: None,
            markings: markings.into_iter().map(|s| s.to_string()).collect(),
            created_at: Utc::now(),
            created_by: "tester".into(),
        }
    }

    fn claims_with(roles: Vec<&str>, allowed: Vec<&str>, tenant: Option<Uuid>) -> Claims {
        Claims {
            sub: Uuid::now_v7(),
            iat: Utc::now().timestamp(),
            exp: Utc::now().timestamp() + 3600,
            iss: None,
            aud: None,
            jti: Uuid::now_v7(),
            email: "u@example.test".into(),
            name: "U".into(),
            roles: roles.into_iter().map(String::from).collect(),
            permissions: vec![],
            org_id: tenant,
            attributes: Value::Null,
            auth_methods: vec![],
            token_use: None,
            api_key_id: None,
            session_kind: None,
            session_scope: Some(auth_middleware::claims::SessionScope {
                allowed_markings: allowed.into_iter().map(String::from).collect(),
                ..Default::default()
            }),
        }
    }

    async fn engine_with_defaults() -> AuthzEngine {
        let store = PolicyStore::with_policies(&default_policy_records())
            .await
            .expect("default policies validate");
        AuthzEngine::new(store, Arc::new(NoopAuditSink))
    }

    #[tokio::test]
    async fn view_allowed_when_clearance_covers_set_markings() {
        let engine = engine_with_defaults().await;
        let tenant = Uuid::now_v7();
        let set = sample_set(vec!["pii"]);
        let claims = claims_with(vec!["viewer"], vec!["pii"], Some(tenant));
        // Note: tenant on the entity is `claims.org_id.to_string()`, so
        // both sides use the same tenant id in the policy `==` check.
        check_media_set(&engine, &claims, action_view(), &set)
            .await
            .expect("allow");
    }

    #[tokio::test]
    async fn view_denied_when_missing_clearance_lists_missing_markings() {
        let engine = engine_with_defaults().await;
        let tenant = Uuid::now_v7();
        let set = sample_set(vec!["pii", "secret"]);
        let claims = claims_with(vec!["viewer"], vec!["pii"], Some(tenant));
        let err = check_media_set(&engine, &claims, action_view(), &set)
            .await
            .expect_err("deny");
        let MediaError::Forbidden(msg) = err else {
            panic!("expected Forbidden");
        };
        assert!(msg.contains("SECRET"), "{msg}");
    }
}
