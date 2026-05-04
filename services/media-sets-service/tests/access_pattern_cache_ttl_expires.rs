//! H5 — `CACHE_TTL` access patterns recompute after the per-row TTL
//! expires. We can't `sleep` for a real TTL window in CI, so we
//! short-circuit by directly aging the row's `expires_at` to a past
//! timestamp and confirming the next run misses + recomputes.
//!
//! Sister to `access_pattern_persist_caches_second_call.rs`: that one
//! covers the cache-hit path; this one covers the cache-miss-on-stale
//! path.

mod common;

use chrono::{Duration, Utc};
use media_sets_service::handlers::access_patterns::{
    register_access_pattern_op, run_access_pattern_op,
};
use media_sets_service::handlers::items::presigned_upload_op;
use media_sets_service::models::{
    PersistencePolicy, PresignedUploadRequest, RegisterAccessPatternBody, TransactionPolicy,
};

#[tokio::test]
async fn cache_ttl_pattern_expires_and_next_call_recomputes() {
    let h = common::spawn().await;

    let set = common::seed_media_set(
        &h.state,
        "ttl-set",
        "ri.foundry.main.project.ttl",
        TransactionPolicy::Transactionless,
    )
    .await;

    let (item, _url) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "ttl/photo.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: None,
            sha256: Some("b".repeat(64)),
            size_bytes: Some(1 * 1024 * 1024 * 1024),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage item");

    let pattern = register_access_pattern_op(
        &h.state,
        &set.rid,
        RegisterAccessPatternBody {
            kind: "thumbnail".into(),
            params: serde_json::json!({"max_dim": 128}),
            persistence: PersistencePolicy::CacheTtl,
            // 1h nominal TTL — we expire the row out-of-band below.
            ttl_seconds: Some(3600),
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("register pattern");

    // First call: miss, charges, writes cache row with future expires_at.
    let first = run_access_pattern_op(
        &h.state,
        &pattern.id,
        &item.rid,
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("first run");
    assert!(!first.cache_hit);
    assert!(first.compute_seconds > 0);

    // Second call: hit (warm cache).
    let second = run_access_pattern_op(
        &h.state,
        &pattern.id,
        &item.rid,
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("second run");
    assert!(second.cache_hit, "second call must hit the warm cache");

    // Age the Postgres row AND clear the moka snapshot so the next
    // call re-evaluates the (now stale) cache entry. In production
    // moka's natural eviction handles this; in test we short-circuit
    // both layers to avoid sleeping for an hour.
    sqlx::query("UPDATE media_set_access_pattern_outputs SET expires_at = $1 WHERE pattern_id = $2")
        .bind(Utc::now() - Duration::seconds(60))
        .bind(&pattern.id)
        .execute(&h.pool)
        .await
        .expect("age cache row");
    media_sets_service::handlers::access_patterns::clear_in_process_cache_for_tests().await;

    // Third call: miss (stale TTL), recomputes + charges fresh
    // compute-seconds again.
    let third = run_access_pattern_op(
        &h.state,
        &pattern.id,
        &item.rid,
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("third run");
    assert!(
        !third.cache_hit,
        "stale TTL must miss the cache (got cache_hit={})",
        third.cache_hit
    );
    assert!(
        third.compute_seconds > 0,
        "stale TTL must recompute and re-charge (got compute_seconds={})",
        third.compute_seconds
    );
}
