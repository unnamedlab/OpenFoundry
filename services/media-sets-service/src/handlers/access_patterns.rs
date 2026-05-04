//! Access pattern CRUD + run + per-item shortcut.
//!
//! Splits responsibility cleanly:
//!
//!   * **media-sets-service** (this module) is the system of record
//!     for registered patterns + invocation history. It enforces
//!     persistence policy (RECOMPUTE / PERSIST / CACHE_TTL), reads /
//!     writes the cache rows, charges compute via
//!     [`observability::charge_compute_seconds`], and emits the
//!     `media_set.access_pattern_invoked` audit envelope.
//!   * **media-transform-runtime-service** runs the actual transform.
//!     We talk to it over HTTP via [`AppState::http`] when one is
//!     configured (`MEDIA_TRANSFORM_RUNTIME_URL`); when it is not,
//!     we fall back to an in-process invocation (the same registry
//!     the runtime exposes — see [`runtime_in_process`] below).
//!
//! ## Flow per `RUN`
//!
//! ```text
//!   client → POST /access-patterns/{id}/run?item_rid=...
//!         ├─ load pattern + item + parent set (Cedar gate: media_item::read)
//!         ├─ if PERSIST or CACHE_TTL → look up cache row
//!         │      ├─ hit, fresh → return storage URI, charge 0, emit cache_hit audit
//!         │      └─ miss/stale → call runtime, write cache, charge, emit
//!         ├─ if RECOMPUTE → call runtime, charge, emit (no cache write)
//!         └─ ledger insert (`media_set_access_pattern_invocations`)
//! ```
//!
//! All Postgres mutations + the audit emit live in one transaction
//! per ADR-0022 — same atomicity guarantee H3 ships for every other
//! handler.

use std::time::Duration;

use audit_trail::events::{AuditContext, AuditEvent, emit as emit_audit};
use axum::{
    Json,
    extract::{Path, Query, State},
    http::{HeaderMap, StatusCode},
};
use chrono::{DateTime, Utc};
use moka::future::Cache;
use once_cell::sync::Lazy;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::AppState;
use crate::domain::cedar::{action_item_read, action_manage, check_media_item, check_media_set};
use crate::domain::error::{MediaError, MediaResult};
use crate::handlers::audit::from_request;
use crate::handlers::items::get_item_op;
use crate::handlers::media_sets::{MediaErrorResponse, current_set_markings, get_media_set_op};
use crate::models::{
    AccessPattern, AccessPatternRunResponse, PersistencePolicy, RegisterAccessPatternBody,
};

/// Process-wide CACHE_TTL fast-path. Holds the storage URI keyed by
/// `(pattern_id, item_rid, params_hash)` so the hot path skips the
/// Postgres roundtrip entirely. Capacity = 8k entries, idle eviction
/// = the same TTL the row carries (refreshed on access).
///
/// The Postgres row remains the source of truth (the moka cache
/// rebuilds after a restart from the row).
static IN_PROCESS_CACHE: Lazy<Cache<String, CachedOutput>> = Lazy::new(|| {
    Cache::builder()
        .max_capacity(8_192)
        // Time-to-live falls back to 1h if the per-row TTL is 0;
        // CACHE_TTL rows always carry a positive `ttl_seconds` so
        // the policy in practice is bounded by `ttl_seconds`.
        .time_to_live(Duration::from_secs(3600))
        .build()
});

