package geospatial

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/models"
)

func makeLayer(t *testing.T, indexed bool, featureCount int) models.LayerDefinition {
	t.Helper()
	feats := make([]models.MapFeature, featureCount)
	for i := range feats {
		feats[i] = models.MapFeature{
			ID:       "f",
			Label:    "l",
			Geometry: models.Geometry{Type: models.GeometryTypePoint, Point: &models.Coordinate{Lat: float64(i), Lon: 0}},
		}
	}
	return models.LayerDefinition{
		ID:            uuid.New(),
		Name:          "L",
		SourceKind:    models.LayerSourceKindDataset,
		SourceDataset: "ds",
		GeometryType:  models.GeometryTypePoint,
		Style:         models.NewDefaultLayerStyle(),
		Features:      feats,
		Tags:          []string{},
		Indexed:       indexed,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

func TestBuildOverviewEmpty(t *testing.T) {
	t.Parallel()
	o := BuildOverview(nil)
	assert.Equal(t, 0, o.LayerCount)
	assert.Equal(t, 0, o.IndexedLayers)
	assert.Equal(t, 0, o.TotalFeatures)
	assert.Equal(t, 0, o.TileReadyLayers)
	assert.Equal(t, []string{
		"within", "intersects", "nearest", "buffer", "dbscan", "kmeans", "vector_tiles", "routing",
	}, o.SupportedOperations)
}

func TestBuildOverviewCountsTileReady(t *testing.T) {
	t.Parallel()
	layers := []models.LayerDefinition{
		makeLayer(t, true, 3),  // indexed + features → tile-ready
		makeLayer(t, true, 0),  // indexed but empty → NOT tile-ready
		makeLayer(t, false, 5), // unindexed → NOT tile-ready
		makeLayer(t, true, 1),  // indexed + features → tile-ready
	}
	o := BuildOverview(layers)
	assert.Equal(t, 4, o.LayerCount)
	assert.Equal(t, 3, o.IndexedLayers)
	assert.Equal(t, 9, o.TotalFeatures)
	assert.Equal(t, 2, o.TileReadyLayers)
}

func TestSupportedOperationsIsImmutable(t *testing.T) {
	t.Parallel()
	// BuildOverview must hand back a *copy* — clobbering one overview's
	// slice cannot leak into another caller. Mutating the package-level
	// `SupportedOperations` directly is allowed (test isolation), but
	// the value returned by BuildOverview owns its own backing array.
	o1 := BuildOverview(nil)
	o2 := BuildOverview(nil)
	o1.SupportedOperations[0] = "MUTATED"
	assert.Equal(t, "within", o2.SupportedOperations[0])
}

func TestOverviewWireShape(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(BuildOverview([]models.LayerDefinition{makeLayer(t, true, 2)}))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, float64(1), got["layer_count"])
	assert.Equal(t, float64(1), got["indexed_layers"])
	assert.Equal(t, float64(2), got["total_features"])
	assert.Equal(t, float64(1), got["tile_ready_layers"])
	ops, ok := got["supported_operations"].([]any)
	require.True(t, ok)
	require.Len(t, ops, 8)
	assert.Equal(t, "within", ops[0])
	assert.Equal(t, "routing", ops[7])
}
