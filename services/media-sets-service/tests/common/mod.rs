//! Shared test harness for `media-sets-service` integration tests.
//!
//! Boots an ephemeral Postgres via testcontainers, applies the crate's
//! migrations, builds the Axum router with a `LocalStorage`-backed
//! [`MediaStorage`] under a tempdir, and mints a JWT the production
//! `auth_layer` accepts. Keep the returned [`Harness`] alive for the
//! duration of the test (drop ⇒ container teardown).

#![allow(dead_code)]

use std::sync::Arc;

use audit_trail::events::AuditContext;
use auth_middleware::claims::SessionScope;
use auth_middleware::jwt::{JwtConfig, build_access_claims_with_scope, encode_token};
use authz_cedar::{AuthzEngine, PolicyStore, audit::NoopAuditSink};
use axum::Router;
use db_pool::DualPool;
use media_sets_service::{
    AppState, BackendMediaStorage, MediaStorage, build_router,
    domain::cedar::default_policy_records,
    models::{CreateMediaSetRequest, MediaSet, MediaSetSchema, TransactionPolicy},
};
use serde_json::Value;
use sqlx::PgPool;
use storage_abstraction::backend::StorageBackend;
use storage_abstraction::local::LocalStorage;
use tempfile::TempDir;
use testcontainers::{ContainerAsync, GenericImage};
use testing::{containers::boot_postgres, fixtures};
use uuid::Uuid;

pub struct Harness {
    pub container: ContainerAsync<GenericImage>,
    pub pool: PgPool,
    pub router: Router,
    pub jwt_config: JwtConfig,
    pub token: String,
    pub state: AppState,
    /// Tenant id baked into both the JWT (`org_id`) and the Cedar
    /// `MediaSet`/`MediaItem` entities (`tenant`). Tests reuse it to
    /// mint additional tokens for "other-tenant" cross-checks.
    pub tenant: Uuid,
    pub _storage_dir: TempDir,
}

/// Mint a JWT carrying the supplied roles + clearances + tenant. Used
/// by H3 tests to compose denial / allow scenarios deterministically.
pub fn mint_token(
    cfg: &JwtConfig,
    roles: Vec<String>,
    allowed_markings: Vec<String>,
    tenant: Option<Uuid>,
) -> String {
    let claims = build_access_claims_with_scope(
        cfg,
        Uuid::now_v7(),
        "tester@openfoundry.test",
        "Integration Tester",
        roles,
        vec![],
        tenant,
        Value::Null,
        vec!["password".to_string()],
        Some(SessionScope {
            allowed_markings,
            ..Default::default()
        }),
        Some("access".to_string()),
    );
    encode_token(cfg, &claims).expect("encode token")
}

pub async fn spawn() -> Harness {
    spawn_with_connector(None).await
}

/// Variant used by tests that need to mock `connector-management-service`
/// (currently only the virtual-media-set test). Pass `Some(mock_url)`
/// to wire it into [`AppState::connector_service_url`].
pub async fn spawn_with_connector(connector_service_url: Option<String>) -> Harness {
    let (container, pool, _url) = boot_postgres().await;

    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("apply media-sets-service migrations");

    let dir = tempfile::tempdir().expect("temp storage root");
    let backend: Arc<dyn StorageBackend> =
        Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).expect("local storage"));
    let storage: Arc<dyn MediaStorage> = Arc::new(BackendMediaStorage::new(
        backend,
        "media".to_string(),
        "".to_string(), // empty endpoint → emit local:// URIs in test
    ));

    let jwt_config = fixtures::jwt_config();
    let tenant = Uuid::now_v7();
    // Default token: admin role + every standard clearance, so the
    // baseline tests built before H3 still pass without the operator
    // hitting Cedar denials. H3 tests use `mint_token` to craft
    // narrower principals.
    let token = mint_token(
        &jwt_config,
        vec!["admin".into()],
        vec!["public".into(), "confidential".into(), "pii".into()],
        Some(tenant),
    );

    // Cedar engine seeded with the bundled media-set defaults.
    let policy_store = PolicyStore::with_policies(&default_policy_records())
        .await
        .expect("default policies validate");
    let engine = Arc::new(AuthzEngine::new(policy_store, Arc::new(NoopAuditSink)));

    let state = AppState {
        db: DualPool::from_pools(pool.clone(), None),
        jwt_config: Arc::new(jwt_config.clone()),
        storage,
        presign_ttl_seconds: 300,
        http: reqwest::Client::new(),
        connector_service_url,
        engine,
        presign_secret: Arc::new(testing::fixtures::JWT_SECRET.as_bytes().to_vec()),
    };
    let router = build_router(state.clone());

    Harness {
        container,
        pool,
        router,
        jwt_config,
        token,
        state,
        tenant,
        _storage_dir: dir,
    }
}

/// Convenience: create a media set via the operation layer (no HTTP) and
/// return its row. Tests that just need a parent set use this; tests
/// that exercise the HTTP surface call the route directly.
pub async fn seed_media_set(
    state: &AppState,
    name: &str,
    project_rid: &str,
    policy: TransactionPolicy,
) -> MediaSet {
    let req = CreateMediaSetRequest {
        name: name.to_string(),
        project_rid: project_rid.to_string(),
        schema: MediaSetSchema::Image,
        allowed_mime_types: vec!["image/png".into()],
        transaction_policy: policy,
        retention_seconds: 0,
        virtual_: false,
        source_rid: None,
        markings: vec![],
    };
    media_sets_service::handlers::media_sets::create_media_set_op(state, req, "test", &test_ctx())
        .await
        .expect("seed media set")
}

/// Default test [`AuditContext`] used by tests that do not care about
/// the per-event metadata. Carries a stable actor + a fresh request id
/// so deterministic outbox `event_id`s collide only inside one call.
pub fn test_ctx() -> AuditContext {
    AuditContext::for_actor("test")
        .with_request_id(Uuid::now_v7().to_string())
        .with_source_service("media-sets-service")
}
