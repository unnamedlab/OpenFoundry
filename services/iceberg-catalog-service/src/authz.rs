//! D1.1.8 P3 — Cedar authorization layer for `iceberg-catalog-service`.
//!
//! Sits between the bearer extractor (`handlers::auth::bearer`) and
//! every spec / admin handler:
//!
//!   1. The bearer extractor produces an
//!      [`AuthenticatedPrincipal`] (subject + scopes + tenant flag).
//!   2. This module turns the principal into a Cedar entity (`User`
//!      *or* `ServicePrincipal`), the resource into the matching
//!      `IcebergNamespace` / `IcebergTable` entity, and runs
//!      [`PolicyStore::is_authorized`] from `authz-cedar`.
//!   3. On `Decision::Deny` the handler returns 403 + emits an
//!      `iceberg.access.denied` audit event labelled with the reason
//!      (`missing_clearance` / `missing_scope` / `missing_role`).
//!
//! The bridge stays catalog-internal so the Cedar dependency doesn't
//! leak into `auth-middleware` or `core-models`.

use std::collections::HashSet;
use std::sync::Arc;

use authz_cedar::{AuthzEngine, PolicyStore};
use cedar_policy::{
    Context, Entities, Entity, EntityId, EntityTypeName, EntityUid, RestrictedExpression,
};
use std::str::FromStr;
use uuid::Uuid;

use crate::audit;
use crate::domain::namespace::Namespace;
use crate::domain::table::IcebergTable;
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;

/// What kind of caller we built the Cedar entity for. The bearer
/// extractor sources the answer from token shape (`ofty_*` long-lived
/// tokens are always `User`; OAuth2 client_credentials JWTs are
/// `ServicePrincipal`). Inferable from the scopes set.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PrincipalKind {
    User,
    ServicePrincipal,
}

impl PrincipalKind {
    pub fn as_str(self) -> &'static str {
        match self {
            PrincipalKind::User => "User",
            PrincipalKind::ServicePrincipal => "ServicePrincipal",
        }
    }
}

/// Resource pinned in the Cedar request. Each variant carries the
/// fully-hydrated set of attributes the bundled iceberg policies
/// inspect.
#[derive(Debug, Clone)]
pub enum AuthzResource {
    Namespace(NamespaceAttrs),
    Table(TableAttrs),
}

#[derive(Debug, Clone)]
pub struct NamespaceAttrs {
    pub rid: String,
    pub project_rid: String,
    pub tenant: String,
    pub name: String,
    pub markings: Vec<String>,
}

#[derive(Debug, Clone)]
pub struct TableAttrs {
    pub rid: String,
    pub namespace_rid: String,
    pub tenant: String,
    pub format_version: i32,
    pub markings: Vec<String>,
    pub explicit_markings: Vec<String>,
}

impl NamespaceAttrs {
    pub fn from_namespace(namespace: &Namespace, markings: Vec<String>, tenant: &str) -> Self {
        Self {
            rid: format!("ri.foundry.main.iceberg-namespace.{}", namespace.id),
            project_rid: namespace.project_rid.clone(),
            tenant: tenant.to_string(),
            name: namespace.name.clone(),
            markings,
        }
    }
}

impl TableAttrs {
    pub fn from_table(
        table: &IcebergTable,
        markings: Vec<String>,
        explicit: Vec<String>,
        tenant: &str,
    ) -> Self {
        Self {
            rid: table.rid.clone(),
            namespace_rid: format!(
                "ri.foundry.main.iceberg-namespace.{}",
                table.namespace_id
            ),
            tenant: tenant.to_string(),
            format_version: table.format_version,
            markings,
            explicit_markings: explicit,
        }
    }
}

/// Reason for an `iceberg.access.denied` audit event. Mirrors the
/// metric label so dashboards can split by reason.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DenialReason {
    MissingClearance,
    MissingScope,
    MissingRole,
    OutOfTenant,
    Unknown,
}

impl DenialReason {
    pub fn as_str(self) -> &'static str {
        match self {
            DenialReason::MissingClearance => "missing_clearance",
            DenialReason::MissingScope => "missing_scope",
            DenialReason::MissingRole => "missing_role",
            DenialReason::OutOfTenant => "out_of_tenant",
            DenialReason::Unknown => "unknown",
        }
    }
}

