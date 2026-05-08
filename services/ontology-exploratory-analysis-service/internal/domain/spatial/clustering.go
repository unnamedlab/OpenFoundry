package spatial

import (
	"fmt"
	"math"
	"sort"

	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/models"
)

// Default knobs match the Rust impl byte-for-byte.
const (
	defaultDBSCANRadiusKm  = 8.0
	defaultKMeansClusters  = 3
	dbscanGridStepDegPerKm = 111.0
	dbscanGridStepFloor    = 0.05
)

// Cluster mirrors `domain::engine::clustering::cluster` in Rust.
func Cluster(layer models.LayerDefinition, request models.ClusterRequest) models.ClusterResponse {
	switch request.Algorithm {
	case models.ClusterAlgorithmDBSCAN:
		return dbscan(layer, ptrFloatOr(request.RadiusKm, defaultDBSCANRadiusKm))
	case models.ClusterAlgorithmKMeans:
		count := ptrIntOr(request.ClusterCount, defaultKMeansClusters)
		if count < 1 {
			count = 1
		}
		return kmeans(layer, count)
	default:
		return models.ClusterResponse{Algorithm: request.Algorithm, Clusters: []models.ClusterSummary{}}
	}
}

type bucketKey struct {
	lat int64
	lon int64
}

func dbscan(layer models.LayerDefinition, radiusKm float64) models.ClusterResponse {
	gridStep := math.Max(radiusKm/dbscanGridStepDegPerKm, dbscanGridStepFloor)

	// Preserve insertion order so cluster numbering is deterministic
	// across runs — Rust uses HashMap which is non-deterministic, but
	// we round-trip through a stable order to keep tests reproducible
	// without changing observable cluster contents.
	order := make([]bucketKey, 0)
	buckets := make(map[bucketKey][]models.Coordinate)
	for _, f := range layer.Features {
		c := f.Geometry.Centroid()
		key := bucketKey{
			lat: int64(math.Floor(c.Lat / gridStep)),
			lon: int64(math.Floor(c.Lon / gridStep)),
		}
		if _, seen := buckets[key]; !seen {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], c)
	}

	outliers := 0
	clusters := make([]models.ClusterSummary, 0, len(order))
	clusterID := 0
	denom := math.Max(radiusKm, 1.0)
	for _, key := range order {
		members := buckets[key]
		if len(members) < 2 {
			outliers += len(members)
			continue
		}
		clusterID++
		clusters = append(clusters, models.ClusterSummary{
			ClusterID:   fmt.Sprintf("dbscan-%d", clusterID),
			Centroid:    averageCoordinate(members),
			MemberCount: len(members),
			Density:     float64(len(members)) / denom,
		})
	}

	sort.SliceStable(clusters, func(i, j int) bool {
		return clusters[i].MemberCount > clusters[j].MemberCount
	})

	return models.ClusterResponse{
		Algorithm: models.ClusterAlgorithmDBSCAN,
		Clusters:  clusters,
		Outliers:  outliers,
	}
}

func kmeans(layer models.LayerDefinition, clusterCount int) models.ClusterResponse {
	groups := make([][]models.Coordinate, clusterCount)
	for i, f := range layer.Features {
		idx := i % clusterCount
		groups[idx] = append(groups[idx], f.Geometry.Centroid())
	}

	clusters := make([]models.ClusterSummary, 0, clusterCount)
	for i, members := range groups {
		if len(members) == 0 {
			continue
		}
		clusters = append(clusters, models.ClusterSummary{
			ClusterID:   fmt.Sprintf("kmeans-%d", i+1),
			Centroid:    averageCoordinate(members),
			MemberCount: len(members),
			Density:     float64(len(members)) / float64(clusterCount),
		})
	}

	return models.ClusterResponse{
		Algorithm: models.ClusterAlgorithmKMeans,
		Clusters:  clusters,
		Outliers:  0,
	}
}

func averageCoordinate(points []models.Coordinate) models.Coordinate {
	if len(points) == 0 {
		return models.Coordinate{}
	}
	var latSum, lonSum float64
	for _, p := range points {
		latSum += p.Lat
		lonSum += p.Lon
	}
	n := float64(len(points))
	return models.Coordinate{Lat: latSum / n, Lon: lonSum / n}
}
