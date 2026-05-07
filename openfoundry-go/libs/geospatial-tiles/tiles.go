// Package geospatialtiles is the tile-key + URL helper for geo-anchored
// media-set layers. Mirrors libs/geospatial-tiles/src/lib.rs verbatim —
// same TileCoord shape, same /tiles/{rid}/{z}/{x}/{y}.png path
// template, same TileSourceDescriptor descriptor.
//
// The wire-form URL shape is owned here so media-sets-service,
// geospatial-intelligence-service, the front-end Map raster source and
// apps/web Vertex map layer all agree on a single string.
//
// Pure-Go, no external deps — any service can pull this in without
// inflating its build graph.
package geospatialtiles

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxZ is the maximum zoom we accept. MapLibre/Leaflet support up to
// 24 in theory; Foundry's geo-tile pyramid caps at 22 in practice.
const MaxZ uint8 = 24

// TileCoord is a Web-Mercator XYZ tile coordinate. Matches the
// convention MapLibre, OpenLayers and Leaflet use for raster overlays.
type TileCoord struct {
	Z uint8  `json:"z"`
	X uint32 `json:"x"`
	Y uint32 `json:"y"`
}

// TileError reports a coordinate validation failure. Mirrors the
// thiserror enum on the Rust side.
type TileError struct {
	Kind TileErrorKind
	Z    uint8
	V    uint32
	Max  uint32
}

// TileErrorKind tags the failure case.
type TileErrorKind int

const (
	// ErrZoomOutOfRange — z exceeds MaxZ.
	ErrZoomOutOfRange TileErrorKind = iota
	// ErrXOutOfRange — x exceeds 2^z - 1.
	ErrXOutOfRange
	// ErrYOutOfRange — y exceeds 2^z - 1.
	ErrYOutOfRange
)

func (e *TileError) Error() string {
	switch e.Kind {
	case ErrZoomOutOfRange:
		return fmt.Sprintf("z=%d is out of range (0..=24 supported)", e.Z)
	case ErrXOutOfRange:
		return fmt.Sprintf("x=%d is out of range for zoom %d (max %d)", e.V, e.Z, e.Max)
	case ErrYOutOfRange:
		return fmt.Sprintf("y=%d is out of range for zoom %d (max %d)", e.V, e.Z, e.Max)
	default:
		return "geospatial-tiles: unknown error"
	}
}

// Is supports errors.Is on the kind discriminator. Same shape as
// thiserror's PartialEq derivation.
func (e *TileError) Is(target error) bool {
	var t *TileError
	if !errors.As(target, &t) {
		return false
	}
	return e.Kind == t.Kind && e.Z == t.Z && e.V == t.V && e.Max == t.Max
}

// NewTileCoord validates (z, x, y) against the MaxZ + 2^z-1 bounds
// and returns the coord on success.
func NewTileCoord(z uint8, x, y uint32) (TileCoord, error) {
	if z > MaxZ {
		return TileCoord{}, &TileError{Kind: ErrZoomOutOfRange, Z: z}
	}
	max := MaxIndex(z)
	if x > max {
		return TileCoord{}, &TileError{Kind: ErrXOutOfRange, Z: z, V: x, Max: max}
	}
	if y > max {
		return TileCoord{}, &TileError{Kind: ErrYOutOfRange, Z: z, V: y, Max: max}
	}
	return TileCoord{Z: z, X: x, Y: y}, nil
}

// MaxIndex returns the largest valid x/y index at zoom z. Equals
// 2^z - 1, with a saturating subtract at z=0.
func MaxIndex(z uint8) uint32 {
	if z == 0 {
		return 0
	}
	return (uint32(1) << z) - 1
}

// TileURLPath is the public URL the front-end fetches for a single
// tile of a media item. Mirrors the H7 spec path
// `/tiles/{mediaSetRid}/{z}/{x}/{y}.png`.
//
// The path produced here is STABLE across services because the Vertex
// MapLibre raster source bakes it into its `tiles: [...]` template at
// component-mount time.
func TileURLPath(mediaSetRID string, coord TileCoord) string {
	return fmt.Sprintf("/tiles/%s/%d/%d/%d.png", mediaSetRID, coord.Z, coord.X, coord.Y)
}

// ParseTileURLPath is the inverse helper: parse a tile path back into
// (rid, coord) so the geospatial-intelligence-service route can
// dispatch without re-implementing the parser. Whitespace +
// leading/trailing slashes are tolerated; case is sensitive (RIDs are).
//
// Returns ok=false if the path doesn't match the canonical shape.
func ParseTileURLPath(path string) (rid string, coord TileCoord, ok bool) {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 5 || parts[0] != "tiles" {
		return "", TileCoord{}, false
	}
	rid = parts[1]
	zParsed, err := strconv.ParseUint(parts[2], 10, 8)
	if err != nil {
		return "", TileCoord{}, false
	}
	xParsed, err := strconv.ParseUint(parts[3], 10, 32)
	if err != nil {
		return "", TileCoord{}, false
	}
	yStr, hasSuffix := strings.CutSuffix(parts[4], ".png")
	if !hasSuffix {
		return "", TileCoord{}, false
	}
	yParsed, err := strconv.ParseUint(yStr, 10, 32)
	if err != nil {
		return "", TileCoord{}, false
	}
	coord, err = NewTileCoord(uint8(zParsed), uint32(xParsed), uint32(yParsed))
	if err != nil {
		return "", TileCoord{}, false
	}
	return rid, coord, true
}

// TileSourceDescriptor is the tile-source descriptor the front-end
// reads to mount a MapLibre raster source. Centralised here so the
// descriptor JSON stays stable across apps/web Vertex + Map and the
// geospatial-intelligence-service's /tiles/{rid} introspection
// endpoint.
type TileSourceDescriptor struct {
	MediaSetRID string `json:"media_set_rid"`
	// One template per visible Foundry tile endpoint —
	// {z}/{x}/{y} placeholders mirror MapLibre's expected form.
	TileURLTemplate string `json:"tile_url_template"`
	// Pixel size of each tile. 256 today; 512 reserved for HiDPI.
	TileSize uint32 `json:"tile_size"`
	// Min/max zoom where the layer renders.
	MinZoom uint8 `json:"minzoom"`
	MaxZoom uint8 `json:"maxzoom"`
	// Foundry-doc canonical attribution to surface in the legend.
	Attribution string `json:"attribution"`
}

// NewTileSourceDescriptor produces the canonical descriptor for
// `mediaSetRID`. Defaults pinned to the Rust source: tile size 256,
// minzoom 0, maxzoom 22, plus the verbatim attribution string.
func NewTileSourceDescriptor(mediaSetRID string) TileSourceDescriptor {
	return TileSourceDescriptor{
		MediaSetRID:     mediaSetRID,
		TileURLTemplate: fmt.Sprintf("/tiles/%s/{z}/{x}/{y}.png", mediaSetRID),
		TileSize:        256,
		MinZoom:         0,
		MaxZoom:         22,
		Attribution:     "© OpenFoundry media-sets-service · access pattern: geo_tile",
	}
}