/// The handler-side entry point. Builds the request, evaluates the
/// store, on deny emits audit + a 403 ApiError.
pub async fn enforce(
    engine: &AuthzEngine,
    principal: &AuthenticatedPrincipal,
    kind: PrincipalKind,
    action_name: &str,
    resource: &AuthzResource,
    tenant: &str,
) -> Result<(), ApiError> {
    let principal_uid = principal_entity_uid(principal, kind);
    let action_uid = action_uid(action_name);
    let resource_uid = resource_entity_uid(resource);

    let mut entities_vec = Vec::new();
    entities_vec.push(build_principal_entity(principal, kind, tenant)?);
    entities_vec.extend(build_resource_entities(resource)?);

    let entities = Entities::from_entities(entities_vec, Some(&engine.store().schema()))
        .map_err(|err| ApiError::Internal(format!("authz entity hydration: {err}")))?;
    let allowed = engine
        .store()
        .is_allowed(
            principal_uid,
            action_uid,
            resource_uid,
            Context::empty(),
            &entities,
        )
        .await
        .map_err(|err| ApiError::Internal(format!("authz evaluation: {err}")))?;

    if allowed {
        Ok(())
    } else {
        let reason = infer_denial_reason(principal, kind, action_name, resource);
        audit::access_denied(
            principal_subject(principal),
            target_rid(resource),
            action_name,
            reason.as_str(),
        );
        Err(ApiError::Forbidden(format!(
            "iceberg authz denied for `{action_name}` ({reason})",
            reason = reason.as_str()
        )))
    }
}

fn principal_entity_uid(principal: &AuthenticatedPrincipal, kind: PrincipalKind) -> EntityUid {
    let type_name = EntityTypeName::from_str(kind.as_str()).expect("entity type valid");
    let id = EntityId::new(&principal.subject);
    EntityUid::from_type_name_and_id(type_name, id)
}

fn action_uid(action_name: &str) -> EntityUid {
    let type_name = EntityTypeName::from_str("Action").expect("Action type valid");
    let id = EntityId::new(action_name);
    EntityUid::from_type_name_and_id(type_name, id)
}

fn resource_entity_uid(resource: &AuthzResource) -> EntityUid {
    let (kind, rid) = match resource {
        AuthzResource::Namespace(n) => ("IcebergNamespace", n.rid.clone()),
        AuthzResource::Table(t) => ("IcebergTable", t.rid.clone()),
    };
    let type_name = EntityTypeName::from_str(kind).expect("entity type valid");
    let id = EntityId::new(&rid);
    EntityUid::from_type_name_and_id(type_name, id)
}

fn build_principal_entity(
    principal: &AuthenticatedPrincipal,
    kind: PrincipalKind,
    tenant: &str,
) -> Result<Entity, ApiError> {
    let principal_uid = principal_entity_uid(principal, kind);

    let mut attrs: std::collections::HashMap<String, RestrictedExpression> = Default::default();
    attrs.insert(
        "tenant".to_string(),
        RestrictedExpression::new_string(tenant.to_string()),
    );

    // Both User and ServicePrincipal carry a `clearances: Set<Marking>`.
    let clearances = principal_clearances(principal);
    let marking_set: Vec<RestrictedExpression> = clearances
        .iter()
        .map(|name| {
            let type_name = EntityTypeName::from_str("Marking").expect("Marking type valid");
            let id = EntityId::new(name);
            RestrictedExpression::new_entity_uid(EntityUid::from_type_name_and_id(type_name, id))
        })
        .collect();
    attrs.insert(
        "clearances".to_string(),
        RestrictedExpression::new_set(marking_set),
    );

    match kind {
        PrincipalKind::User => {
            // `roles: Set<String>` and the optional fields the schema
            // declares. Inferred from the principal's scope set: the
            // bearer extractor decides admin/editor/viewer at the
            // call-site (see `enforce_for_method`); here we surface
            // them through the scope namespace `role:<value>` so a
            // policy can match `principal.roles.contains("admin")`.
            let roles: Vec<RestrictedExpression> = principal
                .scopes
                .iter()
                .filter_map(|scope| scope.strip_prefix("role:"))
                .map(|role| RestrictedExpression::new_string(role.to_string()))
                .collect();
            attrs.insert("roles".to_string(), RestrictedExpression::new_set(roles));
        }
        PrincipalKind::ServicePrincipal => {
            // ServicePrincipal in the bundled schema requires `rid`
            // and `project_scope_rids`. The catalog doesn't yet model
            // the project-scope mapping, so we surface an empty set
            // — iceberg policies don't inspect it.
            attrs.insert(
                "rid".to_string(),
                RestrictedExpression::new_string(principal.subject.clone()),
            );
            attrs.insert(
                "project_scope_rids".to_string(),
                RestrictedExpression::new_set(Vec::<RestrictedExpression>::new()),
            );
        }
    }

    Entity::new(principal_uid, attrs, HashSet::new())
        .map_err(|err| ApiError::Internal(format!("principal entity hydration: {err}")))
}

