//! H7 — Sensitive Data Scanner pass over a PDF media set.
//!
//! Mirrors the Foundry "SDS / Media set scanning" doc contract:
//!   * The scanner runs against media items addressable by their item
//!     RID; for documents the upstream OCR/extract-text pass produces
//!     PII findings keyed by tag (GovernmentId, Email, …).
//!   * The dispatcher pre-checks the per-tenant quota before queueing
//!     items.
//!   * The aggregate report distinguishes "PII found" from "scanned
//!     clean" so the SDS UI can paint the per-item badge.
//!
//! We keep the upstream OCR runtime mocked: the production scanner
//! ships in a future binary that pulls
//! `media-transform-runtime-service`'s `doc_ocr` access pattern. The
//! contract tested here lives at the trait boundary.

mod common;

use media_scanner::{
    MediaScanner, MockMediaScanRuntime, PiiTag, ScanError, SdsFinding, SdsScanReport,
};
use media_sets_service::handlers::items::presigned_upload_op;
use media_sets_service::models::{PresignedUploadRequest, TransactionPolicy};

#[tokio::test]
async fn sds_scan_finds_government_id_and_email_in_pdf_via_ocr_runtime() {
    let h = common::spawn().await;

    let set = common::seed_media_set(
        &h.state,
        "kyc-pdfs",
        "ri.foundry.main.project.sds",
        TransactionPolicy::Transactionless,
    )
    .await;

    // Stage one document item — the bytes are irrelevant since the
    // scanner mocks the OCR layer; only the item RID must exist so
    // the SDS dispatcher can address it.
    let (item, _url) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "kyc/applicant-1.pdf".into(),
            mime_type: "application/pdf".into(),
            branch: Some("main".into()),
            transaction_rid: None,
            sha256: Some("c".repeat(64)),
            size_bytes: Some(512 * 1024),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage PDF item");

    // ── 1. Wire a mock SDS runtime with a scripted report for our
    //       item: a GovernmentId hit on page 1 and an Email hit on
    //       page 2. ──────────────────────────────────────────────────
    let scanner = MockMediaScanRuntime::new();
    scanner.put_report(
        &item.rid,
        SdsScanReport {
            media_set_rid: set.rid.clone(),
            item_rid: item.rid.clone(),
            findings: vec![
                SdsFinding {
                    media_set_rid: set.rid.clone(),
                    item_rid: item.rid.clone(),
                    tag: PiiTag::GovernmentId,
                    matched: "***-**-6789".into(), // upstream redacts
                    confidence: 0.97,
                    page: Some(0),
                },
                SdsFinding {
                    media_set_rid: set.rid.clone(),
                    item_rid: item.rid.clone(),
                    tag: PiiTag::Email,
                    matched: "ops@example.com".into(),
                    confidence: 0.92,
                    page: Some(1),
                },
            ],
        },
    );
    // Per-tenant quota — dispatcher checks this before enqueueing.
    scanner.put_quota("acme", 10_000);

    // ── 2. Pre-flight quota check passes. ─────────────────────────
    let remaining = scanner.quota_remaining("acme").await;
    assert_eq!(remaining, Some(10_000), "tenant must have headroom");

    // ── 3. Dispatch the scan. The doc-canonical contract: the
    //       returned report carries the exact tags the upstream
    //       detector hit, with page indices for documents. ────────
    let report = scanner
        .scan_item(&set.rid, &item.rid)
        .await
        .expect("scan should succeed against the scripted runtime");
    assert!(
        report.has_findings(),
        "the report must surface PII so the UI lights up the badge"
    );
    let tags: Vec<&'static str> = report
        .distinct_tags()
        .into_iter()
        .map(|tag| tag.as_str())
        .collect();
    assert_eq!(
        tags,
        vec!["GOVERNMENT_ID", "EMAIL"],
        "scripted findings must round-trip via distinct_tags()"
    );
    let pages: Vec<u32> = report.findings.iter().filter_map(|f| f.page).collect();
    assert_eq!(pages, vec![0, 1], "page indices must travel through unchanged");

    // ── 4. Negative path: a not-yet-scanned item surfaces NotFound
    //       so the dispatcher can defer/retry rather than synthesise
    //       a bogus "clean" report. ─────────────────────────────────
    let err = scanner
        .scan_item(&set.rid, "ri.foundry.main.media_item.never-scanned")
        .await
        .expect_err("missing items must error rather than report clean");
    assert_eq!(
        err,
        ScanError::NotFound("ri.foundry.main.media_item.never-scanned".into())
    );
}
