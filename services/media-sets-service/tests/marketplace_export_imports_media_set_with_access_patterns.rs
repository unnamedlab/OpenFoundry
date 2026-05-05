//! H7 — Marketplace export round-trips a media set descriptor + its
//! registered access patterns + markings + sync flag.
//!
//! Pins the Foundry "Build a product / Add packaged resources / Media
//! set" doc contract:
//!
//!   * `MarketplaceArtifact::MediaSet` is a first-class artifact kind
//!     alongside the existing `ActionType` variant.
//!   * The packaged shape carries (a) the media-set descriptor, (b)
//!     access-pattern rows, (c) a `sync` flag, (d) the source markings
//!     so the importer re-applies them after the rid remap.
//!   * `PackageType::MediaSet` round-trips through its as_str / FromStr
//!     pair so the importer can switch on the kind.
//!
//! The full importer (rid remapping, access-pattern re-registration,
//! marking copy) lands in the marketplace-service. This test pins the
//! wire-form contract so a refactor of the artifact enum cannot
//! silently break the importer.

mod common;

use marketplace_service::models::package::{MarketplaceArtifact, PackageType};
use media_sets_service::handlers::access_patterns::register_access_pattern_op;
use media_sets_service::models::{
    PersistencePolicy, RegisterAccessPatternBody, TransactionPolicy,
};
use std::str::FromStr;

#[tokio::test]
async fn marketplace_export_round_trips_media_set_artifact_with_access_patterns_and_markings() {
    let h = common::spawn().await;

    // ── 1. Source workspace — a media set with one access pattern
    //       and a marking attached. ──────────────────────────────────
    let set = common::seed_media_set(
        &h.state,
        "exportable",
        "ri.foundry.main.project.export",
        TransactionPolicy::Transactionless,
    )
    .await;
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
    .expect("register thumbnail pattern");

    // ── 2. Build the Marketplace artifact the curator would ship.
    //       The shape lives in `marketplace-service::models::package`. ─
    let artifact = MarketplaceArtifact::MediaSet {
        media_set: serde_json::json!({
            "rid": set.rid,
            "name": set.name,
            "schema": set.schema,
            "transaction_policy": set.transaction_policy,
            "allowed_mime_types": set.allowed_mime_types,
        }),
        access_patterns: vec![serde_json::json!({
            "id": pattern.id,
            "kind": pattern.kind,
            "params": pattern.params,
            "persistence": pattern.persistence,
            "ttl_seconds": pattern.ttl_seconds,
        })],
        sync: true,
        markings: vec!["confidential".into()],
    };

    // ── 3. Round-trip via JSON — the on-disk bundle is a JSON file. ─
    let json = serde_json::to_string(&artifact).expect("serialise media-set artifact");
    let parsed: MarketplaceArtifact =
        serde_json::from_str(&json).expect("re-parse via the same enum");

    let MarketplaceArtifact::MediaSet {
        media_set,
        access_patterns,
        sync,
        markings,
    } = parsed
    else {
        panic!("expected MediaSet variant, got {parsed:?}");
    };
    assert_eq!(media_set["rid"].as_str(), Some(set.rid.as_str()));
    assert_eq!(media_set["schema"].as_str(), Some(set.schema.as_str()));
    assert_eq!(access_patterns.len(), 1);
    assert_eq!(
        access_patterns[0]["kind"].as_str(),
        Some("thumbnail"),
        "the importer reads `kind` to pick the runtime handler"
    );
    assert!(sync, "sync flag must travel through unchanged");
    assert_eq!(markings, vec!["confidential".to_string()]);

    // ── 4. PackageType wire-form contract. The importer switches on
    //       `PackageType::from_str(packageType)`; if the round-trip
    //       slug drifts, every published bundle on the marketplace
    //       silently goes unrouted. ─────────────────────────────────
    assert_eq!(PackageType::MediaSet.as_str(), "media_set");
    assert_eq!(
        PackageType::from_str("media_set").expect("media_set parses"),
        PackageType::MediaSet,
    );
}