fn build_resource_entities(resource: &AuthzResource) -> Result<Vec<Entity>, ApiError> {
    match resource {
        AuthzResource::Namespace(ns) => Ok(vec![namespace_entity(ns)?]),
        AuthzResource::Table(t) => {
            // Tables reference the parent namespace via Cedar's
            // `in [IcebergNamespace]` parent group. We need a stub
            // entity for the namespace_rid so `principal in namespace`
            // queries don't fail; the resource itself carries the
            // markings so policies don't need to dereference parents.
            let namespace_uid = {
                let type_name = EntityTypeName::from_str("IcebergNamespace")
                    .expect("namespace type valid");
                let id = EntityId::new(&t.namespace_rid);
                EntityUid::from_type_name_and_id(type_name, id)
            };
            let namespace_stub = Entity::new(
                namespace_uid.clone(),
                {
                    let mut a: std::collections::HashMap<String, RestrictedExpression> =
                        Default::default();
                    a.insert(
                        "rid".to_string(),
                        RestrictedExpression::new_string(t.namespace_rid.clone()),
                    );
                    a.insert(
                        "tenant".to_string(),
                        RestrictedExpression::new_string(t.tenant.clone()),
                    );
                    a.insert(
                        "project_rid".to_string(),
                        RestrictedExpression::new_string(String::new()),
                    );
                    a.insert(
                        "name".to_string(),
                        RestrictedExpression::new_string(String::new()),
                    );
                    a.insert(
                        "markings".to_string(),
                        RestrictedExpression::new_set(Vec::<RestrictedExpression>::new()),
                    );
                    a
                },
                HashSet::new(),
            )
            .map_err(|err| ApiError::Internal(format!("namespace stub: {err}")))?;
            Ok(vec![namespace_stub, table_entity(t, namespace_uid)?])
        }
    }
}

fn namespace_entity(ns: &NamespaceAttrs) -> Result<Entity, ApiError> {
    let uid = {
        let type_name =
            EntityTypeName::from_str("IcebergNamespace").expect("namespace type valid");
        let id = EntityId::new(&ns.rid);
        EntityUid::from_type_name_and_id(type_name, id)
    };
    let mut attrs: std::collections::HashMap<String, RestrictedExpression> = Default::default();
    attrs.insert(
        "rid".to_string(),
        RestrictedExpression::new_string(ns.rid.clone()),
    );
    attrs.insert(
        "tenant".to_string(),
        RestrictedExpression::new_string(ns.tenant.clone()),
    );
    attrs.insert(
        "project_rid".to_string(),
        RestrictedExpression::new_string(ns.project_rid.clone()),
    );
    attrs.insert(
        "name".to_string(),
        RestrictedExpression::new_string(ns.name.clone()),
    );
    attrs.insert(
        "markings".to_string(),
        RestrictedExpression::new_set(marking_set(&ns.markings)),
    );
    Entity::new(uid, attrs, HashSet::new())
        .map_err(|err| ApiError::Internal(format!("namespace entity: {err}")))
}

fn table_entity(t: &TableAttrs, namespace_uid: EntityUid) -> Result<Entity, ApiError> {
    let uid = {
        let type_name = EntityTypeName::from_str("IcebergTable").expect("table type valid");
        let id = EntityId::new(&t.rid);
        EntityUid::from_type_name_and_id(type_name, id)
    };
    let mut attrs: std::collections::HashMap<String, RestrictedExpression> = Default::default();
    attrs.insert(
        "rid".to_string(),
        RestrictedExpression::new_string(t.rid.clone()),
    );
    attrs.insert(
        "tenant".to_string(),
        RestrictedExpression::new_string(t.tenant.clone()),
    );
    attrs.insert(
        "namespace_rid".to_string(),
        RestrictedExpression::new_string(t.namespace_rid.clone()),
    );
    attrs.insert(
        "format_version".to_string(),
        RestrictedExpression::new_long(t.format_version as i64),
    );
    attrs.insert(
        "markings".to_string(),
        RestrictedExpression::new_set(marking_set(&t.markings)),
    );
    attrs.insert(
        "explicit_markings".to_string(),
        RestrictedExpression::new_set(marking_set(&t.explicit_markings)),
    );
    let mut parents = HashSet::new();
    parents.insert(namespace_uid);
    Entity::new(uid, attrs, parents)
        .map_err(|err| ApiError::Internal(format!("table entity: {err}")))
}

fn marking_set(markings: &[String]) -> Vec<RestrictedExpression> {
    markings
        .iter()
        .map(|name| {
            let type_name = EntityTypeName::from_str("Marking").expect("Marking type valid");
            let id = EntityId::new(name);
            RestrictedExpression::new_entity_uid(EntityUid::from_type_name_and_id(type_name, id))
        })
        .collect()
}

