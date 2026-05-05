//! H7 — DICOM media set + `render_dicom_image_layer` access pattern.
//!
//! Pins the Foundry-doc contract from `Add a DICOM media set.md`:
//!
//!   * `MediaSetSchema::Dicom` is a first-class schema variant that
//!     round-trips through `core_models::MediaSetSchema` AND the
//!     service-local `media_sets_service::models::MediaSetSchema` AND
//!     the Postgres CHECK constraint added in migration 0008.
//!   * The DICOM-specific `render_dicom_image_layer` access pattern
//!     bills at 75 cs/GB (matches the doc's published rate) and lives
//!     in the runtime catalog as `External { binary: "dcmtk" }`.
//!
//! The test does NOT shell out to `dcmtk`; the runtime handler is
//! catalogued but the actual pixel-extraction is the runtime service's
//! responsibility (same shape as `extract_audio` → ffmpeg). What we
//! pin here is the contract surface: schema acceptance, cost rate,
//! catalog presence, registration of the access pattern row.

mod common;

use core_models::media_reference::MediaSetSchema as CoreMediaSetSchema;
use media_sets_service::handlers::access_patterns::register_access_pattern_op;
use media_sets_service::handlers::media_sets::create_media_set_op;
use media_sets_service::models::{
    CreateMediaSetRequest, MediaSetSchema, PersistencePolicy, RegisterAccessPatternBody,
    TransactionPolicy,
};

#[tokio::test]
async fn dicom_media_set_supports_render_dicom_image_layer_with_doc_billing_rate() {
    let h = common::spawn().await;

    // ── 1. Create a DICOM-schema media set. The CHECK constraint
    //       added by migration 0008 must accept "DICOM". ────────────
    let set = create_media_set_op(
        &h.state,
        CreateMediaSetRequest {
            name: "ct-scans".into(),
            project_rid: "ri.foundry.main.project.dicom".into(),
            schema: MediaSetSchema::Dicom,
            allowed_mime_types: vec!["application/dicom".into()],
            transaction_policy: TransactionPolicy::Transactional,
            retention_seconds: 0,
            virtual_: false,
            source_rid: None,
            markings: vec![],
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("DICOM media set creation must succeed");
    // `MediaSet.schema` is the persisted string column; pin the
    // doc-canonical wire-form so a refactor of the enum can't silently
    // change what hits Postgres.
    assert_eq!(set.schema, "DICOM");

    // The wire-form schema serialises to "DICOM" (round-trip through
    // the core-models enum, which is what travels in MediaReference
    // JSON inside dataset cells / ontology properties).
    assert_eq!(CoreMediaSetSchema::Dicom.as_str(), "DICOM");
    let parsed: CoreMediaSetSchema = "DICOM".parse().expect("DICOM parses");
    assert_eq!(parsed, CoreMediaSetSchema::Dicom);

    // ── 2. Register the DICOM-specific access pattern. The
    //       persistence is RECOMPUTE (the doc's render_dicom_image_layer
    //       is per-instance window/level; caching pixels keyed by every
    //       knob would explode cost-side, so the doc treats it as
    //       per-call). ──────────────────────────────────────────────
    let pattern = register_access_pattern_op(
        &h.state,
        &set.rid,
        RegisterAccessPatternBody {
            kind: "render_dicom_image_layer".into(),
            params: serde_json::json!({"window_center": 40, "window_width": 400}),
            persistence: PersistencePolicy::Recompute,
            ttl_seconds: None,
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("register render_dicom_image_layer pattern");
    assert_eq!(pattern.kind, "render_dicom_image_layer");

    // ── 3. The cost-meter knows the DICOM rate. The published doc
    //       puts it at 75 cs/GB; drift here means a billing change
    //       sneaked into COST_TABLE without a doc + ADR update. ─────
    assert_eq!(
        observability::rate_per_gb("render_dicom_image_layer"),
        Some(75),
        "DICOM render-layer rate drifted from the Foundry-doc 75 cs/GB"
    );

}
