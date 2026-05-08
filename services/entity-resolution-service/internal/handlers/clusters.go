package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// ListClusters mirrors `handlers::clusters::list_clusters`.
func (h *Handlers) ListClusters(w http.ResponseWriter, r *http.Request) {
	clusters, err := h.Clusters.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ResolvedCluster]{Data: clusters})
}

// GetCluster mirrors `handlers::clusters::get_cluster`.
func (h *Handlers) GetCluster(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	cluster, err := h.Clusters.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	review, err := h.Review.LatestForCluster(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	golden, err := h.Golden.LatestForCluster(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ClusterDetail{
		Cluster:      *cluster,
		ReviewItem:   review,
		GoldenRecord: golden,
	})
}

// ListReviewQueue mirrors `handlers::clusters::list_review_queue`.
func (h *Handlers) ListReviewQueue(w http.ResponseWriter, r *http.Request) {
	items, err := h.Review.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ReviewQueueItem]{Data: items})
}

// ListGoldenRecords mirrors `handlers::clusters::list_golden_records`.
func (h *Handlers) ListGoldenRecords(w http.ResponseWriter, r *http.Request) {
	records, err := h.Golden.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.GoldenRecord]{Data: records})
}

// SubmitReview mirrors `handlers::clusters::submit_review`.
func (h *Handlers) SubmitReview(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var body models.SubmitReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cluster, err := h.Clusters.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	review, err := h.Review.LatestForCluster(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}

	updatedCluster, updatedReview := domain.ApplyReview(*cluster, review, body)

	if err := h.Clusters.UpdateAfterReview(r.Context(), id,
		updatedCluster.Status, updatedCluster.RequiresReview, updatedCluster.SuggestedGoldenRecordID); err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if updatedReview != nil {
		if err := h.Review.UpdateAfterReview(r.Context(), *updatedReview); err != nil {
			writeError(w, http.StatusInternalServerError, "database operation failed")
			return
		}
	}

	goldenStatus := "active"
	switch updatedCluster.Status {
	case "rejected":
		goldenStatus = "rejected"
	case "split_requested":
		goldenStatus = "superseded"
	}
	if err := h.Golden.SetStatusByCluster(r.Context(), id, goldenStatus); err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}

	golden, err := h.Golden.LatestForCluster(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ClusterDetail{
		Cluster:      updatedCluster,
		ReviewItem:   updatedReview,
		GoldenRecord: golden,
	})
}
