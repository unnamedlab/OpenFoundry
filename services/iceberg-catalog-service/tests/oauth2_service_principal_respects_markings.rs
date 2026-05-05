//! An OAuth2 service principal with a limited clearance set is
//! denied access to a `confidential`-marked table even though the
//! token carries the right scope (`api:iceberg-read`).

use std::sync::Arc;

use authz_cedar::{AuthzEngine, PolicyStore};
use iceberg_catalog_service::authz::{self, AuthzResource, PrincipalKind, TableAttrs};
use iceberg_catalog_service::handlers::auth::bearer::AuthenticatedPrincipal;
use std::collections::HashSet;

async fn engine() -> Arc<AuthzEngine> {
    let store = PolicyStore::with_policies(&authz_cedar::iceberg_policies::all_iceberg_policies())
        .await
        .expect("policies validate");
    Arc::new(AuthzEngine::with_noop_audit(store))
}

fn confidential_table() -> AuthzResource {
    AuthzResource::Table(TableAttrs {
        rid: "ri.foundry.main.iceberg-table.s".to_string(),
        namespace_rid: "ri.foundry.main.iceberg-namespace.s".to_string(),
        tenant: "default".to_string(),
        format_version: 2,
        markings: vec!["confidential".to_string()],
        explicit_markings: vec![],
    })
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn service_principal_without_clearance_is_denied_view() {
    let engine = engine().await;
    let principal = AuthenticatedPrincipal {
        subject: "00000000-0000-0000-0000-00000000c001".to_string(),
        scopes: HashSet::from_iter([
            "api:iceberg-read".to_string(),
            "svc:client-1".to_string(),
            "iceberg-clearance:public".to_string(),
        ]),
    };
    let result = authz::enforce(
        engine.as_ref(),
        &principal,
        PrincipalKind::ServicePrincipal,
        "iceberg::table::view",
        &confidential_table(),
        "default",
    )
    .await;
    assert!(result.is_err());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn service_principal_with_matching_clearance_is_allowed_view() {
    let engine = engine().await;
    let principal = AuthenticatedPrincipal {
        subject: "00000000-0000-0000-0000-00000000c002".to_string(),
        scopes: HashSet::from_iter([
            "api:iceberg-read".to_string(),
            "svc:client-2".to_string(),
            "iceberg-clearance:public".to_string(),
            "iceberg-clearance:confidential".to_string(),
        ]),
    };
    let result = authz::enforce(
        engine.as_ref(),
        &principal,
        PrincipalKind::ServicePrincipal,
        "iceberg::table::view",
        &confidential_table(),
        "default",
    )
    .await;
    assert!(result.is_ok(), "expected allowed, got {:?}", result);
}
