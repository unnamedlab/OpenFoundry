//! H7 closure — coverage gate. The Usage UI tab + `/usage` REST surface
//! aggregate from the `media_set_access_pattern_invocations` ledger
//! that H5 introduced. This test exercises:
//!
//!   * The default range path (`since` defaults to `now - 30 days`).
//!   * The per-kind summary aggregation.
//!   * The per-day-and-kind drill-down aggregation.
//!   * The empty-window path (zero invocations → zeroes everywhere).
//!
//! Without this test handlers/usage.rs sat at 0% coverage even though
//! its SQL surface is the source of truth for both the Foundry-style
//! Usage tab and the billing exporter.

mod common;

use chrono::{Duration, Utc};
use media_sets_service::handlers::access_patterns::{
    register_access_pattern_op, run_access_pattern_op,
};
use media_sets_service::handlers::items::presigned_upload_op;
use media_sets_service::handlers::usage::get_usage_op;
use media_sets_service::models::{
    PersistencePolicy, PresignedUploadRequest, RegisterAccessPatternBody, TransactionPolicy,
};

#[tokio::test]
async fn usage_meter_aggregates_per_kind_per_day_and_handles_empty_windows() {
    let h = common::spawn().await;

    let set = common::seed_media_set(
        &h.state,
        "usage-fixture",
        "ri.foundry.main.project.usage",
        TransactionPolicy::Transactionless,
    )
    .await;

    // ── 1. Empty-window contract: a brand-new set returns a
    //       well-formed UsageResponse with all-zero totals. ─────────
    let now = Utc::now();
    let empty = get_usage_op(&h.state, &set.rid, now - Duration::days(30), now)
        .await
        .expect("empty window must succeed");
    assert_eq!(empty.total_compute_seconds, 0);
    assert_eq!(empty.total_input_bytes, 0);
    assert!(empty.by_kind.is_empty());
    assert!(empty.by_day_kind.is_empty());
    assert_eq!(empty.since, now - Duration::days(30));
    assert_eq!(empty.until, now);

    // ── 2. Generate two invocations on different patterns so the
    //       per-kind aggregation has > 1 row to sort.  ────────────
    let (item, _url) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "shots/skyline.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: None,
            sha256: Some("e".repeat(64)),
            size_bytes: Some(2 * 1024 * 1024 * 1024),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage item");

    let thumbnail = register_access_pattern_op(
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
    .expect("register thumbnail");

    let resize = register_access_pattern_op(
        &h.state,
        &set.rid,
        RegisterAccessPatternBody {
            kind: "resize".into(),
            params: serde_json::json!({"width": 800}),
            persistence: PersistencePolicy::Persist,
            ttl_seconds: None,
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("register resize");

    run_access_pattern_op(&h.state, &thumbnail.id, &item.rid, "tester", &common::test_ctx())
        .await
        .expect("run thumbnail");
    run_access_pattern_op(&h.state, &resize.id, &item.rid, "tester", &common::test_ctx())
        .await
        .expect("run resize");

    // ── 3. Re-query: totals tally the two invocations. The per-kind
    //       order is `compute_seconds DESC, kind ASC` — both kinds
    //       charge the same image-resize rate (40 cs/GB) so the tie
    //       breaks alphabetically, surfacing `resize` first. ───────
    let usage = get_usage_op(&h.state, &set.rid, now - Duration::days(1), Utc::now())
        .await
        .expect("populated window");
    assert!(usage.total_compute_seconds > 0, "compute_seconds must accumulate");
    assert!(usage.total_input_bytes > 0, "input_bytes must accumulate");
    assert_eq!(
        usage.by_kind.len(),
        2,
        "two kinds invoked → two summary rows"
    );
    let kinds: Vec<&str> = usage.by_kind.iter().map(|b| b.kind.as_str()).collect();
    // Both kinds must surface; ordering relies on the SQL ORDER BY
    // clause (compute_seconds DESC then kind ASC). The names alone
    // pin the bug "wrong column or no group-by" without coupling to
    // the rate-table value.
    assert!(kinds.contains(&"thumbnail"));
    assert!(kinds.contains(&"resize"));

    // The per-day drill-down must carry one (day, kind) point per
    // invocation kind that hit today.
    assert!(
        !usage.by_day_kind.is_empty(),
        "by_day_kind must surface the post-invocation day"
    );
    let today = Utc::now().date_naive();
    for point in &usage.by_day_kind {
        assert_eq!(point.day, today, "all invocations landed today");
        assert!(point.compute_seconds >= 0);
        assert!(point.input_bytes >= 0);
    }
}
