// Spatial-feature handlers ported 1:1 from
// services/ontology-exploratory-analysis-service/src/geospatial/geospatial_base/handlers/features.rs
// (S8 / ADR-0030 — geospatial-intelligence-service absorbed). The
// route_features handler is intentionally not ported in this slice —
// see ROADMAP OEA-5+ where routing.rs lands its own engine and HTTP
// surface. Only query_features and cluster_features (the predicate +
// clustering surface for OEA-4) are wired here.
package geospatial

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/domain/spatial"
	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/models"
)

// QueryFeatures mirrors `handlers::features::query_features`.
func (s *AppState) QueryFeatures(w http.ResponseWriter, r *http.Request) {
	var req models.SpatialQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Operation {
	case models.SpatialOperationWithin, models.SpatialOperationIntersects:
		if req.Bounds == nil {
			writeError(w, http.StatusBadRequest, "bounds are required for within/intersects queries")
			return
		}
	case models.SpatialOperationNearest, models.SpatialOperationBuffer:
		if req.Point == nil {
			writeError(w, http.StatusBadRequest, "point is required for nearest/buffer queries")
			return
		}
	}

	layer, ok, err := s.findLayer(r, req.LayerID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "layer not found")
		return
	}

	writeJSON(w, http.StatusOK, spatial.Execute(layer, req))
}

// ClusterFeatures mirrors `handlers::features::cluster_features`.
func (s *AppState) ClusterFeatures(w http.ResponseWriter, r *http.Request) {
	var req models.ClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	layer, ok, err := s.findLayer(r, req.LayerID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "layer not found")
		return
	}

	writeJSON(w, http.StatusOK, spatial.Cluster(layer, req))
}

// findLayer mirrors the Rust helper inlined in features.rs:
// load all layers, then look up by id in-memory. The Rust impl uses
// `load_all_layers` rather than `load_layer_row` to keep the lazy
// indexer hot — preserve that semantics here verbatim.
func (s *AppState) findLayer(r *http.Request, id uuid.UUID) (models.LayerDefinition, bool, error) {
	layers, err := LoadAllLayers(r.Context(), s.DB)
	if err != nil {
		return models.LayerDefinition{}, false, err
	}
	for _, layer := range layers {
		if layer.ID == id {
			return layer, true, nil
		}
	}
	return models.LayerDefinition{}, false, nil
}
