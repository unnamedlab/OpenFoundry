//! Long-lived API tokens issued for Iceberg clients.
//!
//! The Foundry doc § "Using an API token" says these are "long-lived
//! and tied to your user". We follow the same pattern as
//! `oauth-integration-service::api_keys`: the secret is shown to the
//! caller exactly once, the catalog stores only its SHA-256 hash and a
//! 4-character hint for the UI.

use chrono::{DateTime, Utc};
use rand::RngCore;
use rand::rngs::OsRng;
use sha2::{Digest, Sha256};
use sqlx::{FromRow, PgPool};
use uuid::Uuid;

#[derive(Debug, Clone, FromRow)]
pub struct ApiToken {
    pub id: Uuid,
    pub user_id: Uuid,
    pub name: String,
    pub token_hint: String,
    pub scopes: Vec<String>,
    pub expires_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub last_used_at: Option<DateTime<Utc>>,
    pub revoked_at: Option<DateTime<Utc>>,
}

#[derive(Debug, thiserror::Error)]
pub enum TokenError {
    #[error("token not found or revoked")]
    NotFound,
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

/// Returned once on creation; the raw `token` value is **never** stored.
#[derive(Debug, Clone)]
pub struct IssuedToken {
    pub record: ApiToken,
    pub raw_token: String,
}

pub fn hash_token(token: &str) -> String {
    let digest = Sha256::digest(token.as_bytes());
    hex::encode(digest)
}

mod hex {
    pub fn encode(bytes: impl AsRef<[u8]>) -> String {
        let bytes = bytes.as_ref();
        let mut out = String::with_capacity(bytes.len() * 2);
        for b in bytes {
            out.push(nibble(b >> 4));
            out.push(nibble(b & 0x0f));
        }
        out
    }
    fn nibble(n: u8) -> char {
        match n {
            0..=9 => (b'0' + n) as char,
            _ => (b'a' + n - 10) as char,
        }
    }
}

pub async fn issue(
    pool: &PgPool,
    user_id: Uuid,
    name: &str,
    scopes: Vec<String>,
    ttl_secs: Option<i64>,
) -> Result<IssuedToken, TokenError> {
    let mut bytes = [0u8; 32];
    OsRng.fill_bytes(&mut bytes);
    let raw_token = format!("ofty_{}", hex::encode(bytes));
    let token_hash = hash_token(&raw_token);
    let token_hint = raw_token[raw_token.len() - 4..].to_string();
    let expires_at = ttl_secs.map(|t| Utc::now() + chrono::Duration::seconds(t));

    let row: ApiToken = sqlx::query_as(
        r#"
        INSERT INTO iceberg_api_tokens (id, user_id, name, token_hash, token_hint, scopes, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        RETURNING id, user_id, name, token_hint, scopes, expires_at, created_at, last_used_at, revoked_at
        "#,
    )
    .bind(Uuid::now_v7())
    .bind(user_id)
    .bind(name)
    .bind(&token_hash)
    .bind(&token_hint)
    .bind(&scopes)
    .bind(expires_at)
    .fetch_one(pool)
    .await?;

    Ok(IssuedToken {
        record: row,
        raw_token,
    })
}

pub async fn validate(pool: &PgPool, raw_token: &str) -> Result<ApiToken, TokenError> {
    let token_hash = hash_token(raw_token);
    let row: Option<ApiToken> = sqlx::query_as(
        r#"
        SELECT id, user_id, name, token_hint, scopes, expires_at, created_at, last_used_at, revoked_at
        FROM iceberg_api_tokens
        WHERE token_hash = $1
          AND revoked_at IS NULL
          AND (expires_at IS NULL OR expires_at > NOW())
        "#,
    )
    .bind(&token_hash)
    .fetch_optional(pool)
    .await?;

    let token = row.ok_or(TokenError::NotFound)?;

    sqlx::query("UPDATE iceberg_api_tokens SET last_used_at = NOW() WHERE id = $1")
        .bind(token.id)
        .execute(pool)
        .await?;
    Ok(token)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hash_token_is_deterministic() {
        let a = hash_token("secret");
        let b = hash_token("secret");
        assert_eq!(a, b);
        assert_eq!(a.len(), 64);
    }

    #[test]
    fn distinct_tokens_have_distinct_hashes() {
        assert_ne!(hash_token("a"), hash_token("b"));
    }
}
