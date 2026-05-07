// Package handlers wires the generic HTTP CRUD for one feature pair.
//
// Each of the four features (telemetry-exports, health-checks,
// execution-runs, monitoring-rules) gets its own Feature struct
// instance in the server, mounted at /api/v1/<feature>.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/repo"
)

// Feature is the per-feature handler instance.
type Feature struct{ Repo *repo.FeatureRepo }

// ListPrimary handles GET /api/v1/<feature>.
func (f *Feature) ListPrimary(w http.ResponseWriter, r *http.Request) {
	rows, err := f.Repo.ListPrimary(r.Context())
	if err != nil {
		slog.Error("list primary failed", slog.String("feature", f.Repo.Tables.Feature), slog.String("error", err.Error()))
		writeText(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// CreatePrimary handles POST /api/v1/<feature>.
func (f *Feature) CreatePrimary(w http.ResponseWriter, r *http.Request) {
	var body models.CreatePrimaryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeText(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := f.Repo.CreatePrimary(r.Context(), ids.New(), body.Payload)
	if err != nil {
		slog.Error("create primary failed", slog.String("feature", f.Repo.Tables.Feature), slog.String("error", err.Error()))
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// GetPrimary handles GET /api/v1/<feature>/{id}.
func (f *Feature) GetPrimary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	row, err := f.Repo.GetPrimary(r.Context(), id)
	if err != nil {
		slog.Error("get primary failed", slog.String("feature", f.Repo.Tables.Feature), slog.String("error", err.Error()))
		writeText(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeText(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// ListSecondary handles GET /api/v1/<feature>/{id}/<children>.
func (f *Feature) ListSecondary(w http.ResponseWriter, r *http.Request) {
	parent, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := f.Repo.ListSecondary(r.Context(), parent)
	if err != nil {
		slog.Error("list secondary failed", slog.String("feature", f.Repo.Tables.Feature), slog.String("error", err.Error()))
		writeText(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// CreateSecondary handles POST /api/v1/<feature>/{id}/<children>.
func (f *Feature) CreateSecondary(w http.ResponseWriter, r *http.Request) {
	parent, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.CreateSecondaryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeText(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := f.Repo.CreateSecondary(r.Context(), ids.New(), parent, body.Payload)
	if err != nil {
		slog.Error("create secondary failed", slog.String("feature", f.Repo.Tables.Feature), slog.String("error", err.Error()))
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeText(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg))
}
