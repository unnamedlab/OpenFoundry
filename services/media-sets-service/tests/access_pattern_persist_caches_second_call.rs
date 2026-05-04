//! H5 — `PERSIST` access patterns are computed once and served from
//! the cache row on every subsequent invocation.
//!
//! The first run charges compute-seconds via the cost meter and
//! writes a row into `media_set_access_pattern_outputs`. The second
//! run on the same `(pattern, item)` returns `cache_hit = true` with
//! `compute_seconds = 0` — no double-charge — and returns the same
//! `output_storage_uri`.

mod common;

use media_sets_service::handlers::access_patterns::{
    register_access_pattern_op, run_access_pattern_op,
};
use media_sets_service::handlers::items::presigned_upload_op;
use media_sets_service::models::{
    PersistencePolicy, PresignedUploadRequest, RegisterAccessPatternBody, TransactionPolicy,
};

#[tokio::test]
async fn persist_pattern_caches_second_call_with_zero_compute_seconds() {
    let h = common::spawn().await;

    let set = common::seed_media_set(
        &h.state,
        "thumbnails-set",
        "ri.foundry.main.project.thumbs",
        TransactionPolicy::Transactionless,
    )
    .await;

    // Stage one image item the access pattern can chew on.
    let (item, _url) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "shots/skyline.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: None,
            sha256: Some("a".repeat(64)),
            size_bytes: Some(2 * 1024 * 1024 * 1024), // 2 GiB so compute > 0
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage item");

    // Register a PERSIST thumbnail pattern (cost row exists, charges
    // resize rate).
    let pattern = register_access_pattern_op(
        &h.state,
        &set.rid,
        RegisterAccessPatternBody {
            kind: "thumbnail".into(),
            params: serde_json::json!({"max_dim": 256}),
            persistence: PersistencePolicy::Persist,
            ttl_seconds: None,
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("register pattern");

    // ── First run: miss, charges compute, writes cache row ──────
    let first = run_access_pattern_op(
        &h.state,
        &pattern.id,
        &item.rid,
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("first run");
    assert!(!first.cache_hit, "first run must miss the cache");
    assert!(
        first.compute_seconds > 0,
        "first run must charge compute_seconds (got {})",
        first.compute_seconds
    );
    let first_uri = first
        .output_storage_uri
        .clone()
        .expect("PERSIST pattern must surface a derived storage URI");

    // ── Second run: hit, charges 0, same URI ────────────────────
    let second = run_access_pattern_op(
        &h.state,
        &pattern.id,
        &item.rid,
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("second run");
    assert!(
        second.cache_hit,
        "second run on the same (pattern, item) must serve from cache"
    );
    assert_eq!(
        second.compute_seconds, 0,
        "cache hits must not double-charge compute_seconds"
    );
    assert_eq!(second.output_storage_uri.as_deref(), Some(first_uri.as_str()));

    // The ledger must record both invocations: one miss + one hit.
    let counts: (i64, i64) = sqlx::query_as(
        r#"SELECT
              COUNT(*) FILTER (WHERE NOT cache_hit)::bigint,
              COUNT(*) FILTER (WHERE cache_hit)::bigint
             FROM media_set_access_pattern_invocations
            WHERE pattern_id = $1"#,
    )
    .bind(&pattern.id)
    .fetch_one(&h.pool)
    .await
    .expect("count invocations");
    assert_eq!(counts, (1, 1), "ledger must hold exactly 1 miss + 1 hit");
}
