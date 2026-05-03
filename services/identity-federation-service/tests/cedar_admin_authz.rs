//! S3.1.i — End-to-end checks for the bundled Cedar admin policies.
//!
//! Loads `services/identity-federation-service/policies/identity_admin.cedar`
//! through `cedar_authz::bootstrap_engine` (the same code path the bin
//! takes) and exercises [`AuthzEngine::authorize`] directly. The tests
//! are deliberately HTTP-free: they validate the policy logic, not the
//! Axum extractor pipeline.
//!
//! Scenarios covered:
//!   1. A user in `Group::"IdentityKeyRotators"` with a fresh
//!      `mfa_age_secs` (60s) is allowed to `rotate_jwks`.
//!   2. A `human` principal carrying the `scim_writer` role is denied
//!      `scim_provision_user` (the explicit `forbid` rule fires).
//!   3. A user *without* `Group::"IdentityKeyRotators"` is denied
//!      `rotate_jwks` (no permit fires).

use std::collections::HashSet;

use authz_cedar::axum::uid;
use cedar_policy::{Context, Decision, Entities, Entity, EntityUid, RestrictedExpression};
use identity_federation_service::cedar_authz;

/// Helper: build a `User` entity with optional `kind` / `mfa_age_secs`
/// attrs and a list of `Group` parent UIDs.
fn build_user(
    sub: &str,
    kind: Option<&str>,
    mfa_age_secs: Option<i64>,
    group_ids: &[&str],
    role_ids: &[&str],
) -> (Entity, Vec<Entity>, EntityUid) {
    let user_uid = uid("User", sub);
    let mut attrs: Vec<(String, RestrictedExpression)> = vec![
        (
            "tenant".into(),
            RestrictedExpression::new_string(String::new()),
        ),
        (
            "clearances".into(),
            RestrictedExpression::new_set(Vec::<RestrictedExpression>::new()),
        ),
        (
            "roles".into(),
            RestrictedExpression::new_set(
                role_ids
                    .iter()
                    .map(|r| RestrictedExpression::new_string((*r).to_string()))
                    .collect::<Vec<_>>(),
            ),
        ),
    ];
    if let Some(k) = kind {
        attrs.push((
            "kind".into(),
            RestrictedExpression::new_string(k.to_string()),
        ));
    }
    if let Some(age) = mfa_age_secs {
        attrs.push((
            "mfa_age_secs".into(),
            RestrictedExpression::new_long(age),
        ));
    }

    let mut parents: HashSet<EntityUid> = HashSet::new();
    let mut parent_entities: Vec<Entity> = Vec::new();
    for g in group_ids {
        let g_uid = uid("Group", g);
        parents.insert(g_uid.clone());
        let g_attrs = [(
            "id".to_string(),
            RestrictedExpression::new_string((*g).to_string()),
        )]
        .into_iter()
        .collect();
        parent_entities
            .push(Entity::new(g_uid, g_attrs, HashSet::new()).expect("group attrs are valid"));
    }
    for r in role_ids {
        let r_uid = uid("Role", r);
        parents.insert(r_uid.clone());
        let r_attrs = [(
            "id".to_string(),
            RestrictedExpression::new_string((*r).to_string()),
        )]
        .into_iter()
        .collect();
        parent_entities
            .push(Entity::new(r_uid, r_attrs, HashSet::new()).expect("role attrs are valid"));
    }

    let user = Entity::new(
        user_uid.clone(),
        attrs.into_iter().collect(),
        parents,
    )
    .expect("user attrs are valid");

    (user, parent_entities, user_uid)
}

fn jwks_resource() -> (Entity, EntityUid) {
    let r_uid = uid("JwksKey", "_active");
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
    let entity = Entity::new(r_uid.clone(), attrs, HashSet::new())
        .expect("jwks attrs are valid");
    (entity, r_uid)
}

