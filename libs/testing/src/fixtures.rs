//! Deterministic JWT issuance and SQL seed helpers for tests.
//!
//! The auth helpers reuse [`auth_middleware::jwt`] so the issued tokens
//! pass the production [`auth_middleware::layer::auth_layer`] without
//! any special-case handling.

use auth_middleware::jwt::{self, JwtConfig, build_access_claims, encode_token};
use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

/// Shared HS256 secret used across the test suite. Long enough to
/// satisfy the upstream length checks; never used outside tests.
pub const JWT_SECRET: &str = "openfoundry-shared-test-secret-do-not-use-in-prod-aaaa";

/// Build a [`JwtConfig`] from [`JWT_SECRET`].
pub fn jwt_config() -> JwtConfig {
    jwt::JwtConfig::new(JWT_SECRET)
}

/// Issue a Bearer-ready JWT with the requested permissions. The user id
/// is randomised so each call produces a distinct subject.
pub fn dev_token(cfg: &JwtConfig, permissions: Vec<String>) -> String {
    let claims = build_access_claims(
        cfg,
        Uuid::now_v7(),
        "tester@openfoundry.test",
        "Integration Tester",
        vec!["admin".to_string()],
        permissions,
        None,
        Value::Null,
        vec!["password".to_string()],
    );
    encode_token(cfg, &claims).expect("encode test token")
}

/// Insert a minimal `datasets` row, returning its id. Works against
/// either the catalog or versioning schemas (both use the same column
/// names: `id`, `rid`, `name`, `format`, `storage_path`, `owner_id`).
pub async fn seed_dataset(pool: &PgPool, rid: &str, name: &str, format: &str) -> Uuid {
    let id = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO datasets (id, rid, name, format, storage_path, owner_id)
           VALUES ($1, $2, $3, $4, $5, $6)"#,
    )
    .bind(id)
    .bind(rid)
    .bind(name)
    .bind(format)
    .bind(format!("local://{rid}/v1"))
    .bind(Uuid::now_v7())
    .execute(pool)
    .await
    .expect("seed dataset row");
    id
}

