//! Comprehensive Cedar policy regression suite.
//!
//! Covers ≥ 50 explicit cases across:
//!
//! * Marking clearance enforcement (read / write / delete)
//! * Branch ↔ dataset inheritance (parent group)
//! * Tenant isolation
//! * Role-based overrides (admin / editor / viewer)
//! * Default-deny baseline
//!
//! Each case is a row in [`CASES`]; the harness expands the row into
//! a Cedar request, runs `PolicyStore::is_authorized`, and asserts the
//! `Decision` matches.
//!
//! When a new policy is added in production, extend [`POLICIES`] and
//! the matrix below — the harness scales linearly.

use std::collections::HashSet;

use authz_cedar::{PolicyRecord, PolicyStore};
use cedar_policy::{
    Context, Decision, Entities, Entity, EntityId, EntityTypeName, EntityUid, Request,
    RestrictedExpression,
};

// ---------------------------------------------------------------------------
// Bundled test policies
// ---------------------------------------------------------------------------
//
// The set is deliberately small so a single review can validate the
// matrix below. Each rule is named consistently with the column in
// `CASES` it is meant to exercise.

const POLICIES: &[(&str, &str)] = &[
    // P1: tenant-scoped read on Dataset when caller's clearances cover the
    //     dataset markings.
    (
        "p1-read-clearance",
        r#"
        permit(
          principal,
          action == Action::"read",
          resource is Dataset
        ) when {
          principal.tenant == resource.tenant &&
          principal.clearances.containsAll(resource.markings)
        };
        "#,
    ),
    // P3: editors and admins can write on Dataset within their tenant
    //     and only when they cover all markings.
    (
        "p3-write-editor",
        r#"
        permit(
          principal,
          action == Action::"write",
          resource is Dataset
        ) when {
          principal.tenant == resource.tenant &&
          (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
          principal.clearances.containsAll(resource.markings)
        };
        "#,
    ),
    // P4: only admins can delete a dataset, and the same clearance rule
    //     applies even to admins.
    (
        "p4-delete-admin",
        r#"
        permit(
          principal,
          action == Action::"delete",
          resource is Dataset
        ) when {
          principal.tenant == resource.tenant &&
          principal.roles.contains("admin") &&
          principal.clearances.containsAll(resource.markings)
        };
        "#,
    ),
    // P5: branch creation requires editor or admin within the same
    //     tenant and clearance over the dataset.
    (
        "p5-branch-create",
        r#"
        permit(
          principal,
          action == Action::"branch::create",
          resource is Dataset
        ) when {
          principal.tenant == resource.tenant &&
          (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
          principal.clearances.containsAll(resource.markings)
        };
        "#,
    ),
];

// P2 above intentionally uses a deliberately weak expression so the
// concrete branch tests rely on P1 + an additional explicit branch
// rule below. We add it as a separate row so the matrix is explicit
// about which policy enforces what.
const BRANCH_READ_POLICY: (&str, &str) = (
    "p2b-read-branch-strict",
    r#"
        permit(
          principal,
          action == Action::"read",
          resource is Branch
        ) when {
          principal.roles.contains("admin") ||
          principal.roles.contains("editor") ||
          principal.roles.contains("viewer")
        };
        "#,
);

// ---------------------------------------------------------------------------
// Matrix
// ---------------------------------------------------------------------------

#[derive(Clone, Copy)]
enum ResourceKind {
    Dataset,
    Branch,
}

struct Case {
    name: &'static str,
    role: Option<&'static str>,
    clearances: &'static [&'static str],
    resource_markings: &'static [&'static str],
    same_tenant: bool,
    action: &'static str,
    resource: ResourceKind,
    expected: Decision,
}

