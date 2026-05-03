//! Path-deduplication primitive shared by the REST and gRPC item-creation
//! paths.
//!
//! Foundry semantics ("Importing media.md" → *Path deduplication*):
//!
//! > If a file is uploaded to a media set and has the same path as an
//! > existing item in the media set, the new item will overwrite the
//! > existing one.
//!
//! Concretely, when a new item is registered for `(media_set_rid, branch,
//! path)` and a *live* (non-soft-deleted) item already exists at that
//! path, [`soft_delete_previous_at_path`] flips its `deleted_at` to
//! `NOW()` and returns its RID so the caller can store it as
//! `deduplicated_from` on the new row. Both writes happen inside the
//! same SQL transaction the caller passes in, so the partial unique
//! index `uq_media_items_live_path` cannot be violated mid-flight.

use sqlx::{Postgres, Transaction};

use crate::domain::error::MediaResult;

/// Soft-delete the live item at `(media_set_rid, branch, path)`, if any,
/// and return its RID. Returns `None` when no live item exists.
pub async fn soft_delete_previous_at_path(
    tx: &mut Transaction<'_, Postgres>,
    media_set_rid: &str,
    branch: &str,
    path: &str,
) -> MediaResult<Option<String>> {
    let row: Option<(String,)> = sqlx::query_as(
        r#"UPDATE media_items
              SET deleted_at = NOW()
            WHERE media_set_rid = $1
              AND branch        = $2
              AND path          = $3
              AND deleted_at IS NULL
        RETURNING rid"#,
    )
    .bind(media_set_rid)
    .bind(branch)
    .bind(path)
    .fetch_optional(&mut **tx)
    .await?;

    Ok(row.map(|(rid,)| rid))
}
