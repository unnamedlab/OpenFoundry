//! Postgres-backed store for global branches + resource links.

use chrono::Utc;
use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

use super::model::{
    CreateGlobalBranchLinkRequest, CreateGlobalBranchRequest, GlobalBranch, GlobalBranchLink,
    GlobalBranchSummary,
};

/// Inserts a new global branch row.
pub async fn create_branch(
    pool: &PgPool,
    request: &CreateGlobalBranchRequest,
    created_by: &str,
) -> Result<GlobalBranch, sqlx::Error> {
    sqlx::query_as::<_, GlobalBranch>(
        r#"INSERT INTO global_branches (id, name, description, parent_global_branch, created_by)
            VALUES ($1, $2, $3, $4, $5)
            RETURNING id, rid, name, parent_global_branch, description,
                      created_by, created_at, archived_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(&request.name)
    .bind(request.description.clone().unwrap_or_default())
    .bind(request.parent_global_branch)
    .bind(created_by)
    .fetch_one(pool)
    .await
}

pub async fn get_branch(pool: &PgPool, id: Uuid) -> Result<Option<GlobalBranch>, sqlx::Error> {
    sqlx::query_as::<_, GlobalBranch>(
        r#"SELECT id, rid, name, parent_global_branch, description,
                  created_by, created_at, archived_at
             FROM global_branches WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(pool)
    .await
}

pub async fn list_branches(pool: &PgPool) -> Result<Vec<GlobalBranch>, sqlx::Error> {
    sqlx::query_as::<_, GlobalBranch>(
        r#"SELECT id, rid, name, parent_global_branch, description,
                  created_by, created_at, archived_at
             FROM global_branches
            ORDER BY created_at DESC"#,
    )
    .fetch_all(pool)
    .await
}

pub async fn add_link(
    pool: &PgPool,
    global_branch_id: Uuid,
    request: &CreateGlobalBranchLinkRequest,
) -> Result<GlobalBranchLink, sqlx::Error> {
    sqlx::query_as::<_, GlobalBranchLink>(
        r#"INSERT INTO global_branch_resource_links
              (global_branch_id, resource_type, resource_rid, branch_rid, status, last_synced_at)
            VALUES ($1, $2, $3, $4, 'in_sync', $5)
            ON CONFLICT (global_branch_id, resource_type, resource_rid)
            DO UPDATE SET branch_rid = EXCLUDED.branch_rid,
                          status     = 'in_sync',
                          last_synced_at = EXCLUDED.last_synced_at
            RETURNING global_branch_id, resource_type, resource_rid,
                      branch_rid, status, last_synced_at"#,
    )
    .bind(global_branch_id)
    .bind(&request.resource_type)
    .bind(&request.resource_rid)
    .bind(&request.branch_rid)
    .bind(Utc::now())
    .fetch_one(pool)
    .await
}

pub async fn list_links(
    pool: &PgPool,
    global_branch_id: Uuid,
) -> Result<Vec<GlobalBranchLink>, sqlx::Error> {
    sqlx::query_as::<_, GlobalBranchLink>(
        r#"SELECT global_branch_id, resource_type, resource_rid,
                  branch_rid, status, last_synced_at
             FROM global_branch_resource_links
            WHERE global_branch_id = $1
            ORDER BY resource_type, resource_rid"#,
    )
    .bind(global_branch_id)
    .fetch_all(pool)
    .await
}

pub async fn summary(
    pool: &PgPool,
    branch: GlobalBranch,
) -> Result<GlobalBranchSummary, sqlx::Error> {
    let row: (i64, i64, i64) = sqlx::query_as(
        r#"SELECT
              COUNT(*) AS total,
              COUNT(*) FILTER (WHERE status = 'drifted')  AS drifted,
              COUNT(*) FILTER (WHERE status = 'archived') AS archived
            FROM global_branch_resource_links
            WHERE global_branch_id = $1"#,
    )
    .bind(branch.id)
    .fetch_one(pool)
    .await?;
    Ok(GlobalBranchSummary {
        branch,
        link_count: row.0,
        drifted_count: row.1,
        archived_count: row.2,
    })
}

/// Update the `status` column of every link that points at
/// `branch_rid`. Used by the Kafka subscriber when an upstream
/// branch is archived / restored / drifted.
pub async fn update_links_for_branch(
    pool: &PgPool,
    branch_rid: &str,
    status: &str,
) -> Result<u64, sqlx::Error> {
    let result = sqlx::query(
        r#"UPDATE global_branch_resource_links
              SET status = $2, last_synced_at = NOW()
            WHERE branch_rid = $1"#,
    )
    .bind(branch_rid)
    .bind(status)
    .execute(pool)
    .await?;
    Ok(result.rows_affected())
}

/// Build the promote-request payload that we then push onto the
/// outbox via [`outbox::enqueue`]. Kept as a plain function so the
/// handler and the unit tests share the same JSON shape.
pub fn promote_payload(global_id: Uuid, name: &str, actor: &str) -> Value {
    serde_json::json!({
        "event_type": "global.branch.promote.requested.v1",
        "global_branch_id": global_id,
        "global_branch_name": name,
        "actor": actor,
        "occurred_at": Utc::now(),
    })
}
