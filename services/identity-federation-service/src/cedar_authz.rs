//! Cedar wiring for `identity-federation-service` (S3.1.i).
//!
//! Three responsibilities:
//!
//! 1. **Bundled policy bootstrap.** The 3 admin policies live on disk
//!    at `services/identity-federation-service/policies/identity_admin.cedar`
//!    and are baked into the binary via `include_str!`. [`bootstrap_engine`]
//!    parses them through `cedar_policy::PolicySet::from_str`, then
//!    feeds the records to [`authz_cedar::PolicyStore::with_policies`]
//!    so they get strict-validated against the bundled schema before
//!    the engine takes any traffic.
//!
//! 2. **Hot-reload subscriber.** [`spawn_policy_reload`] hooks the
//!    `authz.policy.changed` NATS subject (per ADR-0027) and rewrites
//!    the in-memory policy set whenever the authoring service emits a
//!    change event. The subscriber re-parses the bundled file — the
//!    Postgres-backed loader will overlay rows from
//!    `pg-policy.cedar_policies` once the writer goes live.
//!
//! 3. **Action / resource markers + guard.** [`AdminAuthzGuard<A, R>`]
//!    is the request extractor that handlers compose
//!    (`security_ops::rotate_jwks` and `scim::create_user/...`). It
//!    differs from `authz_cedar::axum::AuthzGuard` in one important
//!    way: it reads `kind`, `mfa_age_secs` and `groups` from
//!    `claims.attributes` and wires them as Cedar entity attrs /
//!    parent UIDs so the policies in `identity_admin.cedar` actually
//!    match service-account / IdentityKeyRotators calls.

use std::{collections::HashSet, marker::PhantomData, str::FromStr, sync::Arc};

use auth_middleware::Claims;
use authz_cedar::{
    AuthzEngine, PolicyRecord, PolicyStore, PolicyStoreError,
    audit::TracingAuditSink,
    axum::{AuthzAction, AuthzResource, uid},
    nats::PolicyReloadSubscriber,
};
use axum::{
    extract::FromRequestParts,
    http::{StatusCode, request::Parts},
    response::{IntoResponse, Response},
};
use cedar_policy::{Context, Entities, Entity, EntityUid, PolicySet, RestrictedExpression};
use serde_json::Value;

/// Bundled admin policy set (S3.1.i). Baked into the binary so the
/// service boots without depending on `pg-policy.cedar_policies`.
pub const IDENTITY_ADMIN_POLICIES: &str = include_str!("../policies/identity_admin.cedar");

/// Parse [`IDENTITY_ADMIN_POLICIES`] and return the records the
/// `PolicyStore` expects (one per Cedar `permit`/`forbid` block).
pub fn bundled_policy_records() -> Result<Vec<PolicyRecord>, PolicyStoreError> {
    let parsed = PolicySet::from_str(IDENTITY_ADMIN_POLICIES).map_err(|e| {
        PolicyStoreError::PolicyParse {
            id: "identity_admin.cedar".to_string(),
            source: e,
        }
    })?;
    Ok(parsed
        .policies()
        .map(|policy| PolicyRecord {
            id: policy.id().to_string(),
            version: 1,
            description: None,
            source: policy.to_string(),
        })
        .collect())
}

/// Build the [`AuthzEngine`] used by the service.
///
/// Uses [`TracingAuditSink`] so every decision lands in the standard
/// `authz.audit` tracing target — production will swap in a Kafka sink
/// once `authorization-policy-service` exposes the `audit.authz.v1`
/// publisher.
pub async fn bootstrap_engine() -> Result<Arc<AuthzEngine>, PolicyStoreError> {
    let records = bundled_policy_records()?;
    let store = PolicyStore::with_policies(&records).await?;
    Ok(Arc::new(AuthzEngine::new(
        store,
        Arc::new(TracingAuditSink),
    )))
}

/// Subscribe to `authz.policy.changed` and re-load the bundled policy
/// set on every signal.
///
/// The handle returned must be kept alive (`std::mem::forget` in
/// `main` is fine — we only stop on process exit). On NATS connection
/// errors we log and return without subscribing; the engine keeps
/// serving the boot-time policy set.
pub async fn spawn_policy_reload(nats_url: &str, engine: Arc<AuthzEngine>) -> anyhow::Result<()> {
    let client = async_nats::connect(nats_url).await?;
    let subscriber = PolicyReloadSubscriber::new(client);
    let store = engine.store().clone();
    let _handle = subscriber
        .run(move || {
            let store = store.clone();
            Box::pin(async move {
                let records =
                    bundled_policy_records().map_err(|e| anyhow::anyhow!("reload parse: {e}"))?;
                store
                    .replace_policies(&records)
                    .await
                    .map_err(|e| anyhow::anyhow!("reload apply: {e}"))?;
                Ok(records.len())
            })
        })
        .await?;
    // Detach: handle aborts on drop, and we want the subscriber to live
    // for the lifetime of the process.
    std::mem::forget(_handle);
    Ok(())
}

