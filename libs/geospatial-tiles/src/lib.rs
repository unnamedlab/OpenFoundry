//! Tile-key + URL helpers for geo-anchored media-set layers.
//!
//! Foundry's Vertex "Media layers and image annotations" doc and the
//! Map app both consume `(z, x, y)` raster tiles for a media item via
//! the `geo_tile` access pattern (75 cs/GB in
//! `observability::COST_TABLE`). The wire-form URL shape is owned
//! here so `media-sets-service`, `geospatial-intelligence-service`,
//! the front-end Map raster source and `apps/web` Vertex map layer
//! all agree on a single string.
//!
//! The helpers are pure-Rust (only `serde` + `thiserror`) so any
//! service can pull this in without inflating its compile graph.

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Web-Mercator XYZ tile coordinate. Matches the convention MapLibre,
/// OpenLayers and Leaflet use for raster overlays.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct TileCoord {
    pub z: u8,
    pub x: u32,
    pub y: u32,
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum TileError {
    #[error("z={z} is out of range (0..=24 supported)")]
    ZoomOutOfRange { z: u8 },
    #[error("x={x} is out of range for zoom {z} (max {max})")]
    XOutOfRange { z: u8, x: u32, max: u32 },
    #[error("y={y} is out of range for zoom {z} (max {max})")]
    YOutOfRange { z: u8, y: u32, max: u32 },
}

impl TileCoord {
    /// Maximum zoom we accept. MapLibre/Leaflet support up to 24 in
    /// theory; Foundry's geo-tile pyramid caps at 22 in practice.
    pub const MAX_Z: u8 = 24;

    pub fn new(z: u8, x: u32, y: u32) -> Result<Self, TileError> {
        if z > Self::MAX_Z {
            return Err(TileError::ZoomOutOfRange { z });
        }
        let max = Self::max_index(z);
        if x > max {
            return Err(TileError::XOutOfRange { z, x, max });
        }
        if y > max {
            return Err(TileError::YOutOfRange { z, y, max });
        }
        Ok(Self { z, x, y })
    }

    /// Largest valid `x` / `y` index at zoom `z`. Equals `2^z - 1`.
    pub fn max_index(z: u8) -> u32 {
        if z == 0 {
            0
        } else {
            (1u32 << z).saturating_sub(1)
        }
    }
}

/// Public URL the front-end fetches for a single tile of a media
/// item. Mirrors the path the H7 spec calls for:
///   `/tiles/{mediaSetRid}/{z}/{x}/{y}.png`
///
/// We keep the path produced here STABLE across services because the
/// Vertex `<MapLibre>` raster source bakes it into its `tiles: [...]`
/// template at component-mount time.
pub fn tile_url_path(media_set_rid: &str, coord: TileCoord) -> String {
    format!(
        "/tiles/{set}/{z}/{x}/{y}.png",
        set = media_set_rid,
        z = coord.z,
        x = coord.x,
        y = coord.y,
    )
}

/// Inverse helper: parse a tile path back into `(rid, coord)` so the
/// `geospatial-intelligence-service` route can dispatch without
/// re-implementing the parser. Whitespace + leading/trailing slashes
/// are tolerated; case is sensitive (RIDs are).
pub fn parse_tile_url_path(path: &str) -> Option<(String, TileCoord)> {
    let trimmed = path.trim().trim_matches('/');
    let mut parts = trimmed.split('/');
    if parts.next()? != "tiles" {
        return None;
    }
    let rid = parts.next()?.to_string();
    let z: u8 = parts.next()?.parse().ok()?;
    let x: u32 = parts.next()?.parse().ok()?;
    let last = parts.next()?;
    if parts.next().is_some() {
        return None;
    }
    let y: u32 = last.strip_suffix(".png")?.parse().ok()?;
    let coord = TileCoord::new(z, x, y).ok()?;
    Some((rid, coord))
}

/// Tile-source descriptor the front-end reads to mount a MapLibre
/// raster source. Centralised here so the descriptor JSON stays
/// stable across `apps/web` Vertex + Map and the
/// `geospatial-intelligence-service`'s `/tiles/{rid}` introspection
/// endpoint.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct TileSourceDescriptor {
    pub media_set_rid: String,
    /// One template per visible Foundry tile endpoint —
    /// `{z}/{x}/{y}` placeholders mirror MapLibre's expected form.
    pub tile_url_template: String,
    /// Pixel size of each tile. 256 today; 512 reserved for HiDPI.
    pub tile_size: u32,
    /// Min/max zoom where the layer renders.
    pub minzoom: u8,
    pub maxzoom: u8,
    /// Foundry-doc canonical attribution to surface in the legend.
    pub attribution: String,
}

impl TileSourceDescriptor {
    pub fn new(media_set_rid: impl Into<String>) -> Self {
        let rid: String = media_set_rid.into();
        Self {
            tile_url_template: format!("/tiles/{rid}/{{z}}/{{x}}/{{y}}.png"),
            media_set_rid: rid,
            tile_size: 256,
            minzoom: 0,
            maxzoom: 22,
            attribution: "© OpenFoundry media-sets-service · access pattern: geo_tile".into(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn z0_is_one_tile() {
        assert_eq!(TileCoord::max_index(0), 0);
        TileCoord::new(0, 0, 0).unwrap();
        assert!(TileCoord::new(0, 1, 0).is_err());
    }

    #[test]
    fn z3_has_64_tiles() {
        assert_eq!(TileCoord::max_index(3), 7);
        TileCoord::new(3, 7, 7).unwrap();
        assert!(TileCoord::new(3, 8, 0).is_err());
    }

    #[test]
    fn url_round_trips() {
        let coord = TileCoord::new(5, 12, 13).unwrap();
        let path = tile_url_path("ri.foundry.main.media_set.tiles", coord);
        assert_eq!(path, "/tiles/ri.foundry.main.media_set.tiles/5/12/13.png");
        let (rid, parsed) = parse_tile_url_path(&path).unwrap();
        assert_eq!(rid, "ri.foundry.main.media_set.tiles");
        assert_eq!(parsed, coord);
    }

    #[test]
    fn parse_rejects_garbage() {
        assert!(parse_tile_url_path("/foo/bar").is_none());
        assert!(parse_tile_url_path("/tiles/r/1/2/3.jpg").is_none());
        assert!(parse_tile_url_path("/tiles/r/extra/1/2/3.png").is_none());
    }

    #[test]
    fn descriptor_carries_canonical_template() {
        let desc = TileSourceDescriptor::new("ri.foundry.main.media_set.x");
        assert_eq!(
            desc.tile_url_template,
            "/tiles/ri.foundry.main.media_set.x/{z}/{x}/{y}.png"
        );
        assert_eq!(desc.tile_size, 256);
        assert_eq!(desc.maxzoom, 22);
    }
}