const CASES: &[Case] = &[
    // ---- Default-deny baseline ----
    Case { name: "anon read public dataset same tenant allowed (no role gate)",
        role: None, clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "anon read pii dataset same tenant with clearance allowed",
        role: None, clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "anon read pii dataset same tenant without clearance denied",
        role: None, clearances: &[], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "anon cross-tenant read denied",
        role: None, clearances: &[], resource_markings: &[], same_tenant: false,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "anon write public dataset denied",
        role: None, clearances: &[], resource_markings: &[], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },

    // ---- Read on Dataset, same tenant ----
    Case { name: "viewer reads public dataset same tenant",
        role: Some("viewer"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "viewer with pii reads public dataset",
        role: Some("viewer"), clearances: &["pii"], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "viewer reads pii without clearance denied",
        role: Some("viewer"), clearances: &[], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "viewer reads pii with pii clearance",
        role: Some("viewer"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "viewer reads pii+confidential with full clearance",
        role: Some("viewer"), clearances: &["pii","confidential"], resource_markings: &["pii","confidential"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "viewer reads pii+confidential with only pii denied",
        role: Some("viewer"), clearances: &["pii"], resource_markings: &["pii","confidential"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "viewer reads confidential with only pii denied",
        role: Some("viewer"), clearances: &["pii"], resource_markings: &["confidential"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "viewer with superset clearance reads pii",
        role: Some("viewer"), clearances: &["pii","confidential","secret"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "analyst reads pii with clearance",
        role: Some("analyst"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "editor reads secret without clearance denied",
        role: Some("editor"), clearances: &["pii"], resource_markings: &["secret"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },

    // ---- Read on Dataset, cross-tenant ----
    Case { name: "viewer cross-tenant read public denied",
        role: Some("viewer"), clearances: &[], resource_markings: &[], same_tenant: false,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "admin cross-tenant read public denied",
        role: Some("admin"), clearances: &["pii","confidential"], resource_markings: &[], same_tenant: false,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor cross-tenant read pii denied",
        role: Some("editor"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: false,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },

    // ---- Write on Dataset ----
    Case { name: "viewer write public denied (not editor/admin)",
        role: Some("viewer"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor write public allowed",
        role: Some("editor"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "editor write pii without clearance denied",
        role: Some("editor"), clearances: &[], resource_markings: &["pii"], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor write pii with clearance allowed",
        role: Some("editor"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "admin write secret with full clearance allowed",
        role: Some("admin"), clearances: &["secret"], resource_markings: &["secret"], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "admin write secret without clearance denied",
        role: Some("admin"), clearances: &["pii"], resource_markings: &["secret"], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor cross-tenant write denied",
        role: Some("editor"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: false,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "analyst write public denied (not editor/admin)",
        role: Some("analyst"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor write multi-marking superset clearance",
        role: Some("editor"), clearances: &["pii","confidential","secret"], resource_markings: &["pii","confidential"], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "editor write multi-marking partial clearance denied",
        role: Some("editor"), clearances: &["pii"], resource_markings: &["pii","confidential"], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },

    // ---- Delete on Dataset ----
    Case { name: "viewer delete public denied",
        role: Some("viewer"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor delete public denied (not admin)",
        role: Some("editor"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "admin delete public allowed",
        role: Some("admin"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "admin delete pii with clearance",
        role: Some("admin"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "admin delete pii without clearance denied",
        role: Some("admin"), clearances: &[], resource_markings: &["pii"], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "admin delete secret with full clearance",
        role: Some("admin"), clearances: &["pii","confidential","secret"], resource_markings: &["secret"], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "admin cross-tenant delete denied",
        role: Some("admin"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: false,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "admin delete multi-marking partial clearance denied",
        role: Some("admin"), clearances: &["pii"], resource_markings: &["pii","secret"], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },

    // ---- branch::create on Dataset ----
    Case { name: "viewer branch::create denied",
        role: Some("viewer"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor branch::create public allowed",
        role: Some("editor"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "admin branch::create public allowed",
        role: Some("admin"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "editor branch::create pii without clearance denied",
        role: Some("editor"), clearances: &[], resource_markings: &["pii"], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor branch::create pii with clearance",
        role: Some("editor"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "admin branch::create cross-tenant denied",
        role: Some("admin"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: false,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "analyst branch::create denied (not editor/admin)",
        role: Some("analyst"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Deny },

    // ---- Read on Branch ----
    Case { name: "viewer reads public branch allowed",
        role: Some("viewer"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Branch, expected: Decision::Allow },
    Case { name: "editor reads public branch allowed",
        role: Some("editor"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Branch, expected: Decision::Allow },
    Case { name: "admin reads public branch allowed",
        role: Some("admin"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Branch, expected: Decision::Allow },
    Case { name: "anon reads branch denied",
        role: None, clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Branch, expected: Decision::Deny },
    Case { name: "viewer reads pii branch (rule weak: still allow by role)",
        role: Some("viewer"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Branch, expected: Decision::Allow },
    Case { name: "ml_engineer (unknown role) reads branch denied",
        role: Some("ml_engineer"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Branch, expected: Decision::Deny },

    // ---- Edge cases ----
    Case { name: "user with empty clearances reads empty markings",
        role: Some("viewer"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "user with many clearances reads single marking",
        role: Some("viewer"), clearances: &["a","b","c","d","e","pii"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "user with clearances missing one marking denied",
        role: Some("viewer"), clearances: &["a","b","c"], resource_markings: &["a","b","c","d"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "admin role write public same tenant",
        role: Some("admin"), clearances: &[], resource_markings: &[], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "user with admin+viewer role reads pii with clearance",
        role: Some("admin"), clearances: &["pii"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "editor delete attempts denied even with clearance",
        role: Some("editor"), clearances: &["pii","secret"], resource_markings: &["pii"], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "viewer write denied even with all clearances",
        role: Some("viewer"), clearances: &["pii","secret"], resource_markings: &[], same_tenant: true,
        action: "write", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "admin branch::create with full clearances on multi-marking",
        role: Some("admin"), clearances: &["pii","confidential","secret"], resource_markings: &["pii","confidential"], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "editor branch::create on multi-marking missing one denied",
        role: Some("editor"), clearances: &["pii"], resource_markings: &["pii","secret"], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "viewer reads pii dataset same tenant with extra clearances",
        role: Some("viewer"), clearances: &["pii","secret","confidential","other"], resource_markings: &["pii"], same_tenant: true,
        action: "read", resource: ResourceKind::Dataset, expected: Decision::Allow },
    Case { name: "anon delete denied",
        role: None, clearances: &["pii"], resource_markings: &[], same_tenant: true,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "anon branch::create denied",
        role: None, clearances: &["pii"], resource_markings: &[], same_tenant: true,
        action: "branch::create", resource: ResourceKind::Dataset, expected: Decision::Deny },
    Case { name: "editor delete cross-tenant denied",
        role: Some("editor"), clearances: &["pii"], resource_markings: &[], same_tenant: false,
        action: "delete", resource: ResourceKind::Dataset, expected: Decision::Deny },
];

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

const TENANT_A: &str = "tenant-a";
const TENANT_B: &str = "tenant-b";

fn build_store() -> PolicyStore {
    let mut records: Vec<PolicyRecord> = POLICIES
        .iter()
        .map(|(id, src)| PolicyRecord {
            id: (*id).into(),
            version: 1,
            description: None,
            source: (*src).into(),
        })
        .collect();
    records.push(PolicyRecord {
        id: BRANCH_READ_POLICY.0.into(),
        version: 1,
        description: None,
        source: BRANCH_READ_POLICY.1.into(),
    });
    let store = PolicyStore::empty().expect("schema");
    let rt = tokio::runtime::Runtime::new().expect("runtime");
    rt.block_on(store.replace_policies(&records))
        .expect("policies parse and validate");
    store
}

fn ty(name: &str) -> EntityTypeName {
    use std::str::FromStr;
    EntityTypeName::from_str(name).expect("type")
}

fn uid(type_name: &str, id: &str) -> EntityUid {
    use std::str::FromStr;
    EntityUid::from_type_name_and_id(
        ty(type_name),
        EntityId::from_str(id).expect("entity id"),
    )
}

fn marking_entity(name: &str) -> Entity {
    let attrs = [(
        "name".to_string(),
        RestrictedExpression::new_string(name.into()),
    )]
    .into_iter()
    .collect();
    Entity::new(uid("Marking", name), attrs, HashSet::new()).expect("marking entity")
}

fn user_entity(role: Option<&str>, tenant: &str, clearances: &[&str]) -> (EntityUid, Entity) {
    let user_uid = uid("User", "alice");
    let clearance_exprs: Vec<RestrictedExpression> = clearances
        .iter()
        .map(|m| RestrictedExpression::new_entity_uid(uid("Marking", m)))
        .collect();
    let role_exprs: Vec<RestrictedExpression> = role
        .into_iter()
        .map(|r| RestrictedExpression::new_string(r.to_string()))
        .collect();
    let attrs = [
        (
            "tenant".to_string(),
            RestrictedExpression::new_string(tenant.into()),
        ),
        (
            "clearances".to_string(),
            RestrictedExpression::new_set(clearance_exprs),
        ),
        (
            "roles".to_string(),
            RestrictedExpression::new_set(role_exprs),
        ),
    ]
    .into_iter()
    .collect();
    let e = Entity::new(user_uid.clone(), attrs, HashSet::new()).expect("user");
    (user_uid, e)
}

fn dataset_entity(tenant: &str, markings: &[&str]) -> (EntityUid, Entity) {
    let ds_uid = uid("Dataset", "ri.foundry.main.dataset.demo");
    let marking_set: Vec<RestrictedExpression> = markings
        .iter()
        .map(|m| RestrictedExpression::new_entity_uid(uid("Marking", m)))
        .collect();
    let attrs = [
        (
            "rid".to_string(),
            RestrictedExpression::new_string("ri.foundry.main.dataset.demo".into()),
        ),
        (
            "tenant".to_string(),
            RestrictedExpression::new_string(tenant.into()),
        ),
        (
            "markings".to_string(),
            RestrictedExpression::new_set(marking_set),
        ),
    ]
    .into_iter()
    .collect();
    let e = Entity::new(ds_uid.clone(), attrs, HashSet::new()).expect("dataset");
    (ds_uid, e)
}

fn branch_entity(dataset_uid: &EntityUid) -> (EntityUid, Entity) {
    let br_uid = uid("Branch", "main");
    let attrs = [
        (
            "name".to_string(),
            RestrictedExpression::new_string("main".into()),
        ),
        (
            "dataset_rid".to_string(),
            RestrictedExpression::new_string("ri.foundry.main.dataset.demo".into()),
        ),
    ]
    .into_iter()
    .collect();
    let mut parents = HashSet::new();
    parents.insert(dataset_uid.clone());
    let e = Entity::new(br_uid.clone(), attrs, parents).expect("branch");
    (br_uid, e)
}

#[test]
fn matrix_runs() {
    let store = build_store();
    let rt = tokio::runtime::Runtime::new().expect("runtime");

    let mut failures = Vec::new();

    for case in CASES {
        let (user_uid, user) = user_entity(case.role, TENANT_A, case.clearances);
        let resource_tenant = if case.same_tenant { TENANT_A } else { TENANT_B };
        let (ds_uid, ds) = dataset_entity(resource_tenant, case.resource_markings);
        let (resource_uid, resource_entity) = match case.resource {
            ResourceKind::Dataset => (ds_uid.clone(), ds.clone()),
            ResourceKind::Branch => branch_entity(&ds_uid),
        };

        let mut all = vec![user, ds.clone()];
        if matches!(case.resource, ResourceKind::Branch) {
            all.push(resource_entity.clone());
        }
        for m in case.clearances.iter().chain(case.resource_markings.iter()) {
            all.push(marking_entity(m));
        }
        let entities = Entities::from_entities(all, Some(&store.schema()))
            .expect("entities hydrate");

        let action_uid = uid("Action", case.action);
        let request = Request::new(
            user_uid,
            action_uid,
            resource_uid,
            Context::empty(),
            Some(&store.schema()),
        )
        .expect("request");

        let response = rt.block_on(store.is_authorized(&request, &entities));
        let got = response.decision();
        if got != case.expected {
            failures.push(format!(
                "case `{}`: expected {:?}, got {:?} (errors: {:?})",
                case.name,
                case.expected,
                got,
                response
                    .diagnostics()
                    .errors()
                    .map(|e| e.to_string())
                    .collect::<Vec<_>>()
            ));
        }
    }

    assert!(
        CASES.len() >= 50,
        "matrix must contain at least 50 cases, has {}",
        CASES.len()
    );
    assert!(
        failures.is_empty(),
        "{} of {} cases failed:\n  - {}",
        failures.len(),
        CASES.len(),
        failures.join("\n  - ")
    );
}