fn scim_user_resource() -> (Entity, EntityUid) {
    let r_uid = uid("ScimUser", "_pool");
    let attrs = [(
        "id".to_string(),
        RestrictedExpression::new_string("_pool".into()),
    )]
    .into_iter()
    .collect();
    let entity = Entity::new(r_uid.clone(), attrs, HashSet::new())
        .expect("scim user attrs are valid");
    (entity, r_uid)
}

#[tokio::test]
async fn rotate_jwks_allowed_for_group_member_with_recent_mfa() {
    let engine = cedar_authz::bootstrap_engine()
        .await
        .expect("engine boots");

    let (user, mut parent_entities, principal_uid) = build_user(
        "00000000-0000-0000-0000-000000000001",
        None,
        Some(60),
        &["IdentityKeyRotators"],
        &[],
    );
    let (resource, resource_uid) = jwks_resource();
    let mut entity_set = vec![user];
    entity_set.append(&mut parent_entities);
    entity_set.push(resource);

    let entities = Entities::from_entities(entity_set, Some(&engine.store().schema()))
        .expect("entities hydrate");

    let outcome = engine
        .authorize(
            principal_uid,
            uid("Action", "rotate_jwks"),
            resource_uid,
            Context::empty(),
            &entities,
        )
        .await
        .expect("authorize succeeds");

    assert_eq!(
        outcome.decision,
        Decision::Allow,
        "rotate_jwks should allow IdentityKeyRotators with fresh MFA; diagnostics={:?}",
        outcome.diagnostics
    );
}

#[tokio::test]
async fn scim_provision_user_denied_for_human_principal() {
    let engine = cedar_authz::bootstrap_engine()
        .await
        .expect("engine boots");

    // Human principal — even with the `scim_writer` role, the explicit
    // `forbid` clause must fire and block provisioning.
    let (user, mut parent_entities, principal_uid) = build_user(
        "00000000-0000-0000-0000-000000000002",
        Some("human"),
        None,
        &[],
        &["scim_writer"],
    );
    let (resource, resource_uid) = scim_user_resource();
    let mut entity_set = vec![user];
    entity_set.append(&mut parent_entities);
    entity_set.push(resource);

    let entities = Entities::from_entities(entity_set, Some(&engine.store().schema()))
        .expect("entities hydrate");

    let outcome = engine
        .authorize(
            principal_uid,
            uid("Action", "scim_provision_user"),
            resource_uid,
            Context::empty(),
            &entities,
        )
        .await
        .expect("authorize succeeds");

    assert_eq!(
        outcome.decision,
        Decision::Deny,
        "scim_provision_user must reject `kind=human` principals; diagnostics={:?}",
        outcome.diagnostics
    );
}

#[tokio::test]
async fn rotate_jwks_denied_without_identity_key_rotators_membership() {
    let engine = cedar_authz::bootstrap_engine()
        .await
        .expect("engine boots");

    // No groups, MFA fresh — but rotate policy requires explicit
    // membership in `Group::"IdentityKeyRotators"`. Without it, no
    // permit fires and the engine defaults to deny (HTTP-side that's
    // what the AuthzGuard turns into a 403).
    let (user, mut parent_entities, principal_uid) = build_user(
        "00000000-0000-0000-0000-000000000003",
        None,
        Some(60),
        &[],
        &[],
    );
    let (resource, resource_uid) = jwks_resource();
    let mut entity_set = vec![user];
    entity_set.append(&mut parent_entities);
    entity_set.push(resource);

    let entities = Entities::from_entities(entity_set, Some(&engine.store().schema()))
        .expect("entities hydrate");

    let outcome = engine
        .authorize(
            principal_uid,
            uid("Action", "rotate_jwks"),
            resource_uid,
            Context::empty(),
            &entities,
        )
        .await
        .expect("authorize succeeds");

    assert_eq!(
        outcome.decision,
        Decision::Deny,
        "non-rotator principals must be denied (HTTP-equivalent: 403); diagnostics={:?}",
        outcome.diagnostics
    );
}
