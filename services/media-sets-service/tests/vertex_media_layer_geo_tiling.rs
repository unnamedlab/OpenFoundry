//! H7 — Vertex "Media layers and image annotations" round-trip.
//!
//! Foundry's Vertex doc lets a curator promote a (geo-anchored) media
//! item into a Map / Vertex layer that ships raster tiles via
//!   `/tiles/{mediaSetRid}/{z}/{x}/{y}.png`
//! against the `geo_tile` access pattern (75 cs/GB).
//!
//! This test pins three things end-to-end:
//!
//!   1. The shared `geospatial-tiles` lib produces the doc-canonical
//!      tile URL path AND parses it back into `(rid, TileCoord)`.
//!   2. The `geo_tile` access pattern can be registered against the
//!      media set (the doc requires this before tiles serve).
//!   3. The cost-meter publishes `geo_tile` at the doc-pinned 75 cs/GB
//!      so Vertex layers and Map layers bill identically.
//!
//! The actual pyramid generation is a runtime-service concern (the
//! `media-transform-runtime-service` catalogs `geo_tile` as
//! NotImplemented today, deferred to the geospatial-intelligence-service
//! follow-up). What we lock here is the contract surface every layer
//! consumer reads.

mod common;

use geospatial_tiles::{TileCoord, TileSourceDescriptor, parse_tile_url_path, tile_url_path};
use media_sets_service::handlers::access_patterns::register_access_pattern_op;
use media_sets_service::models::{
    PersistencePolicy, RegisterAccessPatternBody, TransactionPolicy,
};

#[tokio::test]
async fn vertex_media_layer_round_trips_geo_tile_url_and_registers_access_pattern() {
    let h = common::spawn().await;

    let set = common::seed_media_set(
        &h.state,
        "satellite-imagery",
        "ri.foundry.main.project.geo",
        TransactionPolicy::Transactionless,
    )
    .await;

    // ── 1. Tile URL path round-trip via the shared lib. ────────────
    let coord = TileCoord::new(7, 64, 42).expect("(7,64,42) is in-range");
    let path = tile_url_path(&set.rid, coord);
    assert!(
        path.ends_with("/7/64/42.png"),
        "tile_url_path must emit /tiles/{{rid}}/{{z}}/{{x}}/{{y}}.png — got {path}"
    );
    let (parsed_rid, parsed_coord) =
        parse_tile_url_path(&path).expect("parse_tile_url_path must accept its own output");
    assert_eq!(parsed_rid, set.rid);
    assert_eq!(parsed_coord, coord);

    // The MapLibre-side descriptor carries the same template the
    // front-end <RasterMediaLayer> mounts as a raster source.
    let descriptor = TileSourceDescriptor::new(&set.rid);
    assert_eq!(descriptor.media_set_rid, set.rid);
    assert!(
        descriptor.tile_url_template.contains("{z}/{x}/{y}.png"),
        "descriptor template must keep MapLibre's {{z}}/{{x}}/{{y}} placeholders — got {}",
        descriptor.tile_url_template
    );

    // ── 2. Register the `geo_tile` access pattern. The doc's Vertex
    //       page calls this out as a prerequisite before tiles serve.
    let pattern = register_access_pattern_op(
        &h.state,
        &set.rid,
        RegisterAccessPatternBody {
            kind: "geo_tile".into(),
            params: serde_json::json!({"min_zoom": 0, "max_zoom": 22}),
            // The doc treats geo-tile pyramids as PERSIST: pre-rendered
            // tiles live alongside the source media item and serve
            // statically — refreshing only when the source changes.
            persistence: PersistencePolicy::Persist,
            ttl_seconds: None,
        },
        "tester",
        &common::test_ctx(),
    )
    .await
    .expect("register geo_tile pattern");
    assert_eq!(pattern.kind, "geo_tile");

    // ── 3. Cost rate matches the doc (75 cs/GB). The same row covers
    //       Vertex layers AND Map app raster sources because both go
    //       through the same access pattern key. ───────────────────
    assert_eq!(
        observability::rate_per_gb("geo_tile"),
        Some(75),
        "geo_tile rate drifted from the Foundry-doc 75 cs/GB"
    );
}
