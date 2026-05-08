// Package spatial holds the pure-logic spatial query and clustering
// engines ported 1:1 from
// services/ontology-exploratory-analysis-service/src/geospatial/geospatial_base/domain/engine/
// (S8 / ADR-0030 — geospatial-intelligence-service absorbed). The
// engines are dependency-free so handler tests can drive them against
// synthetic layers without spinning up Postgres.
package spatial

import (
	"sort"

	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/models"
)

// Default knobs match the Rust impl byte-for-byte — clients rely on
// the same numeric outputs across runtimes.
const (
	defaultNearestLimit   = 5
	defaultBufferRadiusKm = 10.0
	defaultResultLimit    = 25
	bufferRingDegPerKm    = 111.0
)

// Execute mirrors `domain::engine::spatial_query::execute` in Rust.
func Execute(layer models.LayerDefinition, request models.SpatialQueryRequest) models.SpatialQueryResponse {
	var features []models.MapFeature
	switch request.Operation {
	case models.SpatialOperationWithin:
		features = withinQuery(layer, request.Bounds)
	case models.SpatialOperationIntersects:
		features = intersectsQuery(layer, request.Bounds)
	case models.SpatialOperationNearest:
		features = nearestQuery(layer, request.Point, ptrIntOr(request.Limit, defaultNearestLimit))
	case models.SpatialOperationBuffer:
		features = bufferQuery(layer, request.Point, ptrFloatOr(request.RadiusKm, defaultBufferRadiusKm))
	}

	var nearestDistanceKm *float64
	if request.Operation == models.SpatialOperationNearest && request.Point != nil && len(features) > 0 {
		d := request.Point.DistanceKm(features[0].Geometry.Centroid())
		nearestDistanceKm = &d
	}

	bufferRing := []models.Coordinate{}
	if request.Operation == models.SpatialOperationBuffer && request.Point != nil {
		bufferRing = bufferRingPoints(*request.Point, ptrFloatOr(request.RadiusKm, defaultBufferRadiusKm))
	}

	limit := ptrIntOr(request.Limit, defaultResultLimit)
	if len(features) > limit {
		features = features[:limit]
	}

	return models.SpatialQueryResponse{
		Operation:       request.Operation,
		MatchedFeatures: features,
		Summary: models.SpatialQuerySummary{
			MatchedCount:      len(features),
			QueryTimeMs:       18 + int32(len(features))*4,
			NearestDistanceKm: nearestDistanceKm,
			Indexed:           layer.Indexed,
		},
		BufferRing: bufferRing,
	}
}

func withinQuery(layer models.LayerDefinition, bounds *models.Bounds) []models.MapFeature {
	if bounds == nil {
		return cloneFeatures(layer.Features)
	}
	out := make([]models.MapFeature, 0, len(layer.Features))
	for _, f := range layer.Features {
		if bounds.Contains(f.Geometry.Centroid()) {
			out = append(out, f)
		}
	}
	return out
}

func intersectsQuery(layer models.LayerDefinition, bounds *models.Bounds) []models.MapFeature {
	if bounds == nil {
		return cloneFeatures(layer.Features)
	}
	out := make([]models.MapFeature, 0, len(layer.Features))
	for _, f := range layer.Features {
		if f.Geometry.BoundsOf().Intersects(*bounds) {
			out = append(out, f)
		}
	}
	return out
}

func nearestQuery(layer models.LayerDefinition, point *models.Coordinate, limit int) []models.MapFeature {
	if point == nil {
		if limit >= len(layer.Features) {
			return cloneFeatures(layer.Features)
		}
		return cloneFeatures(layer.Features[:limit])
	}
	features := cloneFeatures(layer.Features)
	p := *point
	sort.SliceStable(features, func(i, j int) bool {
		return p.DistanceKm(features[i].Geometry.Centroid()) <
			p.DistanceKm(features[j].Geometry.Centroid())
	})
	if limit < len(features) {
		features = features[:limit]
	}
	return features
}

func bufferQuery(layer models.LayerDefinition, point *models.Coordinate, radiusKm float64) []models.MapFeature {
	if point == nil {
		return []models.MapFeature{}
	}
	out := make([]models.MapFeature, 0, len(layer.Features))
	for _, f := range layer.Features {
		if point.DistanceKm(f.Geometry.Centroid()) <= radiusKm {
			out = append(out, f)
		}
	}
	return out
}

// bufferRingPoints reproduces the 7-vertex hexagonal ring the Rust
// engine returns — final vertex repeats the first to close the ring.
func bufferRingPoints(center models.Coordinate, radiusKm float64) []models.Coordinate {
	delta := radiusKm / bufferRingDegPerKm
	return []models.Coordinate{
		{Lat: center.Lat + delta, Lon: center.Lon},
		{Lat: center.Lat + delta/2.0, Lon: center.Lon + delta},
		{Lat: center.Lat - delta/2.0, Lon: center.Lon + delta},
		{Lat: center.Lat - delta, Lon: center.Lon},
		{Lat: center.Lat - delta/2.0, Lon: center.Lon - delta},
		{Lat: center.Lat + delta/2.0, Lon: center.Lon - delta},
		{Lat: center.Lat + delta, Lon: center.Lon},
	}
}

func cloneFeatures(in []models.MapFeature) []models.MapFeature {
	out := make([]models.MapFeature, len(in))
	copy(out, in)
	return out
}

func ptrIntOr(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}

func ptrFloatOr(p *float64, fallback float64) float64 {
	if p == nil {
		return fallback
	}
	return *p
}
