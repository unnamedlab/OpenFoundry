// Package handlers exposes the fusion HTTP surface (rules + merge strategies).
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/repo"
)

// Handlers wires the fusion-plane repos.
type Handlers struct {
	Rules           *repo.MatchRuleRepo
	MergeStrategies *repo.MergeStrategyRepo
	Jobs            *repo.FusionJobRepo
	Clusters        *repo.ClusterRepo
	Review          *repo.ReviewQueueRepo
	Golden          *repo.GoldenRecordRepo
	Overview        *repo.OverviewRepo
}

// --- helpers ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, models.ErrorResponse{Error: msg})
}

// --- match rules --------------------------------------------------------

func (h *Handlers) ListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.Rules.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MatchRule]{Data: rules})
}

func (h *Handlers) CreateRule(w http.ResponseWriter, r *http.Request) {
	var body models.CreateMatchRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Conditions) == 0 {
		writeError(w, http.StatusBadRequest, "rule name and at least one condition are required")
		return
	}
	rule, err := h.Rules.Create(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *Handlers) UpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var body models.UpdateMatchRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := h.Rules.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "match rule not found")
		return
	}
	updated, err := h.Rules.Update(r.Context(), id, body, *current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// --- merge strategies ---------------------------------------------------

func (h *Handlers) ListMergeStrategies(w http.ResponseWriter, r *http.Request) {
	rows, err := h.MergeStrategies.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MergeStrategy]{Data: rows})
}

func (h *Handlers) CreateMergeStrategy(w http.ResponseWriter, r *http.Request) {
	var body models.CreateMergeStrategyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "merge strategy name is required")
		return
	}
	ms, err := h.MergeStrategies.Create(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func (h *Handlers) UpdateMergeStrategy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var body models.UpdateMergeStrategyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := h.MergeStrategies.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "merge strategy not found")
		return
	}
	updated, err := h.MergeStrategies.Update(r.Context(), id, body, *current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
