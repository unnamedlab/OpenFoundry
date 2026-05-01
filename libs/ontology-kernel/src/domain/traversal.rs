//! Multi-hop graph traversal over `link_instances` using a recursive CTE.
//!
//! Foundry exposes object graphs with a `?graph(depth=N, link_types=[...])`
//! style query. This module is the kernel-side primitive used by both the
//! HTTP handler (`handlers::links::traverse_neighbors`) and any internal
//! caller (rules, action previews, exports).
//!
//! The CTE walks `link_instances` (the live, current-state edge table) rather
//! than `link_revisions` (the audit log) so traversal reflects the *current*
//! ontology graph. Each visited row is filtered by:
//!
//! * `link_type_ids` – optional whitelist of link types to traverse.
//! * `marking_filter` – optional list of allowed markings (column on
//!   `link_instances`); the default is the caller's clearance.
//! * `max_depth` – clamped to `[1, 5]` to bound recursion.

use auth_middleware::claims::Claims;
use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::FromRow;
use uuid::Uuid;

use crate::{AppState, domain::access::clearance_rank};

/// One edge returned by [`traverse`].
#[derive(Debug, Clone, FromRow, Serialize)]
pub struct TraversedEdge {
    pub link_id: Uuid,
    pub link_type_id: Uuid,
    pub source_object_id: Uuid,
    pub target_object_id: Uuid,
    pub marking: String,
    pub depth: i32,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone)]
pub struct TraversalParams {
    pub starting_object_id: Uuid,
    pub max_depth: i32,
    pub link_type_ids: Option<Vec<Uuid>>,
    pub marking_filter: Option<Vec<String>>,
    pub limit: i32,
}

/// Returns the set of allowed markings for a caller. If the caller passed an
/// explicit `marking_filter` we honour it; otherwise we derive it from the
/// caller's clearance using the same hierarchy as
/// `domain::access::ensure_object_access` (public ⊂ confidential ⊂ pii).
fn resolve_marking_filter(claims: &Claims, marking_filter: Option<Vec<String>>) -> Vec<String> {
    if let Some(filter) = marking_filter.filter(|f| !f.is_empty()) {
        return filter;
    }
    if claims.has_role("admin") {
        return vec![
            "public".to_string(),
            "confidential".to_string(),
            "pii".to_string(),
        ];
    }
    let granted = clearance_rank(claims);
    let mut allowed = vec!["public".to_string()];
    if granted >= 1 {
        allowed.push("confidential".to_string());
    }
    if granted >= 2 {
        allowed.push("pii".to_string());
    }
    allowed
}

/// Recursive CTE multi-hop traversal.
///
/// The CTE seeds at `starting_object_id` and walks bi-directionally
/// (source→target and target→source) through `link_instances`, expanding up
/// to `max_depth` hops. A per-edge dedup key prevents infinite loops on
/// cyclic graphs.
pub async fn traverse(
    state: &AppState,
    claims: &Claims,
    params: TraversalParams,
) -> Result<Vec<TraversedEdge>, sqlx::Error> {
    let max_depth = params.max_depth.clamp(1, 5);
    let limit = params.limit.clamp(1, 5_000);
    let link_type_filter = params.link_type_ids.unwrap_or_default();
    let marking_filter = resolve_marking_filter(claims, params.marking_filter);

    let sql = r#"
        WITH RECURSIVE walk(
            link_id,
            link_type_id,
            source_object_id,
            target_object_id,
            marking,
            depth,
            created_at,
            visited_node
        ) AS (
            SELECT
                li.id           AS link_id,
                li.link_type_id AS link_type_id,
                li.source_object_id,
                li.target_object_id,
                COALESCE(li.marking, 'public') AS marking,
                1               AS depth,
                li.created_at,
                CASE
                    WHEN li.source_object_id = $1 THEN li.target_object_id
                    ELSE li.source_object_id
                END             AS visited_node
            FROM link_instances li
            WHERE (li.source_object_id = $1 OR li.target_object_id = $1)
              AND ($3::uuid[] IS NULL OR cardinality($3::uuid[]) = 0 OR li.link_type_id = ANY($3::uuid[]))
              AND COALESCE(li.marking, 'public') = ANY($4::text[])

            UNION ALL

            SELECT
                li.id,
                li.link_type_id,
                li.source_object_id,
                li.target_object_id,
                COALESCE(li.marking, 'public'),
                w.depth + 1,
                li.created_at,
                CASE
                    WHEN li.source_object_id = w.visited_node THEN li.target_object_id
                    ELSE li.source_object_id
                END
            FROM link_instances li
            JOIN walk w
              ON (li.source_object_id = w.visited_node OR li.target_object_id = w.visited_node)
            WHERE w.depth < $2
              AND li.id <> w.link_id
              AND ($3::uuid[] IS NULL OR cardinality($3::uuid[]) = 0 OR li.link_type_id = ANY($3::uuid[]))
              AND COALESCE(li.marking, 'public') = ANY($4::text[])
        )
        SELECT DISTINCT ON (link_id)
            link_id,
            link_type_id,
            source_object_id,
            target_object_id,
            marking,
            depth,
            created_at
        FROM walk
        ORDER BY link_id, depth ASC
        LIMIT $5
    "#;

    sqlx::query_as::<_, TraversedEdge>(sql)
        .bind(params.starting_object_id)
        .bind(max_depth)
        .bind(&link_type_filter)
        .bind(&marking_filter)
        .bind(limit as i64)
        .fetch_all(&state.db)
        .await
}
