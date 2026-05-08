// Package handlers wires the HTTP endpoints for media-sets-service.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
)

type Handlers struct{ Repo *repo.Repo }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// validSchema mirrors the DB CHECK constraint, including the H7 DICOM
// extension landed in migration 0008_dicom_schema.sql.
func validSchema(s string) bool {
	switch s {
	case "IMAGE", "AUDIO", "VIDEO", "DOCUMENT", "SPREADSHEET", "EMAIL", "DICOM":
		return true
	}
	return false
}

func validTransactionPolicy(s string) bool {
	return s == "TRANSACTIONLESS" || s == "TRANSACTIONAL"
}

func (h *Handlers) ListMediaSets(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListMediaSets(r.Context(), r.URL.Query().Get("project_rid"))
	if err != nil {
		slog.Error("list media sets", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list media sets")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MediaSet]{Items: items})
}

func (h *Handlers) GetMediaSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	v, err := h.Repo.GetMediaSet(r.Context(), rid)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "media set not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) CreateMediaSet(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateMediaSetRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.ProjectRID == "" || body.Name == "" || body.Schema == "" {
		writeJSONErr(w, http.StatusBadRequest, "project_rid, name, schema required")
		return
	}
	if !validSchema(body.Schema) {
		writeJSONErr(w, http.StatusBadRequest, "schema must be IMAGE / AUDIO / VIDEO / DOCUMENT / SPREADSHEET / EMAIL / DICOM")
		return
	}
	if body.TransactionPolicy != nil && !validTransactionPolicy(*body.TransactionPolicy) {
		writeJSONErr(w, http.StatusBadRequest, "transaction_policy must be TRANSACTIONLESS or TRANSACTIONAL")
		return
	}
	v, err := h.Repo.CreateMediaSet(r.Context(), &body, caller.Sub.String())
	if err != nil {
		slog.Error("create media set", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) UpdateMediaSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	var body models.UpdateMediaSetRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Repo.UpdateMediaSet(r.Context(), rid, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "media set not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) DeleteMediaSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	deleted, err := h.Repo.DeleteMediaSet(r.Context(), rid)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "media set not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
