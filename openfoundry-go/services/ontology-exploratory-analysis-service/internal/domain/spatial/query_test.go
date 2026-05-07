package spatial

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/models"
)

func pointFeature(id string, lat, lon float64) models.MapFeature {
	c := models.Coordinate{Lat: lat, Lon: lon}
	return models.MapFeature{ID: id, Label: id, Geometry: models.Geometry{Type: models.GeometryTypePoint, Point: &c}}
}

func makeLayerWithFeatures(features []models.MapFeature, indexed bool) models.LayerDefinition {
	return models.LayerDefinition{
		ID:           uuid.New(),
		Name:         "L",
		SourceKind:   models.LayerSourceKindDataset,
		GeometryType: models.GeometryTypePoint,
		Features:     features,
		Tags:         []string{},
		Indexed:      indexed,
	}
}

func TestExecuteWithinFiltersByBounds(t *testing.T) {
	t.Parallel()
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("inside-1", 40.4, -3.7),
		pointFeature("outside", 50.0, -3.7),
		pointFeature("inside-2", 40.5, -3.6),
	}, true)
	bounds := models.Bounds{MinLat: 40, MinLon: -4, MaxLat: 41, MaxLon: -3}

	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationWithin,
		Bounds:    &bounds,
	})

	require.Len(t, resp.MatchedFeatures, 2)
	assert.Equal(t, "inside-1", resp.MatchedFeatures[0].ID)
	assert.Equal(t, "inside-2", resp.MatchedFeatures[1].ID)
	assert.Equal(t, models.SpatialOperationWithin, resp.Operation)
	assert.Equal(t, 2, resp.Summary.MatchedCount)
	assert.Equal(t, int32(18+2*4), resp.Summary.QueryTimeMs)
	assert.True(t, resp.Summary.Indexed)
	assert.Nil(t, resp.Summary.NearestDistanceKm)
	assert.Empty(t, resp.BufferRing)
}

func TestExecuteWithinNilBoundsReturnsAll(t *testing.T) {
	t.Parallel()
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 1, 1), pointFeature("b", 2, 2),
	}, false)

	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationWithin,
	})

	assert.Len(t, resp.MatchedFeatures, 2)
	assert.False(t, resp.Summary.Indexed)
}

func TestExecuteIntersectsUsesFeatureBounds(t *testing.T) {
	t.Parallel()
	line := models.Geometry{
		Type:       models.GeometryTypeLineString,
		LineString: []models.Coordinate{{Lat: 0, Lon: 0}, {Lat: 5, Lon: 5}},
	}
	layer := makeLayerWithFeatures([]models.MapFeature{
		{ID: "line", Label: "line", Geometry: line},
		pointFeature("far", 100, 100),
	}, true)
	bounds := models.Bounds{MinLat: 4, MinLon: 4, MaxLat: 6, MaxLon: 6}

	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationIntersects,
		Bounds:    &bounds,
	})

	require.Len(t, resp.MatchedFeatures, 1)
	assert.Equal(t, "line", resp.MatchedFeatures[0].ID)
}

func TestExecuteNearestSortsAndReportsDistance(t *testing.T) {
	t.Parallel()
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("far", 41, -3),
		pointFeature("near", 40.01, -3),
		pointFeature("middle", 40.5, -3),
	}, true)
	point := models.Coordinate{Lat: 40, Lon: -3}
	limit := 2

	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationNearest,
		Point:     &point,
		Limit:     &limit,
	})

	require.Len(t, resp.MatchedFeatures, 2)
	assert.Equal(t, "near", resp.MatchedFeatures[0].ID)
	assert.Equal(t, "middle", resp.MatchedFeatures[1].ID)
	require.NotNil(t, resp.Summary.NearestDistanceKm)
	assert.InDelta(t, point.DistanceKm(models.Coordinate{Lat: 40.01, Lon: -3}), *resp.Summary.NearestDistanceKm, 1e-9)
}

func TestExecuteNearestNilPointReturnsLimitedSlice(t *testing.T) {
	t.Parallel()
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 1, 1), pointFeature("b", 2, 2), pointFeature("c", 3, 3),
	}, false)
	limit := 2
	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationNearest,
		Limit:     &limit,
	})
	require.Len(t, resp.MatchedFeatures, 2)
	assert.Equal(t, "a", resp.MatchedFeatures[0].ID)
	assert.Nil(t, resp.Summary.NearestDistanceKm, "no point ⇒ no distance")
}

func TestExecuteBufferReturnsRingAndFiltersByRadius(t *testing.T) {
	t.Parallel()
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("inside", 40.001, -3.0), // ~0.111 km away
		pointFeature("outside", 41.0, -3.0),  // ~111 km away
	}, true)
	point := models.Coordinate{Lat: 40, Lon: -3}
	radius := 5.0

	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationBuffer,
		Point:     &point,
		RadiusKm:  &radius,
	})

	require.Len(t, resp.MatchedFeatures, 1)
	assert.Equal(t, "inside", resp.MatchedFeatures[0].ID)
	require.Len(t, resp.BufferRing, 7, "hexagonal ring closes back to origin")
	assert.Equal(t, resp.BufferRing[0], resp.BufferRing[6], "first and last vertex are equal")
}

func TestExecuteBufferNilPointReturnsEmpty(t *testing.T) {
	t.Parallel()
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 1, 1),
	}, true)
	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationBuffer,
	})
	assert.Empty(t, resp.MatchedFeatures)
	assert.Empty(t, resp.BufferRing)
}

func TestExecuteResultLimitTruncates(t *testing.T) {
	t.Parallel()
	feats := make([]models.MapFeature, 50)
	for i := range feats {
		feats[i] = pointFeature("f", 0.1*float64(i), 0)
	}
	layer := makeLayerWithFeatures(feats, true)

	// Default limit (25) — truncates the 50-feature within result.
	resp := Execute(layer, models.SpatialQueryRequest{
		LayerID:   layer.ID,
		Operation: models.SpatialOperationWithin,
	})
	assert.Len(t, resp.MatchedFeatures, 25)
}

func TestBufferRingGeometry(t *testing.T) {
	t.Parallel()
	ring := bufferRingPoints(models.Coordinate{Lat: 40, Lon: -3}, 11.1)
	require.Len(t, ring, 7)
	// delta = 11.1/111 = 0.1
	assert.InDelta(t, 40.1, ring[0].Lat, 1e-9)
	assert.InDelta(t, -3.0, ring[0].Lon, 1e-9)
	assert.Equal(t, ring[0], ring[6])
}
