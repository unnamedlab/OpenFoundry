//! D1.1.8 P3 — Iceberg markings persistence + projections.
//!
//! Mirrors the dataset markings model (D1.1.4 P4):
//!
//!   * `iceberg_namespace_markings` records the explicit markings on
//!     the namespace.
//!   * `iceberg_table_markings` records per-table markings split into
//!     `inherited` (snapshotted from the namespace at creation time)
//!     and `explicit` (manually managed via `manage_markings`).
//!   * `iceberg_marking_names` projects `marking_id → name` so
//!     responses surface human-readable strings.
//!
//! Effective markings = union(`inherited`, `explicit`). The catalog
//! caches that union in the `iceberg_tables.markings TEXT[]` column
//! (P1 schema) so existing handlers can read it without joining; the
//! cache is refreshed by [`refresh_table_markings_cache`] after every
//! mutation.

use core_models::security::MarkingId;
use serde::{Deserialize, Serialize};
use sqlx::{PgPool, Row};
use uuid::Uuid;

use crate::domain::namespace::Namespace;
use crate::domain::table::IcebergTable;

/// One marking entry as projected by the markings endpoints.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct MarkingProjection {
    pub marking_id: MarkingId,
    pub name: String,
    pub description: String,
}

/// Effective markings for an Iceberg table.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct TableMarkings {
    pub effective: Vec<MarkingProjection>,
    pub explicit: Vec<MarkingProjection>,
    pub inherited_from_namespace: Vec<MarkingProjection>,
}

/// Effective markings for a namespace (all explicit; namespaces
/// don't inherit from anywhere today — sub-namespace inheritance is
/// reserved for D1.1.8 P5).
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct NamespaceMarkings {
    pub effective: Vec<MarkingProjection>,
    pub explicit: Vec<MarkingProjection>,
}

