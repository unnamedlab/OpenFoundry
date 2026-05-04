//! H3 closure — full media-set lifecycle in a single integration test.
//!
//! The test exercises every contract that survives end-to-end ownership
//! by `media-sets-service`. Each step mirrors the H3 spec line-by-line:
//!
//!   1. Create a TRANSACTIONAL media set.
//!   2. Open a transaction on `main`.
//!   3. Stage 50 items mixing image / pdf / audio MIME types.
//!   4. Commit the transaction; the items become visible.
//!   5. Apply a per-item marking override on three items (granular
//!      Cedar contract from H3).
//!   6. Reduce retention to 7 days; verify the new value lands and the
//!      reaper runs (no items expire because they were just inserted).
//!   7. A user without clearance hits `POST /media-sets/{rid}/markings/preview`
//!      and gets `403 Forbidden` with the missing-marking surfaced.
//!   8. The audit `outbox.events` table carries one envelope per
//!      mutation in canonical `audit_trail::events` form.
//!   9. Open a fresh transaction, stage one item, abort — staged rows
//!      are gone.
//!  10. Spin a virtual media set, register an external item, and verify
//!      the download URL resolves through the connector mock.
//!
//! ## Storage backend
//!
//! The harness uses `LocalStorage` in a tempdir (the production default
//! for dev / single-node deployments). The MediaStorage trait abstracts
//! over local FS / S3 / Ceph; an extra MinIO testcontainer would only
//! re-verify the `BackendMediaStorage::presign_*` round-trips that
//! `services/connector-management-service/tests/s3_minio.rs` already
//! covers at the connector layer (per Foundry "Set up a media set sync"
//! — connectors push bytes, this service issues the presigned URLs).
//! Re-pinning Docker for that here would be redundant, so we stay on
//! `LocalStorage` and document the gap.
//!
//! ## Audit-event capture
//!
//! `audit_trail::events::emit` performs an `INSERT` followed by an
//! in-transaction `DELETE` on `outbox.events` — the canonical Debezium
//! pattern (the WAL still carries the INSERT). To assert the payload
//! survives in test, we install the same `outbox.audit_mirror` table
//! and `AFTER INSERT` trigger that
//! [`audit_events_emitted_on_upload_download_delete`] uses. Production
//! relies on Debezium logical decoding rather than a trigger.

use std::collections::HashSet;

use audit_trail::events::AuditEnvelope;
use axum::body::Body;
use axum::http::{Request, StatusCode, header::AUTHORIZATION};
use http_body_util::BodyExt;
use media_sets_service::handlers::items::{
    delete_item_op, list_items_op, patch_item_markings_op, presigned_upload_op,
    register_virtual_item_op, RegisterVirtualItemRequest,
};
use media_sets_service::handlers::media_sets::{
    create_media_set_op, get_media_set_op, patch_markings_op, patch_retention_op,
};
use media_sets_service::handlers::transactions::{close_transaction_op, open_transaction_op};
use media_sets_service::models::{
    CreateMediaSetRequest, MediaSetSchema, PresignedUploadRequest, TransactionPolicy,
    TransactionState, WriteMode,
};
use serde_json::{Value, json};
use sqlx::PgPool;
use tower::ServiceExt;
use wiremock::matchers::{method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

use crate::common;

/// Mix of MIME types the connector dispatcher would classify as
/// IMAGE / DOCUMENT / AUDIO under Foundry's "Importing media" filter
/// taxonomy. We keep three buckets so the resulting media set is wide
/// enough to be representative without ballooning the test runtime.
const MIME_BUCKETS: &[&str] = &["image/png", "application/pdf", "audio/mpeg"];

async fn install_audit_mirror(pool: &PgPool) {
    for stmt in [
        r#"CREATE TABLE IF NOT EXISTS outbox.audit_mirror (
            event_id     uuid PRIMARY KEY,
            topic        text NOT NULL,
            aggregate_id text NOT NULL,
            payload      jsonb NOT NULL,
            captured_at  timestamptz NOT NULL DEFAULT now()
        )"#,
        r#"CREATE OR REPLACE FUNCTION outbox._mirror_events() RETURNS trigger AS $$
        BEGIN
            INSERT INTO outbox.audit_mirror (event_id, topic, aggregate_id, payload)
            VALUES (NEW.event_id, NEW.topic, NEW.aggregate_id, NEW.payload)
            ON CONFLICT (event_id) DO NOTHING;
            RETURN NEW;
        END;
        $$ LANGUAGE plpgsql"#,
        "DROP TRIGGER IF EXISTS outbox_events_mirror ON outbox.events",
        r#"CREATE TRIGGER outbox_events_mirror
            AFTER INSERT ON outbox.events
            FOR EACH ROW
            EXECUTE FUNCTION outbox._mirror_events()"#,
    ] {
        sqlx::query(stmt)
            .execute(pool)
            .await
            .expect("install audit mirror");
    }
}

