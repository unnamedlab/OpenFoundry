package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

func (h *Handlers) ListCheckpointPolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListCheckpointPolicies(r.Context())
	if err != nil {
		slog.Error("list checkpoint policies", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list policies")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.CheckpointPolicy]{Items: items})
}

func (h *Handlers) ListSensitiveInteractionConfigs(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListSensitiveInteractionConfigs(r.Context())
	if err != nil {
		slog.Error("list sensitive interaction configs", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list configs")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.SensitiveInteractionConfig]{Items: items})
}

func (h *Handlers) ListPurposeTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListPurposeTemplates(r.Context())
	if err != nil {
		slog.Error("list purpose templates", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list templates")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.PurposeTemplate]{Items: items})
}

// CreatePurposeRecord handles POST /api/v1/purpose-records — the audit
// ledger write path.
func (h *Handlers) CreatePurposeRecord(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreatePurposeRecordRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.InteractionType == "" || body.Status == "" {
		writeJSONErr(w, http.StatusBadRequest, "interaction_type and status required")
		return
	}
	if body.ActorID == nil {
		actorID := caller.Sub
		body.ActorID = &actorID
	}
	v, err := h.Repo.CreatePurposeRecord(r.Context(), &body)
	if err != nil {
		slog.Error("create purpose record", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// ListPurposeRecordsByInteraction handles
// GET /api/v1/purpose-records?interaction_type=X&limit=N.
func (h *Handlers) ListPurposeRecordsByInteraction(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	interactionType := r.URL.Query().Get("interaction_type")
	if interactionType == "" {
		// Allow it as a path param via chi too.
		interactionType = chi.URLParam(r, "interaction_type")
	}
	if interactionType == "" {
		writeJSONErr(w, http.StatusBadRequest, "interaction_type query param required")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	items, err := h.Repo.ListPurposeRecordsByInteraction(r.Context(), interactionType, limit)
	if err != nil {
		slog.Error("list purpose records", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list records")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.PurposeRecord]{Items: items})
}