// ── Action markers ───────────────────────────────────────────────

macro_rules! action_marker {
    ($name:ident, $action_id:expr) => {
        pub struct $name;
        impl AuthzAction for $name {
            fn action_uid() -> EntityUid {
                uid("Action", $action_id)
            }
        }
    };
}

action_marker!(RotateJwks, "rotate_jwks");
action_marker!(RetireJwks, "retire_jwks");
action_marker!(ScimProvisionUser, "scim_provision_user");
action_marker!(ScimDeprovisionUser, "scim_provision_user");
action_marker!(ScimProvisionGroup, "scim_provision_group");

// ── Resource extractors ──────────────────────────────────────────

/// Stand-in resource for JWKS rotation / retirement — admin endpoints
/// operate on the *current* signing bundle, not on a single kid.
pub struct JwksKeyResource;

impl AuthzResource for JwksKeyResource {
    fn resource_uid(&self) -> EntityUid {
        uid("JwksKey", "_active")
    }

    fn resource_entities(&self) -> Vec<Entity> {
        let attrs = [
            (
                "kid".to_string(),
                RestrictedExpression::new_string("_active".into()),
            ),
            (
                "status".to_string(),
                RestrictedExpression::new_string("active".into()),
            ),
        ]
        .into_iter()
        .collect();
        vec![
            Entity::new(self.resource_uid(), attrs, HashSet::new())
                .expect("static JwksKey attrs are valid"),
        ]
    }
}

impl<S> FromRequestParts<S> for JwksKeyResource
where
    S: Send + Sync,
{
    type Rejection = std::convert::Infallible;
    async fn from_request_parts(_p: &mut Parts, _s: &S) -> Result<Self, Self::Rejection> {
        Ok(JwksKeyResource)
    }
}

/// Stand-in resource for SCIM user provisioning / deprovisioning.
pub struct ScimUserResource;

impl AuthzResource for ScimUserResource {
    fn resource_uid(&self) -> EntityUid {
        uid("ScimUser", "_pool")
    }

    fn resource_entities(&self) -> Vec<Entity> {
        let attrs = [(
            "id".to_string(),
            RestrictedExpression::new_string("_pool".into()),
        )]
        .into_iter()
        .collect();
        vec![
            Entity::new(self.resource_uid(), attrs, HashSet::new())
                .expect("static ScimUser attrs are valid"),
        ]
    }
}

impl<S> FromRequestParts<S> for ScimUserResource
where
    S: Send + Sync,
{
    type Rejection = std::convert::Infallible;
    async fn from_request_parts(_p: &mut Parts, _s: &S) -> Result<Self, Self::Rejection> {
        Ok(ScimUserResource)
    }
}

/// Stand-in resource for SCIM group provisioning.
pub struct ScimGroupResource;

impl AuthzResource for ScimGroupResource {
    fn resource_uid(&self) -> EntityUid {
        uid("ScimGroup", "_pool")
    }

    fn resource_entities(&self) -> Vec<Entity> {
        let attrs = [(
            "id".to_string(),
            RestrictedExpression::new_string("_pool".into()),
        )]
        .into_iter()
        .collect();
        vec![
            Entity::new(self.resource_uid(), attrs, HashSet::new())
                .expect("static ScimGroup attrs are valid"),
        ]
    }
}

impl<S> FromRequestParts<S> for ScimGroupResource
where
    S: Send + Sync,
{
    type Rejection = std::convert::Infallible;
    async fn from_request_parts(_p: &mut Parts, _s: &S) -> Result<Self, Self::Rejection> {
        Ok(ScimGroupResource)
    }
}

// ── Guard ─────────────────────────────────────────────────────────