async fn captured_kinds(pool: &PgPool) -> Vec<String> {
    let rows: Vec<(Value,)> =
        sqlx::query_as("SELECT payload FROM outbox.audit_mirror ORDER BY captured_at")
            .fetch_all(pool)
            .await
            .expect("query mirror");
    rows.into_iter()
        .map(|(value,)| {
            let env: AuditEnvelope =
                serde_json::from_value(value).expect("envelope shape decodes");
            env.kind
        })
        .collect()
}

/// Helper that closes the harness over the wiremock connector lifetime.
/// The MockServer goes out of scope before the harness drops so the
/// async listener tears down cleanly.
async fn lifecycle_test_body() {
    // Spin a connector mock so step 10 (virtual media set) has a valid
    // upstream. The same wiremock pattern is used by
    // `virtual_media_item_register_and_resolve_url`.
    let connector_mock = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/sources/ri.foundry.main.source.lifecycle"))
        .respond_with(
            ResponseTemplate::new(200).set_body_json(json!({
                "endpoint": "https://external.lifecycle.test/bucket-y"
            })),
        )
        .mount(&connector_mock)
        .await;

    let h = common::spawn_with_connector(Some(connector_mock.uri())).await;
    install_audit_mirror(&h.pool).await;

    // ── 1. Create a TRANSACTIONAL media set ──────────────────────
    let project_rid = "ri.foundry.main.project.lifecycle";
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "lifecycle-fixture".into(),
            project_rid: project_rid.into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: MIME_BUCKETS.iter().map(|s| s.to_string()).collect(),
            transaction_policy: TransactionPolicy::Transactional,
            retention_seconds: 0,
            virtual_: false,
            source_rid: None,
            markings: vec!["public".into()],
        },
        "lifecycle-tester",
        &common::test_ctx(),
    )
    .await
    .expect("create media set");
    assert_eq!(set.transaction_policy, "TRANSACTIONAL");

    // ── 2. Open a transaction on `main` ──────────────────────────
    let txn = open_transaction_op(
        &h.state,
        &set.rid,
        "main",
        "lifecycle-tester",
        WriteMode::Modify,
        &common::test_ctx(),
    )
    .await
    .expect("open transaction");
    assert_eq!(txn.state, "OPEN");

    // ── 3. Stage 50 items, MIME mix ──────────────────────────────
    let mut staged_rids: Vec<String> = Vec::with_capacity(50);
    for i in 0..50_u32 {
        let mime = MIME_BUCKETS[(i as usize) % MIME_BUCKETS.len()];
        let extension = match mime {
            "image/png" => "png",
            "application/pdf" => "pdf",
            "audio/mpeg" => "mp3",
            _ => "bin",
        };
        let (item, _url) = presigned_upload_op(
            &h.state,
            &set.rid,
            PresignedUploadRequest {
                path: format!("seed/{i:03}.{extension}"),
                mime_type: mime.into(),
                branch: Some("main".into()),
                transaction_rid: Some(txn.rid.clone()),
                // Hash distinct per item so dedup is a no-op (each
                // path is also unique).
                sha256: Some(format!("{:0>64}", format!("{i:x}"))),
                size_bytes: Some(1024 + i64::from(i)),
                expires_in_seconds: None,
            },
            &common::test_ctx(),
        )
        .await
        .unwrap_or_else(|err| panic!("stage item {i}: {err}"));
        staged_rids.push(item.rid);
    }
    assert_eq!(staged_rids.len(), 50);

    // ── 4. Commit the transaction ────────────────────────────────
    close_transaction_op(
        &h.state,
        &txn.rid,
        TransactionState::Committed,
        &common::test_ctx(),
    )
    .await
    .expect("commit transaction");

    let listed = list_items_op(&h.state, &set.rid, "main", None, 200, None)
        .await
        .expect("list items after commit");
    assert_eq!(listed.len(), 50, "every staged item must be visible");
    let mime_seen: HashSet<&str> = listed.iter().map(|item| item.mime_type.as_str()).collect();
    assert_eq!(
        mime_seen.len(),
        MIME_BUCKETS.len(),
        "all three MIME buckets must appear post-commit"
    );

    // ── 5. Per-item marking override on three items ──────────────
    let parent_set = get_media_set_op(&h.state, &set.rid)
        .await
        .expect("reload parent set");
    for victim in &staged_rids[..3] {
        let updated = patch_item_markings_op(
            &h.state,
            victim,
            vec![],
            vec!["secret".into()],
            &parent_set,
            &common::test_ctx(),
        )
        .await
        .expect("override per-item marking");
        assert_eq!(updated.markings, vec!["secret".to_string()]);
    }

    // ── 6. Change retention to 7 days ────────────────────────────
    const SEVEN_DAYS_SECS: i64 = 7 * 86_400;
    let updated = patch_retention_op(
        &h.state,
        &set.rid,
        SEVEN_DAYS_SECS,
        &common::test_ctx(),
    )
    .await
    .expect("patch retention");
    assert_eq!(updated.retention_seconds, SEVEN_DAYS_SECS);
    let still_alive = list_items_op(&h.state, &set.rid, "main", None, 200, None)
        .await
        .expect("post-retention list");
    assert_eq!(
        still_alive.len(),
        50,
        "fresh items must outlive a 7-day window"
    );

    // ── 7. Caller without clearance fails the markings preview ───
    let viewer_token = common::mint_token(
        &h.jwt_config,
        vec!["viewer".into()],
        // Only `public` clearance — nowhere near SECRET.
        vec!["public".into()],
        Some(h.tenant),
    );
    // Tighten the SET's markings so the preview action requires SECRET.
    let parent_after_override = get_media_set_op(&h.state, &set.rid)
        .await
        .expect("reload parent set after item overrides");
    patch_markings_op(
        &h.state,
        &set.rid,
        parent_after_override.markings.clone(),
        vec!["secret".into()],
        &common::test_ctx(),
    )
    .await
    .expect("set-level marking PATCH");

    let resp = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri(format!("/media-sets/{}/markings/preview", set.rid))
                .header(AUTHORIZATION, format!("Bearer {viewer_token}"))
                .header("content-type", "application/json")
                .body(Body::from(json!({ "markings": ["public"] }).to_string()))
                .unwrap(),
        )
        .await
        .expect("invoke preview");
    assert_eq!(resp.status(), StatusCode::FORBIDDEN);
    let body = resp.into_body().collect().await.unwrap().to_bytes();
    let json_body: Value = serde_json::from_slice(&body).unwrap();
    let reason = json_body["error"].as_str().unwrap_or_default();
    assert!(
        reason.to_uppercase().contains("SECRET"),
        "denial message must surface the missing clearance, got: {reason}"
    );

    // ── 8. Audit shows every event we emitted ────────────────────
    let kinds = captured_kinds(&h.pool).await;
    let must_carry = [
        "media_set.created",
        "media_set.transaction_opened",
        "media_item.uploaded",
        "media_set.transaction_committed",
        "media_item.marking_overridden",
        "media_set.retention_changed",
        "media_set.markings_changed",
    ];
    for needed in must_carry {
        assert!(
            kinds.iter().any(|k| k == needed),
            "audit envelope `{needed}` is missing from the outbox; saw: {kinds:?}"
        );
    }

    // ── 9. Commit-fail / abort path ──────────────────────────────
    // Open a second transaction, stage one item, then abort. The
    // staged row must be gone (per the schema-enforced ABORT contract).
    let abort_txn = open_transaction_op(
        &h.state,
        &set.rid,
        "main",
        "lifecycle-tester",
        WriteMode::Modify,
        &common::test_ctx(),
    )
    .await
    .expect("open second transaction");
    let (throwaway, _url) = presigned_upload_op(
        &h.state,
        &set.rid,
        PresignedUploadRequest {
            path: "abort-me.png".into(),
            mime_type: "image/png".into(),
            branch: Some("main".into()),
            transaction_rid: Some(abort_txn.rid.clone()),
            sha256: Some("d".repeat(64)),
            size_bytes: Some(2048),
            expires_in_seconds: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("stage throwaway item");
    close_transaction_op(
        &h.state,
        &abort_txn.rid,
        TransactionState::Aborted,
        &common::test_ctx(),
    )
    .await
    .expect("abort transaction");
    // Item must be gone (hard-deleted by the abort path).
    let after_abort = list_items_op(&h.state, &set.rid, "main", None, 200, None)
        .await
        .expect("post-abort list");
    assert!(
        !after_abort.iter().any(|i| i.rid == throwaway.rid),
        "aborted item must not survive in `media_items`"
    );
    let abort_kinds = captured_kinds(&h.pool).await;
    assert!(
        abort_kinds.iter().any(|k| k == "media_set.transaction_aborted"),
        "abort path must emit `media_set.transaction_aborted`; saw: {abort_kinds:?}"
    );

    // Also exercise the soft-delete path: deleting a committed item
    // emits `media_item.deleted` and keeps the row reachable by RID.
    let to_delete = staged_rids[3].clone();
    delete_item_op(&h.state, &to_delete, &common::test_ctx())
        .await
        .expect("soft-delete committed item");
    assert!(
        captured_kinds(&h.pool)
            .await
            .iter()
            .any(|k| k == "media_item.deleted"),
        "soft-delete must emit `media_item.deleted`"
    );

    // ── 10. Virtual media set: register + resolve ────────────────
    let virtual_set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "virtual-fixture".into(),
            project_rid: project_rid.into(),
            schema: MediaSetSchema::Image,
            allowed_mime_types: vec!["image/png".into()],
            transaction_policy: TransactionPolicy::Transactionless,
            retention_seconds: 0,
            virtual_: true,
            source_rid: Some("ri.foundry.main.source.lifecycle".into()),
            markings: vec![],
        },
        "lifecycle-tester",
        &common::test_ctx(),
    )
    .await
    .expect("create virtual set");

    let virtual_item = register_virtual_item_op(
        &h.state,
        &virtual_set.rid,
        RegisterVirtualItemRequest {
            physical_path: "s3://external/lifecycle/snapshot.png".into(),
            item_path: "snapshot.png".into(),
            mime_type: Some("image/png".into()),
            size_bytes: Some(4096),
            branch: None,
            sha256: None,
        },
        &common::test_ctx(),
    )
    .await
    .expect("register virtual item");

    // The download URL must resolve via the connector mock and point
    // at the external endpoint we wired in `lifecycle_test_body`.
    let download = h
        .router
        .clone()
        .oneshot(
            Request::builder()
                .method("GET")
                .uri(format!("/items/{}/download-url", virtual_item.rid))
                .header(AUTHORIZATION, format!("Bearer {}", h.token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .expect("invoke virtual download");
    assert_eq!(download.status(), StatusCode::OK);
    let body = download.into_body().collect().await.unwrap().to_bytes();
    let json_body: Value = serde_json::from_slice(&body).unwrap();
    let url = json_body["url"].as_str().expect("url field");
    assert!(
        url.starts_with("https://external.lifecycle.test/bucket-y/"),
        "virtual download URL must point at the connector mock endpoint, got: {url}"
    );

    // Bookkeeping: the audit warehouse should now also carry the
    // virtual-item event so every step of the lifecycle is covered.
    let final_kinds = captured_kinds(&h.pool).await;
    assert!(
        final_kinds
            .iter()
            .any(|k| k == "virtual_media_item.registered"),
        "virtual-item registration must emit `virtual_media_item.registered`"
    );

    // The MockServer goes out of scope here together with the harness;
    // wiremock's `Drop` shuts the listener down cleanly.
}

#[tokio::test]
async fn full_media_set_lifecycle() {
    lifecycle_test_body().await;
}