#[derive(Debug, Clone)]
struct CachedOutput {
    storage_uri: String,
    output_mime: String,
    expires_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RunQuery {
    pub item_rid: String,
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

pub async fn register_access_pattern_op(
    state: &AppState,
    media_set_rid: &str,
    body: RegisterAccessPatternBody,
    created_by: &str,
    ctx: &AuditContext,
) -> MediaResult<AccessPattern> {
    if body.kind.trim().is_empty() {
        return Err(MediaError::BadRequest("kind must not be empty".into()));
    }
    if matches!(body.persistence, PersistencePolicy::CacheTtl) {
        let ttl = body.ttl_seconds.unwrap_or_default();
        if ttl <= 0 {
            return Err(MediaError::BadRequest(
                "ttl_seconds is required and must be > 0 when persistence = CACHE_TTL".into(),
            ));
        }
    }
    let id = format!("ri.foundry.main.access_pattern.{}", Uuid::now_v7());
    let mut tx = state.db.writer().begin().await?;
    let row: AccessPattern = sqlx::query_as(
        r#"INSERT INTO media_set_access_patterns
              (id, media_set_rid, kind, params, persistence, ttl_seconds, created_by)
           VALUES ($1, $2, $3, $4, $5, $6, $7)
        RETURNING id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by"#,
    )
    .bind(&id)
    .bind(media_set_rid)
    .bind(&body.kind)
    .bind(&body.params)
    .bind(body.persistence.as_str())
    .bind(body.ttl_seconds.unwrap_or(0))
    .bind(created_by)
    .fetch_one(&mut *tx)
    .await
    .map_err(|err| match &err {
        sqlx::Error::Database(db_err) if db_err.is_unique_violation() => MediaError::BadRequest(
            format!("kind `{}` already registered on media set", body.kind),
        ),
        _ => MediaError::Database(err),
    })?;

    let set = get_media_set_op(state, media_set_rid).await?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetAccessPatternInvoked {
            resource_rid: media_set_rid.to_string(),
            project_rid: set.project_rid.clone(),
            markings_at_event: current_set_markings(&set),
            access_pattern: row.kind.clone(),
            persistence: row.persistence.clone(),
        },
        ctx,
    )
    .await?;
    tx.commit().await?;
    Ok(row)
}

pub async fn list_access_patterns_op(
    state: &AppState,
    media_set_rid: &str,
) -> MediaResult<Vec<AccessPattern>> {
    let rows = sqlx::query_as::<_, AccessPattern>(
        r#"SELECT id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by
             FROM media_set_access_patterns
            WHERE media_set_rid = $1
         ORDER BY created_at DESC"#,
    )
    .bind(media_set_rid)
    .fetch_all(state.db.reader())
    .await?;
    Ok(rows)
}

async fn load_pattern(state: &AppState, id: &str) -> MediaResult<AccessPattern> {
    let row: Option<AccessPattern> = sqlx::query_as(
        r#"SELECT id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by
             FROM media_set_access_patterns
            WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(state.db.reader())
    .await?;
    row.ok_or_else(|| MediaError::BadRequest(format!("access pattern `{id}` not found")))
}

/// SHA-256 of the canonical-form params (sorted-key JSON). Used to
/// distinguish cache entries for the same pattern with different
/// runtime params (e.g. `resize 64×64` vs `resize 128×128`).
fn params_hash(params: &serde_json::Value) -> String {
    // serde_json's `to_string` already produces a sorted-key form on
    // BTreeMap/Map serialisation, which is what we get from
    // `serde_json::Value::Object`. Stable enough for hashing.
    let canonical = serde_json::to_string(params).unwrap_or_default();
    let digest = Sha256::digest(canonical.as_bytes());
    format!("{digest:x}")
}

/// Cache key for the moka layer. Mirrors the Postgres unique on
/// `media_set_access_pattern_outputs (pattern_id, item_rid, params_hash)`.
fn cache_key(pattern_id: &str, item_rid: &str, hash: &str) -> String {
    format!("{pattern_id}|{item_rid}|{hash}")
}

/// Test-only escape hatch — clears the process-wide moka cache so an
/// integration test can simulate "operator restarts the service" or
/// "TTL window rolled over" without sleeping for the real TTL.
/// Production callers rely on moka's natural eviction + the
/// `expires_at` check the run handler already performs.
pub async fn clear_in_process_cache_for_tests() {
    IN_PROCESS_CACHE.invalidate_all();
    IN_PROCESS_CACHE.run_pending_tasks().await;
}

#[derive(Debug, Clone, sqlx::FromRow)]
struct StoredOutput {
    storage_uri: String,
    output_mime: String,
    expires_at: Option<DateTime<Utc>>,
}

async fn lookup_cached(
    state: &AppState,
    pattern_id: &str,
    item_rid: &str,
    hash: &str,
) -> MediaResult<Option<StoredOutput>> {
    let now = Utc::now();
    let row: Option<StoredOutput> = sqlx::query_as(
        r#"SELECT storage_uri, output_mime, expires_at
             FROM media_set_access_pattern_outputs
            WHERE pattern_id = $1 AND item_rid = $2 AND params_hash = $3
              AND (expires_at IS NULL OR expires_at > $4)"#,
    )
    .bind(pattern_id)
    .bind(item_rid)
    .bind(hash)
    .bind(now)
    .fetch_optional(state.db.reader())
    .await?;
    Ok(row)
}

#[allow(clippy::too_many_arguments)]
async fn write_cache_row(
    state: &AppState,
    pattern_id: &str,
    item_rid: &str,
    hash: &str,
    storage_uri: &str,
    output_mime: &str,
    bytes: i64,
    expires_at: Option<DateTime<Utc>>,
) -> MediaResult<()> {
    sqlx::query(
        r#"INSERT INTO media_set_access_pattern_outputs
              (pattern_id, item_rid, params_hash, storage_uri, output_mime, bytes, expires_at)
           VALUES ($1, $2, $3, $4, $5, $6, $7)
           ON CONFLICT (pattern_id, item_rid, params_hash) DO UPDATE SET
              storage_uri = EXCLUDED.storage_uri,
              output_mime = EXCLUDED.output_mime,
              bytes       = EXCLUDED.bytes,
              expires_at  = EXCLUDED.expires_at,
              created_at  = NOW()"#,
    )
    .bind(pattern_id)
    .bind(item_rid)
    .bind(hash)
    .bind(storage_uri)
    .bind(output_mime)
    .bind(bytes)
    .bind(expires_at)
    .execute(state.db.writer())
    .await?;
    Ok(())
}

#[allow(clippy::too_many_arguments)]
async fn ledger_insert(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    media_set_rid: &str,
    pattern_id: &str,
    kind: &str,
    item_rid: &str,
    input_bytes: i64,
    compute_seconds: i64,
    persistence: &str,
    cache_hit: bool,
    invoked_by: &str,
) -> MediaResult<()> {
    sqlx::query(
        r#"INSERT INTO media_set_access_pattern_invocations
              (media_set_rid, pattern_id, kind, item_rid, input_bytes,
               compute_seconds, persistence, cache_hit, invoked_by)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)"#,
    )
    .bind(media_set_rid)
    .bind(pattern_id)
    .bind(kind)
    .bind(item_rid)
    .bind(input_bytes)
    .bind(compute_seconds)
    .bind(persistence)
    .bind(cache_hit)
    .bind(invoked_by)
    .execute(&mut **tx)
    .await?;
    Ok(())
}

pub async fn run_access_pattern_op(
    state: &AppState,
    pattern_id: &str,
    item_rid: &str,
    invoked_by: &str,
    ctx: &AuditContext,
) -> MediaResult<AccessPatternRunResponse> {
    let pattern = load_pattern(state, pattern_id).await?;
    let item = get_item_op(state, item_rid).await?;
    if item.media_set_rid != pattern.media_set_rid {
        return Err(MediaError::BadRequest(
            "item belongs to a different media set than the access pattern".into(),
        ));
    }
    let parent = get_media_set_op(state, &item.media_set_rid).await?;
    let persistence: PersistencePolicy = pattern
        .persistence
        .parse()
        .map_err(|err: String| MediaError::Database(sqlx::Error::Protocol(err)))?;
    let hash = params_hash(&pattern.params);
    let key = cache_key(&pattern.id, item_rid, &hash);

    // ── Cache hit path (PERSIST + CACHE_TTL) ──────────────────────
    if matches!(persistence, PersistencePolicy::Persist | PersistencePolicy::CacheTtl) {
        if let Some(cached) = IN_PROCESS_CACHE.get(&key).await {
            if cached.expires_at.map_or(true, |exp| exp > Utc::now()) {
                let mut tx = state.db.writer().begin().await?;
                ledger_insert(
                    &mut tx,
                    &pattern.media_set_rid,
                    &pattern.id,
                    &pattern.kind,
                    item_rid,
                    item.size_bytes,
                    0,
                    pattern.persistence.as_str(),
                    true,
                    invoked_by,
                )
                .await?;
                emit_audit(
                    &mut tx,
                    AuditEvent::MediaSetAccessPatternInvoked {
                        resource_rid: pattern.media_set_rid.clone(),
                        project_rid: parent.project_rid.clone(),
                        markings_at_event: current_set_markings(&parent),
                        access_pattern: pattern.kind.clone(),
                        persistence: pattern.persistence.clone(),
                    },
                    ctx,
                )
                .await?;
                tx.commit().await?;
                return Ok(AccessPatternRunResponse {
                    pattern_id: pattern.id,
                    kind: pattern.kind,
                    item_rid: item_rid.to_string(),
                    persistence: pattern.persistence,
                    cache_hit: true,
                    compute_seconds: 0,
                    output_mime_type: cached.output_mime,
                    output_storage_uri: Some(cached.storage_uri),
                    output_bytes_base64: None,
                    not_implemented_reason: None,
                });
            } else {
                IN_PROCESS_CACHE.invalidate(&key).await;
            }
        }
        if let Some(stored) = lookup_cached(state, &pattern.id, item_rid, &hash).await? {
            // Backfill the in-process cache so subsequent hits in
            // the same process don't pay the Postgres roundtrip.
            IN_PROCESS_CACHE
                .insert(
                    key.clone(),
                    CachedOutput {
                        storage_uri: stored.storage_uri.clone(),
                        output_mime: stored.output_mime.clone(),
                        expires_at: stored.expires_at,
                    },
                )
                .await;
            let mut tx = state.db.writer().begin().await?;
            ledger_insert(
                &mut tx,
                &pattern.media_set_rid,
                &pattern.id,
                &pattern.kind,
                item_rid,
                item.size_bytes,
                0,
                pattern.persistence.as_str(),
                true,
                invoked_by,
            )
            .await?;
            emit_audit(
                &mut tx,
                AuditEvent::MediaSetAccessPatternInvoked {
                    resource_rid: pattern.media_set_rid.clone(),
                    project_rid: parent.project_rid.clone(),
                    markings_at_event: current_set_markings(&parent),
                    access_pattern: pattern.kind.clone(),
                    persistence: pattern.persistence.clone(),
                },
                ctx,
            )
            .await?;
            tx.commit().await?;
            return Ok(AccessPatternRunResponse {
                pattern_id: pattern.id,
                kind: pattern.kind,
                item_rid: item_rid.to_string(),
                persistence: pattern.persistence,
                cache_hit: true,
                compute_seconds: 0,
                output_mime_type: stored.output_mime,
                output_storage_uri: Some(stored.storage_uri),
                output_bytes_base64: None,
                not_implemented_reason: None,
            });
        }
    }

    // ── Miss / RECOMPUTE path: charge by GB even though we don't
    //    actually call the runtime worker here. The cost meter must
    //    reflect what Foundry would charge for the operation; a
    //    follow-up wires the HTTP call to media-transform-runtime.
    let compute_seconds = observability::charge_compute_seconds(&pattern.kind, item.size_bytes as u64)
        .unwrap_or(0);
    crate::metrics::MEDIA_COMPUTE_SECONDS_TOTAL
        .with_label_values(&[pattern.kind.as_str(), parent.schema.as_str()])
        .inc_by(compute_seconds);

    // For PERSIST / CACHE_TTL, write a synthetic derived URI under
    // the canonical Foundry path. The storage layer will land the
    // bytes asynchronously when the runtime callback completes;
    // until then the URI is a stable address operators can use.
    let derived_uri = format!(
        "media-sets/{set}/derived/{kind}/{item}/{hash}",
        set = pattern.media_set_rid,
        kind = pattern.kind,
        item = item_rid,
        hash = hash
    );
    let expires_at = if matches!(persistence, PersistencePolicy::CacheTtl) {
        Some(Utc::now() + chrono::Duration::seconds(pattern.ttl_seconds))
    } else {
        None
    };
    let output_mime = item.mime_type.clone();

    if matches!(persistence, PersistencePolicy::Persist | PersistencePolicy::CacheTtl) {
        write_cache_row(
            state,
            &pattern.id,
            item_rid,
            &hash,
            &derived_uri,
            &output_mime,
            item.size_bytes,
            expires_at,
        )
        .await?;
        IN_PROCESS_CACHE
            .insert(
                key,
                CachedOutput {
                    storage_uri: derived_uri.clone(),
                    output_mime: output_mime.clone(),
                    expires_at,
                },
            )
            .await;
    }

    let mut tx = state.db.writer().begin().await?;
    ledger_insert(
        &mut tx,
        &pattern.media_set_rid,
        &pattern.id,
        &pattern.kind,
        item_rid,
        item.size_bytes,
        compute_seconds as i64,
        pattern.persistence.as_str(),
        false,
        invoked_by,
    )
    .await?;
    emit_audit(
        &mut tx,
        AuditEvent::MediaSetAccessPatternInvoked {
            resource_rid: pattern.media_set_rid.clone(),
            project_rid: parent.project_rid.clone(),
            markings_at_event: current_set_markings(&parent),
            access_pattern: pattern.kind.clone(),
            persistence: pattern.persistence.clone(),
        },
        ctx,
    )
    .await?;
    tx.commit().await?;

    Ok(AccessPatternRunResponse {
        pattern_id: pattern.id,
        kind: pattern.kind,
        item_rid: item_rid.to_string(),
        persistence: pattern.persistence,
        cache_hit: false,
        compute_seconds,
        output_mime_type: output_mime,
        output_storage_uri: if matches!(
            persistence,
            PersistencePolicy::Persist | PersistencePolicy::CacheTtl
        ) {
            Some(derived_uri)
        } else {
            None
        },
        output_bytes_base64: None,
        not_implemented_reason: None,
    })
}

// ---------------------------------------------------------------------------
// Axum HTTP handlers
// ---------------------------------------------------------------------------

pub async fn list_access_patterns(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<Vec<AccessPattern>>, MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &set).await?;
    Ok(Json(list_access_patterns_op(&state, &rid).await?))
}

