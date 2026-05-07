package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// RestrictedViews wires CRUD for the slice-7a CBAC restricted-view rows.
type RestrictedViews struct{ Auth *RBAC }

// NewRestrictedViews wraps an existing RBAC handler so the embedded
// repo + auth helpers don't need re-importing here.
func NewRestrictedViews(rbac *RBAC) *RestrictedViews { return &RestrictedViews{Auth: rbac} }

// List handles GET /api/v1/restricted-views.
func (h *RestrictedViews) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.Auth.Repo.ListRestrictedViews(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// Get handles GET /api/v1/restricted-views/{id}.
func (h *RestrictedViews) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	v, err := h.Auth.Repo.GetRestrictedView(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// Create handles POST /api/v1/restricted-views.
func (h *RestrictedViews) Create(w http.ResponseWriter, r *http.Request) {
	caller := authCallerID(r)
	var body models.CreateRestrictedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" || body.Resource == "" || body.Action == "" {
		writeJSONErr(w, http.StatusBadRequest, "name, resource and action required")
		return
	}
	v, err := h.Auth.Repo.CreateRestrictedView(r.Context(), &body, caller)
	if err != nil {
		slog.Error("create restricted view", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// Update handles PATCH /api/v1/restricted-views/{id}.
func (h *RestrictedViews) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.UpdateRestrictedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Auth.Repo.UpdateRestrictedView(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// Delete handles DELETE /api/v1/restricted-views/{id}.
func (h *RestrictedViews) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.Auth.Repo.DeleteRestrictedView(r.Context(), id); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
