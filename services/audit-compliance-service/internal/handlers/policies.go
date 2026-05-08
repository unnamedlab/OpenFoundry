// Audit-policy CRUD handlers — mirrors `handlers/policies.rs`.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// CreateAuditPolicy ports `handlers::policies::create_policy`.
func (h *Handlers) CreateAuditPolicy(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateAuditPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "policy name is required")
		return
	}
	v, err := h.Repo.CreateAuditPolicy(r.Context(), &body)
	if err != nil {
		slog.Error("create audit policy", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// UpdateAuditPolicy ports `handlers::policies::update_policy`.
func (h *Handlers) UpdateAuditPolicy(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.UpdateAuditPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := h.Repo.UpdateAuditPolicy(r.Context(), id, &body)
	if err != nil {
		slog.Error("update audit policy", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "policy not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}