#[derive(Debug, thiserror::Error)]
pub enum MarkingsError {
    #[error("unknown marking name `{0}`")]
    UnknownName(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

/// Resolve a marking *name* (case-insensitive) to its id via the
/// `iceberg_marking_names` projection.
pub async fn resolve_name(pool: &PgPool, name: &str) -> Result<MarkingId, MarkingsError> {
    let row: Option<(Uuid,)> =
        sqlx::query_as("SELECT marking_id FROM iceberg_marking_names WHERE name = $1")
            .bind(name.to_ascii_lowercase())
            .fetch_optional(pool)
            .await?;
    let id = row
        .map(|r| r.0)
        .ok_or_else(|| MarkingsError::UnknownName(name.to_string()))?;
    Ok(MarkingId::from_uuid(id))
}

/// Hydrate full marking projections (id + name + description) for a
/// list of marking ids. Unknown ids are silently dropped — the caller
/// has already enforced that the id exists when staging the write.
pub async fn project(
    pool: &PgPool,
    ids: &[Uuid],
) -> Result<Vec<MarkingProjection>, MarkingsError> {
    if ids.is_empty() {
        return Ok(Vec::new());
    }
    let rows = sqlx::query(
        r#"
        SELECT marking_id, name, description
        FROM iceberg_marking_names
        WHERE marking_id = ANY($1)
        ORDER BY name
        "#,
    )
    .bind(ids)
    .fetch_all(pool)
    .await?;
    let mut out = Vec::with_capacity(rows.len());
    for row in rows {
        let id: Uuid = row.try_get("marking_id")?;
        let name: String = row.try_get("name")?;
        let description: String = row.try_get("description")?;
        out.push(MarkingProjection {
            marking_id: MarkingId::from_uuid(id),
            name,
            description,
        });
    }
    Ok(out)
}

/// Read the namespace's markings projection.
pub async fn for_namespace(
    pool: &PgPool,
    namespace: &Namespace,
) -> Result<NamespaceMarkings, MarkingsError> {
    let explicit_ids: Vec<Uuid> = sqlx::query_scalar(
        "SELECT marking_id FROM iceberg_namespace_markings WHERE namespace_id = $1",
    )
    .bind(namespace.id)
    .fetch_all(pool)
    .await?;
    let projections = project(pool, &explicit_ids).await?;
    Ok(NamespaceMarkings {
        effective: projections.clone(),
        explicit: projections,
    })
}

/// Read the table's markings projection (effective / explicit /
/// inherited).
pub async fn for_table(
    pool: &PgPool,
    table: &IcebergTable,
) -> Result<TableMarkings, MarkingsError> {
    let rows = sqlx::query("SELECT marking_id, source FROM iceberg_table_markings WHERE table_id = $1")
        .bind(table.id)
        .fetch_all(pool)
        .await?;

    let mut explicit_ids = Vec::new();
    let mut inherited_ids = Vec::new();
    for row in rows {
        let id: Uuid = row.try_get("marking_id")?;
        let source: String = row.try_get("source")?;
        match source.as_str() {
            "inherited" => inherited_ids.push(id),
            _ => explicit_ids.push(id),
        }
    }

    let mut effective_ids = explicit_ids.clone();
    effective_ids.extend(inherited_ids.iter().copied());
    effective_ids.sort();
    effective_ids.dedup();

    Ok(TableMarkings {
        effective: project(pool, &effective_ids).await?,
        explicit: project(pool, &explicit_ids).await?,
        inherited_from_namespace: project(pool, &inherited_ids).await?,
    })
}

/// Replace the namespace's explicit markings with `marking_ids`.
/// Returns the new projection.
pub async fn set_namespace_markings(
    pool: &PgPool,
    namespace: &Namespace,
    marking_ids: &[Uuid],
    actor: Uuid,
) -> Result<NamespaceMarkings, MarkingsError> {
    let mut tx = pool.begin().await?;
    sqlx::query("DELETE FROM iceberg_namespace_markings WHERE namespace_id = $1")
        .bind(namespace.id)
        .execute(&mut *tx)
        .await?;
    for id in marking_ids {
        sqlx::query(
            r#"
            INSERT INTO iceberg_namespace_markings (namespace_id, marking_id, created_by)
            VALUES ($1, $2, $3)
            ON CONFLICT (namespace_id, marking_id) DO NOTHING
            "#,
        )
        .bind(namespace.id)
        .bind(id)
        .bind(actor)
        .execute(&mut *tx)
        .await?;
    }
    tx.commit().await?;
    for_namespace(pool, namespace).await
}

/// Replace the table's explicit markings (inherited rows are left
/// untouched). Returns the new projection.
pub async fn set_table_explicit_markings(
    pool: &PgPool,
    table: &IcebergTable,
    marking_ids: &[Uuid],
    actor: Uuid,
) -> Result<TableMarkings, MarkingsError> {
    let mut tx = pool.begin().await?;
    sqlx::query(
        "DELETE FROM iceberg_table_markings WHERE table_id = $1 AND source = 'explicit'",
    )
    .bind(table.id)
    .execute(&mut *tx)
    .await?;
    for id in marking_ids {
        sqlx::query(
            r#"
            INSERT INTO iceberg_table_markings (table_id, marking_id, source, created_by)
            VALUES ($1, $2, 'explicit', $3)
            ON CONFLICT (table_id, marking_id, source) DO NOTHING
            "#,
        )
        .bind(table.id)
        .bind(id)
        .bind(actor)
        .execute(&mut *tx)
        .await?;
    }
    refresh_table_markings_cache_in_tx(&mut tx, table.id).await?;
    tx.commit().await?;
    for_table(pool, table).await
}

/// Snapshot the namespace's current markings into the table as
/// `inherited`. Called once at table creation time per Foundry
/// snapshot semantics.
pub async fn snapshot_namespace_into_table(
    pool: &PgPool,
    namespace: &Namespace,
    table: &IcebergTable,
    actor: Uuid,
) -> Result<(), MarkingsError> {
    let marking_ids: Vec<Uuid> = sqlx::query_scalar(
        "SELECT marking_id FROM iceberg_namespace_markings WHERE namespace_id = $1",
    )
    .bind(namespace.id)
    .fetch_all(pool)
    .await?;

    let mut tx = pool.begin().await?;
    for id in &marking_ids {
        sqlx::query(
            r#"
            INSERT INTO iceberg_table_markings (table_id, marking_id, source, created_by)
            VALUES ($1, $2, 'inherited', $3)
            ON CONFLICT (table_id, marking_id, source) DO NOTHING
            "#,
        )
        .bind(table.id)
        .bind(id)
        .bind(actor)
        .execute(&mut *tx)
        .await?;
    }
    refresh_table_markings_cache_in_tx(&mut tx, table.id).await?;
    tx.commit().await?;
    Ok(())
}

/// Refresh the cached `iceberg_tables.markings TEXT[]` from the
/// effective union of (`inherited` ∪ `explicit`). Kept as a dedicated
/// helper so the multi-table commit handler can call it inside its
/// own transaction.
pub async fn refresh_table_markings_cache_in_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    table_id: Uuid,
) -> Result<(), MarkingsError> {
    sqlx::query(
        r#"
        UPDATE iceberg_tables t
        SET markings = COALESCE((
            SELECT array_agg(DISTINCT mn.name ORDER BY mn.name)
            FROM iceberg_table_markings tm
            JOIN iceberg_marking_names mn ON mn.marking_id = tm.marking_id
            WHERE tm.table_id = t.id
        ), '{}'::TEXT[])
        WHERE t.id = $1
        "#,
    )
    .bind(table_id)
    .execute(&mut **tx)
    .await?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn table_markings_serialise_with_three_buckets() {
        let pid = MarkingId::from_uuid(Uuid::nil());
        let proj = vec![MarkingProjection {
            marking_id: pid,
            name: "public".into(),
            description: "Public".into(),
        }];
        let payload = TableMarkings {
            effective: proj.clone(),
            explicit: proj.clone(),
            inherited_from_namespace: vec![],
        };
        let serialized = serde_json::to_value(&payload).unwrap();
        assert!(serialized["effective"].is_array());
        assert!(serialized["explicit"].is_array());
        assert!(serialized["inherited_from_namespace"].is_array());
    }
}
