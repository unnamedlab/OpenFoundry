package productdistribution

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

type Handlers struct{ Repo Repository }

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandlers(repo Repository) *Handlers { return &Handlers{Repo: repo} }

func (h *Handlers) ListPeers(w http.ResponseWriter, r *http.Request) {
	items, err := h.Repo.ListPeers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.PeerOrganization]{Items: items})
}

func (h *Handlers) CreatePeer(w http.ResponseWriter, r *http.Request) {
	var req models.CreatePeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	peer, err := h.Repo.CreatePeer(r.Context(), req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, peer)
}

func (h *Handlers) GetPeer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	peer, err := h.Repo.GetPeer(r.Context(), id)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, peer)
}

func (h *Handlers) UpdatePeer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req models.UpdatePeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	peer, err := h.Repo.UpdatePeer(r.Context(), id, req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, peer)
}

func (h *Handlers) DeletePeer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Repo.DeletePeer(r.Context(), id); err != nil {
		handleRepoError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ListShareManifests(w http.ResponseWriter, r *http.Request) {
	items, err := h.Repo.ListShareManifests(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ShareManifest]{Items: items})
}

func (h *Handlers) CreateShareManifest(w http.ResponseWriter, r *http.Request) {
	var req models.CreateShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	manifest, err := h.Repo.CreateShareManifest(r.Context(), req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (h *Handlers) GetShareManifest(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	manifest, err := h.Repo.GetShareManifest(r.Context(), id)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (h *Handlers) ListSyncStatuses(w http.ResponseWriter, r *http.Request) {
	items, err := h.Repo.ListSyncStatuses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.SyncStatus]{Items: items})
}

func (h *Handlers) UpdateSyncStatus(w http.ResponseWriter, r *http.Request) {
	shareID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req models.SyncStatusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, err := h.Repo.UpdateSyncStatus(r.Context(), shareID, req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, http.StatusBadRequest, name+" must be a uuid")
		return uuid.Nil, false
	}
	return id, true
}

func handleRepoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "product distribution resource not found")
	case errors.Is(err, ErrValidation):
		writeError(w, http.StatusBadRequest, stringsAfterColon(err.Error()))
	default:
		writeError(w, http.StatusInternalServerError, "database operation failed")
	}
}

func stringsAfterColon(msg string) string {
	const prefix = "product distribution validation failed: "
	if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
		return msg[len(prefix):]
	}
	return msg
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
