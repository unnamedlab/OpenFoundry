//! PostgreSQL-backed repository for declarative link type metadata.
//!
//! Link instances live behind [`storage_abstraction::repositories::LinkStore`].
//! Link type definitions remain declarative S1 metadata on PostgreSQL, but
//! handlers should call this module instead of embedding SQL.

use sqlx::PgPool;
use uuid::Uuid;

use crate::models::link_type::{
    CreateLinkTypeRequest, LinkType, ListLinkTypesQuery, UpdateLinkTypeRequest,
};

pub async fn create(
    db: &PgPool,
    id: Uuid,
    owner_id: Uuid,
    body: &CreateLinkTypeRequest,
    display_name: &str,
    description: &str,
    cardinality: &str,
) -> Result<LinkType, sqlx::Error> {
    sqlx::query_as::<_, LinkType>(
        r#"INSERT INTO link_types (id, name, display_name, description, source_type_id, target_type_id, cardinality, owner_id)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
           RETURNING *"#,
    )
    .bind(id)
    .bind(&body.name)
    .bind(display_name)
    .bind(description)
    .bind(body.source_type_id)
    .bind(body.target_type_id)
    .bind(cardinality)
    .bind(owner_id)
    .fetch_one(db)
    .await
}

pub async fn list(
    db: &PgPool,
    params: &ListLinkTypesQuery,
    limit: i64,
    offset: i64,
) -> Result<(Vec<LinkType>, i64), sqlx::Error> {
    if let Some(object_type_id) = params.object_type_id {
        let total = sqlx::query_scalar::<_, i64>(
            "SELECT COUNT(*) FROM link_types WHERE source_type_id = $1 OR target_type_id = $1",
        )
        .bind(object_type_id)
        .fetch_one(db)
        .await?;

        let rows = sqlx::query_as::<_, LinkType>(
            r#"SELECT * FROM link_types
               WHERE source_type_id = $1 OR target_type_id = $1
               ORDER BY created_at DESC LIMIT $2 OFFSET $3"#,
        )
        .bind(object_type_id)
        .bind(limit)
        .bind(offset)
        .fetch_all(db)
        .await?;

        Ok((rows, total))
    } else {
        let total = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM link_types")
            .fetch_one(db)
            .await?;

        let rows = sqlx::query_as::<_, LinkType>(
            "SELECT * FROM link_types ORDER BY created_at DESC LIMIT $1 OFFSET $2",
        )
        .bind(limit)
        .bind(offset)
        .fetch_all(db)
        .await?;

        Ok((rows, total))
    }
}

pub async fn delete(db: &PgPool, id: Uuid) -> Result<bool, sqlx::Error> {
    let result = sqlx::query("DELETE FROM link_types WHERE id = $1")
        .bind(id)
        .execute(db)
        .await?;
    Ok(result.rows_affected() > 0)
}

pub async fn load(db: &PgPool, id: Uuid) -> Result<Option<LinkType>, sqlx::Error> {
    sqlx::query_as::<_, LinkType>("SELECT * FROM link_types WHERE id = $1")
        .bind(id)
        .fetch_optional(db)
        .await
}

pub async fn update(
    db: &PgPool,
    id: Uuid,
    body: UpdateLinkTypeRequest,
    cardinality: String,
) -> Result<Option<LinkType>, sqlx::Error> {
    sqlx::query_as::<_, LinkType>(
        r#"UPDATE link_types
           SET display_name = COALESCE($2, display_name),
               description = COALESCE($3, description),
               cardinality = $4,
               updated_at = NOW()
           WHERE id = $1
           RETURNING *"#,
    )
    .bind(id)
    .bind(body.display_name)
    .bind(body.description)
    .bind(cardinality)
    .fetch_optional(db)
    .await
}
