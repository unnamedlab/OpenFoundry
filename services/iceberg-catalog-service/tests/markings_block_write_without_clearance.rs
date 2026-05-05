//! D1.1.8 P3 — A principal whose clearances do not cover the table's
//! effective markings is denied a write_data action via Cedar.

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

fn principal_with(scopes: &[&str]) -> AuthenticatedPrincipal {
    AuthenticatedPrincipal {
        subject: "00000000-0000-0000-0000-000000000001".to_string(),
        scopes: scopes.iter().map(|s| s.to_string()).collect::<HashSet<_>>(),
    }
}

fn pii_table() -> AuthzResource {
    AuthzResource::Table(TableAttrs {
        rid: "ri.foundry.main.iceberg-table.x".to_string(),
        namespace_rid: "ri.foundry.main.iceberg-namespace.x".to_string(),
        tenant: "default".to_string(),
        format_version: 2,
        markings: vec!["pii".to_string()],
        explicit_markings: vec![],
    })
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn principal_without_pii_clearance_is_denied_write() {
    let engine = engine().await;
    let principal = principal_with(&[
        "api:iceberg-write",
        "role:editor",
        "iceberg-clearance:public",
        // Note: no `iceberg-clearance:pii`.
    ]);
    let result = authz::enforce(
        engine.as_ref(),
        &principal,
        PrincipalKind::User,
        "iceberg::table::write_data",
        &pii_table(),
        "default",
    )
    .await;
    assert!(result.is_err(), "expected forbidden, got {:?}", result);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn principal_with_pii_clearance_is_allowed_write() {
    let engine = engine().await;
    let principal = principal_with(&[
        "api:iceberg-write",
        "role:editor",
        "iceberg-clearance:public",
        "iceberg-clearance:pii",
    ]);
    let result = authz::enforce(
        engine.as_ref(),
        &principal,
        PrincipalKind::User,
        "iceberg::table::write_data",
        &pii_table(),
        "default",
    )
    .await;
    assert!(result.is_ok(), "expected allowed, got {:?}", result);
}
