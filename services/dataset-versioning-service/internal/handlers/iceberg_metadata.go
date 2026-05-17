package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/repo"
)

func (h *Handlers) GetIcebergMetadata(w http.ResponseWriter, r *http.Request) {
	datasetID, ok := h.resolveDatasetForCatalog(w, r)
	if !ok {
		return
	}
	out, err := h.Repo.GetDatasetIcebergMetadata(r.Context(), datasetID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeCodedJSONErr(w, http.StatusNotFound, apiErrorNotFound, "iceberg metadata not found")
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, "failed to load iceberg metadata")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) PutIcebergMetadata(w http.ResponseWriter, r *http.Request) {
	datasetID, ok := h.resolveDatasetForCatalog(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireDatasetWrite(w, r, datasetID); !ok {
		return
	}
	var body models.PutDatasetIcebergMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	out, err := h.Repo.PutDatasetIcebergMetadata(r.Context(), datasetID, &body)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeJSONErr(w, http.StatusNotFound, "dataset not found")
			return
		}
		if errors.Is(err, repo.ErrValidation) {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, "failed to save iceberg metadata")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
