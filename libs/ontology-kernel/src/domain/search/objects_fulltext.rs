//! Postgres full-text object search backed by the `searchable_text` tsvector
//! column on `object_instances`.
//!
//! `ts_rank_cd` is used as the BM25 analogue (cover density ranking which
//! takes into account term proximity and inverse-document-frequency-like
//! weighting). Matching is `plainto_tsquery`-based so callers can paste raw
//! user input without worrying about operator syntax.

use auth_middleware::claims::Claims;
use chrono::{DateTime, Utc};
use serde::Serialize;
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

use crate::{AppState, domain::access::clearance_rank};

#[derive(Debug, Clone, FromRow, Serialize)]
pub struct ObjectFulltextHit {
    pub id: Uuid,
    pub object_type_id: Uuid,
    pub properties: Value,
    pub marking: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub rank: f32,
}

#[derive(Debug, Clone)]
pub struct ObjectFulltextQuery {
    pub query: String,
    pub object_type_id: Option<Uuid>,
    pub markings: Option<Vec<String>>,
    pub limit: i64,
}

fn allowed_markings_for(claims: &Claims, requested: Option<Vec<String>>) -> Vec<String> {
    let base = if claims.has_role("admin") {
        vec![
            "public".to_string(),
            "confidential".to_string(),
            "pii".to_string(),
        ]
    } else {
        let granted = clearance_rank(claims);
        let mut allowed = vec!["public".to_string()];
        if granted >= 1 {
            allowed.push("confidential".to_string());
        }
        if granted >= 2 {
            allowed.push("pii".to_string());
        }
        allowed
    };
    match requested {
        Some(filter) if !filter.is_empty() => filter
            .into_iter()
            .filter(|marking| base.contains(marking))
            .collect(),
        _ => base,
    }
}

pub async fn search_objects(
    state: &AppState,
    claims: &Claims,
    query: ObjectFulltextQuery,
) -> Result<Vec<ObjectFulltextHit>, sqlx::Error> {
    let trimmed = query.query.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }
    let limit = query.limit.clamp(1, 200);
    let markings = allowed_markings_for(claims, query.markings);
    if markings.is_empty() {
        return Ok(Vec::new());
    }
    let object_type_filter = query.object_type_id;
    let org_filter = if claims.has_role("admin") {
        None
    } else {
        claims.org_id
    };

    let sql = r#"
        SELECT
            id,
            object_type_id,
            properties,
            marking,
            created_at,
            updated_at,
            ts_rank_cd(searchable_text, plainto_tsquery('simple', $1))::real AS rank
        FROM object_instances
        WHERE searchable_text @@ plainto_tsquery('simple', $1)
          AND ($2::uuid IS NULL OR object_type_id = $2)
          AND marking = ANY($3::text[])
          AND ($4::uuid IS NULL OR organization_id IS NULL OR organization_id = $4)
        ORDER BY rank DESC, updated_at DESC
        LIMIT $5
    "#;

    sqlx::query_as::<_, ObjectFulltextHit>(sql)
        .bind(trimmed)
        .bind(object_type_filter)
        .bind(&markings)
        .bind(org_filter)
        .bind(limit)
        .fetch_all(&state.db)
        .await
}
