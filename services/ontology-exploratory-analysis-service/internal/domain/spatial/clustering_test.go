package spatial

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/models"
)

func TestClusterDBSCANGroupsByGrid(t *testing.T) {
	t.Parallel()
	// gridStep = max(8/111, 0.05) ≈ 0.072 — three points within
	// the same 0.072° cell collapse into one cluster.
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 40.00, -3.00),
		pointFeature("b", 40.01, -3.01),
		pointFeature("c", 40.02, -3.02),
		pointFeature("solo", 50.00, 50.00), // far away ⇒ outlier
	}, true)

	resp := Cluster(layer, models.ClusterRequest{
		Algorithm: models.ClusterAlgorithmDBSCAN,
	})

	require.Len(t, resp.Clusters, 1)
	cluster := resp.Clusters[0]
	assert.Equal(t, "dbscan-1", cluster.ClusterID)
	assert.Equal(t, 3, cluster.MemberCount)
	// density = members / max(radius_km, 1.0); default radius 8 ⇒ 3/8.
	assert.InDelta(t, 3.0/8.0, cluster.Density, 1e-9)
	assert.Equal(t, 1, resp.Outliers)
	assert.Equal(t, models.ClusterAlgorithmDBSCAN, resp.Algorithm)
}

func TestClusterDBSCANSortsByMemberCountDescending(t *testing.T) {
	t.Parallel()
	radius := 8.0
	layer := makeLayerWithFeatures([]models.MapFeature{
		// Cluster A (3 members)
		pointFeature("a1", 0.00, 0.00),
		pointFeature("a2", 0.01, 0.01),
		pointFeature("a3", 0.02, 0.02),
		// Cluster B (2 members)
		pointFeature("b1", 10.00, 10.00),
		pointFeature("b2", 10.01, 10.01),
	}, true)

	resp := Cluster(layer, models.ClusterRequest{
		Algorithm: models.ClusterAlgorithmDBSCAN,
		RadiusKm:  &radius,
	})

	require.Len(t, resp.Clusters, 2)
	assert.Equal(t, 3, resp.Clusters[0].MemberCount)
	assert.Equal(t, 2, resp.Clusters[1].MemberCount)
}

func TestClusterDBSCANRespectsCustomRadius(t *testing.T) {
	t.Parallel()
	// radius 1km ⇒ gridStep clamped to floor of 0.05° (≈5.5 km).
	// Both points must fall in the same lat *and* lon bucket — pick
	// values inside (40.0, 40.05) × (-3.0, -2.95) so floor(coord/0.05)
	// is identical for both.
	radius := 1.0
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 40.001, -2.999),
		pointFeature("b", 40.002, -2.998),
	}, true)
	resp := Cluster(layer, models.ClusterRequest{
		Algorithm: models.ClusterAlgorithmDBSCAN,
		RadiusKm:  &radius,
	})
	require.Len(t, resp.Clusters, 1)
	// density = 2 / max(1, 1) = 2.0
	assert.InDelta(t, 2.0, resp.Clusters[0].Density, 1e-9)
}

func TestClusterKMeansRoundRobinAssignment(t *testing.T) {
	t.Parallel()
	count := 3
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 0, 0), // group 0
		pointFeature("b", 1, 1), // group 1
		pointFeature("c", 2, 2), // group 2
		pointFeature("d", 3, 3), // group 0
		pointFeature("e", 4, 4), // group 1
	}, true)

	resp := Cluster(layer, models.ClusterRequest{
		Algorithm:    models.ClusterAlgorithmKMeans,
		ClusterCount: &count,
	})

	require.Len(t, resp.Clusters, 3)
	assert.Equal(t, "kmeans-1", resp.Clusters[0].ClusterID)
	assert.Equal(t, 2, resp.Clusters[0].MemberCount) // a + d
	assert.Equal(t, 2, resp.Clusters[1].MemberCount) // b + e
	assert.Equal(t, 1, resp.Clusters[2].MemberCount) // c
	for _, c := range resp.Clusters {
		// density = members / cluster_count
		assert.InDelta(t, float64(c.MemberCount)/3.0, c.Density, 1e-9)
	}
	assert.Equal(t, 0, resp.Outliers)
}

func TestClusterKMeansSkipsEmptyGroups(t *testing.T) {
	t.Parallel()
	count := 5 // more clusters than features ⇒ 3 empty
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 0, 0), pointFeature("b", 1, 1),
	}, true)
	resp := Cluster(layer, models.ClusterRequest{
		Algorithm:    models.ClusterAlgorithmKMeans,
		ClusterCount: &count,
	})
	assert.Len(t, resp.Clusters, 2)
}

func TestClusterKMeansClampsToOne(t *testing.T) {
	t.Parallel()
	zero := 0
	layer := makeLayerWithFeatures([]models.MapFeature{
		pointFeature("a", 0, 0), pointFeature("b", 1, 1),
	}, true)
	// Rust calls .max(1) — count of 0 is clamped to 1.
	resp := Cluster(layer, models.ClusterRequest{
		Algorithm:    models.ClusterAlgorithmKMeans,
		ClusterCount: &zero,
	})
	require.Len(t, resp.Clusters, 1)
	assert.Equal(t, 2, resp.Clusters[0].MemberCount)
}

func TestAverageCoordinate(t *testing.T) {
	t.Parallel()
	got := averageCoordinate([]models.Coordinate{
		{Lat: 0, Lon: 0}, {Lat: 2, Lon: 4}, {Lat: 4, Lon: 8},
	})
	assert.Equal(t, models.Coordinate{Lat: 2, Lon: 4}, got)
	assert.Equal(t, models.Coordinate{}, averageCoordinate(nil))
}
