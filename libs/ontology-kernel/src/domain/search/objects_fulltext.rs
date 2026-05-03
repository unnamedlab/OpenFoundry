use auth_middleware::claims::Claims;
use chrono::{DateTime, Utc};
use serde::Serialize;
use serde_json::Value;
use std::collections::HashMap;
use storage_abstraction::repositories::{Page, ReadConsistency, SearchQuery, TypeId};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        access::clearance_rank,
        read_models::{search_hit_to_object_instance, tenant_from_claims},
    },
};

#[derive(Debug, Clone, Serialize)]
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
) -> Result<Vec<ObjectFulltextHit>, String> {
    let trimmed = query.query.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }
    let limit = query.limit.clamp(1, 200);
    let markings = allowed_markings_for(claims, query.markings);
    if markings.is_empty() {
        return Ok(Vec::new());
    }
    let tenant = tenant_from_claims(claims);
    let hits = state
        .stores
        .search
        .search(
            SearchQuery {
                tenant,
                type_id: query.object_type_id.map(|value| TypeId(value.to_string())),
                q: Some(trimmed.to_string()),
                filters: HashMap::new(),
                page: Page {
                    size: limit as u32,
                    token: None,
                },
            },
            ReadConsistency::Eventual,
        )
        .await
        .map_err(|error| format!("search backend full-text query failed: {error}"))?;

    Ok(hits
        .items
        .iter()
        .filter_map(|hit| {
            let object = search_hit_to_object_instance(hit, claims.org_id)?;
            if !markings.iter().any(|marking| marking == &object.marking) {
                return None;
            }
            Some(ObjectFulltextHit {
                id: object.id,
                object_type_id: object.object_type_id,
                properties: object.properties,
                marking: object.marking,
                created_at: object.created_at,
                updated_at: object.updated_at,
                rank: hit.score,
            })
        })
        .collect())
}
