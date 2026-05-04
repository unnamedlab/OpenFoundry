//! Retention reaper: enforces the Foundry retention contract on a
//! periodic Tokio task.
//!
//! Contract (`docs_original_palantir_foundry/.../Advanced media set settings.md`):
//!
//! > Once a media item's retention window expires, it will never become
//! > accessible again, and will be deleted.
//! > * Reducing the window → items older than the new window become
//! >   inaccessible immediately.
//! > * Expanding the window → previously-expired items do NOT become
//! >   accessible again. Same when retention is changed to "forever".
//!
//! The "expansion does not restore" half is enforced by the schema
//! itself: nothing in this module ever clears `deleted_at`. Once
//! soft-deleted, a row stays soft-deleted; the reaper only ever flips
//! `NULL → NOW()`.
//!
//! The "reduction is immediate" half is enforced by:
//!
//! 1. The PATCH handler in [`crate::handlers::media_sets`] running a
//!    one-shot [`reap_media_set`] on the affected set.
//! 2. The background loop in [`spawn_reaper`] re-scanning every set
//!    once per `interval`.
//!
//! Both call sites compute the *current effective* expiration by
//! JOINing items with their parent `media_sets.retention_seconds` —
//! they ignore the per-item `retention_seconds` snapshot because it
//! reflects the value at INSERT time, not the current setting.

use std::sync::Arc;
use std::time::Duration;

use sqlx::PgPool;

use crate::domain::path::MediaItemKey;
use crate::domain::storage::MediaStorage;

/// One iteration of the reaper. Returns the list of (rid, sha256, size)
/// tuples that were just expired so the caller can drop the bytes from
/// the backing store and emit one audit event per item.
pub async fn reap_due(pool: &PgPool) -> Result<Vec<ExpiredItem>, sqlx::Error> {
    // Compute the *current* effective expiration via JOIN with the
    // parent media set. The per-item `retention_seconds` snapshot is
    // intentionally not used here — a PATCH that reduced retention
    // would otherwise be ignored until the snapshot was rewritten.
    let rows: Vec<ExpiredItem> = sqlx::query_as(
        r#"UPDATE media_items i
              SET deleted_at = NOW()
             FROM media_sets s
            WHERE i.media_set_rid = s.rid
              AND i.deleted_at IS NULL
              AND s.retention_seconds > 0
              AND i.created_at + s.retention_seconds * interval '1 second' < NOW()
        RETURNING i.rid, i.media_set_rid, i.branch, i.sha256, i.size_bytes"#,
    )
    .fetch_all(pool)
    .await?;
    Ok(rows)
}

/// Reaper restricted to a single media set. Called synchronously from
/// the PATCH handler so a retention reduction shows up in subsequent
/// reads without waiting for the next periodic pass.
pub async fn reap_media_set(
    pool: &PgPool,
    media_set_rid: &str,
) -> Result<Vec<ExpiredItem>, sqlx::Error> {
    let rows: Vec<ExpiredItem> = sqlx::query_as(
        r#"UPDATE media_items i
              SET deleted_at = NOW()
             FROM media_sets s
            WHERE i.media_set_rid = s.rid
              AND i.media_set_rid = $1
              AND i.deleted_at IS NULL
              AND s.retention_seconds > 0
              AND i.created_at + s.retention_seconds * interval '1 second' < NOW()
        RETURNING i.rid, i.media_set_rid, i.branch, i.sha256, i.size_bytes"#,
    )
    .bind(media_set_rid)
    .fetch_all(pool)
    .await?;
    Ok(rows)
}

/// Drop the byte payloads for items the reaper just expired. Failures
/// are logged but never block the run — the metadata row is the source
/// of truth and the byte may already have been GC'd.
pub async fn drop_bytes(storage: &dyn MediaStorage, expired: &[ExpiredItem]) {
    for item in expired {
        if item.sha256.is_empty() {
            continue;
        }
        let key = MediaItemKey::new(&item.media_set_rid, &item.branch, &item.sha256);
        if let Err(err) = storage.delete(&key).await {
            tracing::warn!(rid = %item.rid, error = %err, "byte cleanup failed");
        }
    }
}

/// Emit one audit event per expired item. Today this drops the event
/// into the `audit` tracing target the way `audit_trail::middleware`
/// already does for HTTP requests; the full bus wiring (Kafka /
/// `audit-compliance-service`) is the H3 deliverable.
pub fn emit_audit(expired: &[ExpiredItem]) {
    for item in expired {
        tracing::info!(
            target: "audit",
            event = "media_item.retention_expired",
            media_item_rid = %item.rid,
            media_set_rid = %item.media_set_rid,
            sha256 = %item.sha256,
            size_bytes = item.size_bytes,
            "media item soft-deleted by retention reaper"
        );
    }
    if !expired.is_empty() {
        crate::metrics::MEDIA_RETENTION_PURGES_TOTAL.inc_by(expired.len() as u64);
    }
}

/// Spawn the periodic reaper. Returns the task handle so callers can
/// abort it on shutdown.
pub fn spawn_reaper(
    pool: PgPool,
    storage: Arc<dyn MediaStorage>,
    interval: Duration,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(interval);
        // The first tick fires immediately — skip it so we don't reap
        // before the service has had a chance to settle.
        ticker.tick().await;
        loop {
            ticker.tick().await;
            match reap_due(&pool).await {
                Ok(expired) if expired.is_empty() => {
                    tracing::debug!("retention reaper: nothing to expire");
                }
                Ok(expired) => {
                    tracing::info!(count = expired.len(), "retention reaper: expired items");
                    drop_bytes(storage.as_ref(), &expired).await;
                    emit_audit(&expired);
                }
                Err(err) => {
                    tracing::error!(error = %err, "retention reaper sweep failed");
                }
            }
        }
    })
}

/// Row returned by the reaper update; surfaced to the caller so byte
/// cleanup + audit emission stay decoupled from the SQL itself.
#[derive(Debug, Clone, sqlx::FromRow)]
pub struct ExpiredItem {
    pub rid: String,
    pub media_set_rid: String,
    pub branch: String,
    pub sha256: String,
    pub size_bytes: i64,
}