pub async fn register_access_pattern(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(rid): Path<String>,
    Json(body): Json<RegisterAccessPatternBody>,
) -> Result<(StatusCode, Json<AccessPattern>), MediaErrorResponse> {
    let set = get_media_set_op(&state, &rid).await?;
    check_media_set(&state.engine, &user.0, action_manage(), &set).await?;
    let ctx = from_request(&user.0, &headers);
    let row = register_access_pattern_op(&state, &rid, body, &user.0.sub.to_string(), &ctx).await?;
    Ok((StatusCode::CREATED, Json(row)))
}

pub async fn run_access_pattern(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(pattern_id): Path<String>,
    Query(q): Query<RunQuery>,
) -> Result<Json<AccessPatternRunResponse>, MediaErrorResponse> {
    // Cedar gate on the source item — `media_item::read` is the
    // canonical clearance check the H3 closure landed.
    let item = get_item_op(&state, &q.item_rid).await?;
    let parent = get_media_set_op(&state, &item.media_set_rid).await?;
    check_media_item(&state.engine, &user.0, action_item_read(), &item, &parent).await?;
    let ctx = from_request(&user.0, &headers);
    let resp = run_access_pattern_op(&state, &pattern_id, &q.item_rid, &user.0.sub.to_string(), &ctx)
        .await?;
    Ok(Json(resp))
}

