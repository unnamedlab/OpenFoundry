use auth_middleware::jwt::{self, JwtConfig, JwtError};
use sqlx::PgPool;
use uuid::Uuid;

use crate::domain::{rbac, security};
use crate::models::session::RefreshToken;
use crate::models::user::User;
use crate::sessions_cassandra::SessionsAdapter;

/// Issue an access + refresh token pair for a user.
pub async fn issue_tokens(
    pool: &PgPool,
    sessions: &SessionsAdapter,
    config: &JwtConfig,
    user: &User,
    auth_methods: Vec<String>,
) -> Result<(String, String), JwtError> {
    let access_bundle = rbac::get_user_access_bundle(pool, user.id)
        .await
        .unwrap_or_default();

    let access_claims = jwt::build_access_claims(
        config,
        user.id,
        &user.email,
        &user.name,
        access_bundle.roles,
        access_bundle.permissions,
        user.organization_id,
        user.attributes.clone(),
        auth_methods,
    );
    let refresh_claims = jwt::build_refresh_claims(config, user.id);

    let access_token = jwt::encode_token(config, &access_claims)?;
    let refresh_token = jwt::encode_token(config, &refresh_claims)?;

    let _ = store_refresh_token(
        sessions,
        user.id,
        refresh_claims.jti,
        &refresh_token,
        refresh_claims.exp,
    )
    .await;

    Ok((access_token, refresh_token))
}

pub async fn store_refresh_token(
    sessions: &SessionsAdapter,
    user_id: Uuid,
    token_id: Uuid,
    refresh_token: &str,
    expires_at_ts: i64,
) -> cassandra_kernel::KernelResult<()> {
    let expires_at = chrono::DateTime::from_timestamp(expires_at_ts, 0)
        .unwrap_or_else(chrono::Utc::now)
        .with_timezone(&chrono::Utc);

    sessions
        .store_refresh_token(
            user_id,
            token_id,
            &security::hash_token(refresh_token),
            expires_at,
        )
        .await
}

pub async fn revoke_refresh_token(
    sessions: &SessionsAdapter,
    token_id: Uuid,
) -> cassandra_kernel::KernelResult<()> {
    sessions.revoke_refresh_token(token_id).await
}

pub async fn get_refresh_token(
    sessions: &SessionsAdapter,
    token_id: Uuid,
) -> cassandra_kernel::KernelResult<Option<RefreshToken>> {
    Ok(sessions
        .get_refresh_token(token_id)
        .await?
        .map(|record| RefreshToken {
            id: record.id,
            user_id: record.user_id,
            token_hash: record.token_hash,
            expires_at: record.expires_at,
            revoked: record.revoked,
            created_at: record.created_at,
        }))
}

pub fn refresh_token_matches(token: &RefreshToken, raw_token: &str) -> bool {
    !token.revoked
        && token.token_hash == security::hash_token(raw_token)
        && token.expires_at > chrono::Utc::now()
}