/// Service-local Cedar guard.
///
/// Differs from `authz_cedar::axum::AuthzGuard` only in how the
/// principal is hydrated: this guard reads `kind` (e.g.
/// `service_account` / `human`), `mfa_age_secs` (Long) and `groups`
/// (array of strings) from `claims.attributes`, and propagates each
/// role / group into the principal's parent UID set so policies that
/// say `principal in Group::"…"` / `principal in Role::"…"` resolve.
///
/// Returns:
///   - `403 Forbidden` on engine deny.
///   - `401 Unauthorized` if no `Claims` are in extensions.
///   - `500 Internal Server Error` for engine / hydration errors.
pub struct AdminAuthzGuard<A, R> {
    pub claims: Claims,
    pub resource: R,
    _action: PhantomData<fn() -> A>,
}

impl<S, A, R> FromRequestParts<S> for AdminAuthzGuard<A, R>
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
            .get::<Arc<AuthzEngine>>()
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

        let (principal_entity, parent_entities) = principal_entities_from_claims(&claims);
        let principal_uid = principal_entity.uid();
        let resource_uid = resource.resource_uid();

        let mut entity_set = vec![principal_entity];
        entity_set.extend(parent_entities);
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

        Ok(AdminAuthzGuard {
            claims,
            resource,
            _action: PhantomData,
        })
    }
}

/// Build the principal `User` entity plus its referenced `Group` /
/// `Role` parent entities from a JWT `Claims`.
///
/// Inputs read off `claims`:
///   - `sub`, `org_id`, `roles`, `session_scope.allowed_markings` —
///     baseline ABAC fields (mirror of
///     `authz_cedar::axum::principal_entity_from_claims`).
///   - `attributes.kind`        → `principal.kind`
///     (typically `"service_account"` or `"human"`).
///   - `attributes.mfa_age_secs`→ `principal.mfa_age_secs` (Long).
///   - `attributes.groups: [..]`→ each value becomes a `Group::"<id>"`
///     parent UID *and* an emitted `Group` entity so Cedar's
///     `principal in Group::"…"` resolves without external lookups.
///
/// `claims.roles` additionally contributes `Role::"<role>"` parent
/// UIDs (and matching `Role` entities), letting policies write
/// `principal in Role::"scim_writer"`.
pub fn principal_entities_from_claims(claims: &Claims) -> (Entity, Vec<Entity>) {
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

    let roles_set: Vec<RestrictedExpression> = claims
        .roles
        .iter()
        .map(|r| RestrictedExpression::new_string(r.clone()))
        .collect();

    let mut attrs = vec![
        (
            "tenant".to_string(),
            RestrictedExpression::new_string(tenant),
        ),
        (
            "clearances".to_string(),
            RestrictedExpression::new_set(clearances),
        ),
        (
            "roles".to_string(),
            RestrictedExpression::new_set(roles_set),
        ),
    ];

    if let Some(kind) = claims
        .attributes
        .get("kind")
        .and_then(Value::as_str)
        .filter(|v| !v.is_empty())
    {
        attrs.push((
            "kind".to_string(),
            RestrictedExpression::new_string(kind.to_string()),
        ));
    }
    if let Some(age) = claims
        .attributes
        .get("mfa_age_secs")
        .and_then(Value::as_i64)
    {
        attrs.push((
            "mfa_age_secs".to_string(),
            RestrictedExpression::new_long(age),
        ));
    }

    let claim_groups: Vec<String> = claims
        .attributes
        .get("groups")
        .and_then(Value::as_array)
        .map(|arr| {
            arr.iter()
                .filter_map(|v| v.as_str().map(str::to_string))
                .collect()
        })
        .unwrap_or_default();

    let mut parents = HashSet::new();
    let mut parent_entities: Vec<Entity> = Vec::new();
    for group_id in &claim_groups {
        let g_uid = uid("Group", group_id);
        parents.insert(g_uid.clone());
        let g_attrs = [(
            "id".to_string(),
            RestrictedExpression::new_string(group_id.clone()),
        )]
        .into_iter()
        .collect();
        parent_entities.push(
            Entity::new(g_uid, g_attrs, HashSet::new()).expect("static Group attrs are valid"),
        );
    }
    for role in &claims.roles {
        let r_uid = uid("Role", role);
        parents.insert(r_uid.clone());
        let r_attrs = [(
            "id".to_string(),
            RestrictedExpression::new_string(role.clone()),
        )]
        .into_iter()
        .collect();
        parent_entities.push(
            Entity::new(r_uid, r_attrs, HashSet::new()).expect("static Role attrs are valid"),
        );
    }

    let user_entity = Entity::new(user_uid, attrs.into_iter().collect(), parents)
        .expect("static User attrs are valid");

    (user_entity, parent_entities)
}