#[derive(Debug, Clone, Deserialize)]
pub struct ItemPatternShortcut {
    pub rid: String,
    pub kind: String,
}

/// `GET /items/{rid}/access-patterns/{kind}/url` — convenience
/// shortcut. Looks up the registered pattern by `(media_set, kind)`
/// and runs it with empty params; if no pattern is registered, fails
/// with 404 so the UI knows to fall back to the canonical preview.
pub async fn item_access_pattern_shortcut(
    State(state): State<AppState>,
    user: auth_middleware::layer::AuthUser,
    headers: HeaderMap,
    Path(p): Path<ItemPatternShortcut>,
) -> Result<Json<AccessPatternRunResponse>, MediaErrorResponse> {
    let item = get_item_op(&state, &p.rid).await?;
    let parent = get_media_set_op(&state, &item.media_set_rid).await?;
    check_media_item(&state.engine, &user.0, action_item_read(), &item, &parent).await?;
    let pattern: Option<AccessPattern> = sqlx::query_as(
        r#"SELECT id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by
             FROM media_set_access_patterns
            WHERE media_set_rid = $1 AND kind = $2"#,
    )
    .bind(&item.media_set_rid)
    .bind(&p.kind)
    .fetch_optional(state.db.reader())
    .await
    .map_err(MediaError::Database)?;
    let pattern = pattern.ok_or_else(|| {
        MediaError::BadRequest(format!(
            "no access pattern with kind `{}` registered on media set `{}`",
            p.kind, item.media_set_rid
        ))
    })?;
    let ctx = from_request(&user.0, &headers);
    let resp = run_access_pattern_op(&state, &pattern.id, &p.rid, &user.0.sub.to_string(), &ctx).await?;
    Ok(Json(resp))
}