/// Effective clearances for a principal: union of token-level
/// `iceberg-clearance:<name>` scopes and any `role:admin` blanket.
fn principal_clearances(principal: &AuthenticatedPrincipal) -> Vec<String> {
    let mut out: Vec<String> = principal
        .scopes
        .iter()
        .filter_map(|scope| scope.strip_prefix("iceberg-clearance:"))
        .map(|s| s.to_string())
        .collect();
    if principal
        .scopes
        .iter()
        .any(|s| s == "role:admin" || s == "iceberg-clearance:*")
    {
        // Admin / wildcard surfaces the standard ladder so most
        // policies are trivially satisfied. The marker `*` is left
        // visible for diagnostics.
        out.extend([
            "public".to_string(),
            "confidential".to_string(),
            "pii".to_string(),
            "restricted".to_string(),
            "secret".to_string(),
        ]);
    }
    out.sort();
    out.dedup();
    out
}

fn infer_denial_reason(
    principal: &AuthenticatedPrincipal,
    _kind: PrincipalKind,
    action_name: &str,
    resource: &AuthzResource,
) -> DenialReason {
    let scope_required = action_name.contains("write")
        || action_name.contains("alter")
        || action_name.contains("drop")
        || action_name.contains("create");
    let has_write_scope = principal.scopes.contains("api:iceberg-write");
    if scope_required && !has_write_scope {
        return DenialReason::MissingScope;
    }
    let cleared: Vec<_> = principal_clearances(principal);
    let resource_markings = match resource {
        AuthzResource::Namespace(n) => &n.markings,
        AuthzResource::Table(t) => &t.markings,
    };
    if !resource_markings.iter().all(|m| cleared.contains(m)) {
        return DenialReason::MissingClearance;
    }
    if action_name.contains("manage_markings") || action_name.contains("drop") {
        return DenialReason::MissingRole;
    }
    DenialReason::Unknown
}

fn principal_subject(principal: &AuthenticatedPrincipal) -> Uuid {
    Uuid::parse_str(&principal.subject).unwrap_or_else(|_| Uuid::nil())
}

fn target_rid(resource: &AuthzResource) -> &str {
    match resource {
        AuthzResource::Namespace(n) => &n.rid,
        AuthzResource::Table(t) => &t.rid,
    }
}

/// Build a fresh [`AuthzEngine`] from the bundled iceberg policies.
/// Used by the binary at boot.
pub async fn bootstrap_engine() -> Arc<AuthzEngine> {
    let store = match PolicyStore::with_policies(
        &authz_cedar::iceberg_policies::all_iceberg_policies(),
    )
    .await
    {
        Ok(s) => s,
        Err(err) => {
            tracing::error!(?err, "iceberg cedar policies failed to load; using empty store");
            PolicyStore::empty().expect("schema parses")
        }
    };
    Arc::new(AuthzEngine::with_noop_audit(store))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet as StdSet;

    #[test]
    fn admin_role_expands_clearance_ladder() {
        let p = AuthenticatedPrincipal {
            subject: "u".to_string(),
            scopes: StdSet::from_iter(["role:admin".to_string()]),
        };
        let clearances = principal_clearances(&p);
        for needed in ["public", "confidential", "pii", "restricted", "secret"] {
            assert!(
                clearances.iter().any(|c| c == needed),
                "missing {needed} in {clearances:?}"
            );
        }
    }

    #[test]
    fn denial_reason_prefers_scope_over_clearance() {
        let p = AuthenticatedPrincipal {
            subject: "u".to_string(),
            scopes: StdSet::from_iter([
                "api:iceberg-read".to_string(),
                "iceberg-clearance:public".to_string(),
            ]),
        };
        let resource = AuthzResource::Table(TableAttrs {
            rid: "t".to_string(),
            namespace_rid: "n".to_string(),
            tenant: "t".to_string(),
            format_version: 2,
            markings: vec!["pii".to_string()],
            explicit_markings: vec![],
        });
        let r = infer_denial_reason(&p, PrincipalKind::User, "iceberg::table::write_data", &resource);
        assert_eq!(r, DenialReason::MissingScope);
    }

    #[test]
    fn missing_clearance_detected_when_scope_present() {
        let p = AuthenticatedPrincipal {
            subject: "u".to_string(),
            scopes: StdSet::from_iter([
                "api:iceberg-write".to_string(),
                "iceberg-clearance:public".to_string(),
            ]),
        };
        let resource = AuthzResource::Table(TableAttrs {
            rid: "t".to_string(),
            namespace_rid: "n".to_string(),
            tenant: "t".to_string(),
            format_version: 2,
            markings: vec!["pii".to_string()],
            explicit_markings: vec![],
        });
        let r = infer_denial_reason(&p, PrincipalKind::User, "iceberg::table::write_data", &resource);
        assert_eq!(r, DenialReason::MissingClearance);
    }
}
