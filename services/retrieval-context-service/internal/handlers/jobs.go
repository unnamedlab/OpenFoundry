// Package handlers exposes the document-intelligence HTTP surface:
//
//	POST   /api/v1/document-intelligence/jobs
//	GET    /api/v1/document-intelligence/jobs
//	GET    /api/v1/document-intelligence/jobs/{id}
//	PATCH  /api/v1/document-intelligence/jobs/{id}
//	DELETE /api/v1/document-intelligence/jobs/{id}
//	POST   /api/v1/document-intelligence/jobs/{id}/events
//	GET    /api/v1/document-intelligence/jobs/{id}/events
//	POST   /api/v1/document-intelligence/jobs/{id}/extractions
//	GET    /api/v1/document-intelligence/jobs/{id}/extractions
//
// All routes assume the auth middleware ran first; claims are read via
// libs/auth-middleware. Sentinel errors from domain map to HTTP status
// codes via writeDomainError.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/repo"
)

// Jobs is the wire layer for document-intelligence routes.
type Jobs struct {
	Store  repo.Store
	Logger *slog.Logger
}

// Mount attaches Jobs to a chi.Router. Caller is expected to apply auth
// middleware on the parent group.
func (h *Jobs) Mount(r chi.Router) {
	r.Post("/jobs", h.CreateJob)
	r.Get("/jobs", h.ListJobs)
	r.Get("/jobs/{id}", h.GetJob)
	r.Patch("/jobs/{id}", h.UpdateJob)
	r.Delete("/jobs/{id}", h.DeleteJob)

	r.Post("/jobs/{id}/events", h.AppendEvent)
	r.Get("/jobs/{id}/events", h.ListEvents)

	r.Post("/jobs/{id}/extractions", h.RecordExtraction)
	r.Get("/jobs/{id}/extractions", h.ListExtractions)
}

// --- HTTP wiring ------------------------------------------------------------

func (h *Jobs) CreateJob(w http.ResponseWriter, r *http.Request) {
	var body models.CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := domain.ValidateCreateJob(body); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	status := models.JobStatusQueued
	if body.Status != nil {
		status = *body.Status
	}
	j := models.Job{
		ID:        uuid.New(),
		SourceURI: strings.TrimSpace(body.SourceURI),
		MimeType:  body.MimeType,
		Pipeline:  strings.TrimSpace(body.Pipeline),
		Status:    status,
		Options:   body.Options,
	}
	created, err := h.Store.CreateJob(r.Context(), j)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Jobs) GetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	j, err := h.Store.GetJob(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (h *Jobs) ListJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := repo.ListJobsFilter{
		Pipeline: q.Get("pipeline"),
	}
	if raw := q.Get("status"); raw != "" {
		st := models.NormalizeJobStatus(raw)
		if !st.IsValid() {
			writeError(w, http.StatusBadRequest, "unknown status: "+raw)
			return
		}
		filter.Status = st
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit must be in (0,500]")
			return
		}
		filter.Limit = int32(n)
	}
	if c := q.Get("cursor"); c != "" {
		filter.Cursor = &c
	}

	items, next, err := h.Store.ListJobs(r.Context(), filter)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListJobsResponse{Data: items, NextCursor: next})
}

func (h *Jobs) UpdateJob(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	var body models.UpdateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	current, err := h.Store.GetJob(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if err := domain.ValidateUpdateJob(current, body); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	updated, err := h.Store.UpdateJob(r.Context(), id, body)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Jobs) DeleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	if err := h.Store.DeleteJob(r.Context(), id); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Jobs) AppendEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	var body models.AppendEventRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	body.Status = models.NormalizeJobStatus(string(body.Status))
	if err := domain.ValidateAppendEvent(body); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	ev := models.StatusEvent{
		ID:      uuid.New(),
		JobID:   id,
		Status:  body.Status,
		Message: body.Message,
	}
	created, err := h.Store.AppendEvent(r.Context(), ev)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Jobs) ListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	events, err := h.Store.ListEvents(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListEventsResponse{Data: events})
}

func (h *Jobs) RecordExtraction(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	var body models.RecordExtractionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := domain.ValidateRecordExtraction(body); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	ex := models.Extraction{
		ID:             uuid.New(),
		JobID:          id,
		ExtractionKind: strings.TrimSpace(body.ExtractionKind),
		Payload:        body.Payload,
		Confidence:     body.Confidence,
	}
	created, err := h.Store.RecordExtraction(r.Context(), ex)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Jobs) ListExtractions(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	out, err := h.Store.ListExtractions(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListExtractionsResponse{Data: out})
}

// --- helpers ---------------------------------------------------------------

func parseJobID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return uuid.Nil, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func (h *Jobs) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInvalidInput), errors.Is(err, domain.ErrUnknownStatus):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrIllegalStateTransition), errors.Is(err, domain.ErrPreconditionFailed):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	default:
		// Unmapped errors signal a server-side fault: log and surface 500.
		log := h.Logger
		if log == nil {
			log = slog.Default()
		}
		sub := ""
		if claims, ok := authmw.FromContext(r.Context()); ok {
			sub = claims.Sub.String()
		}
		log.Error("document-intelligence handler failed",
			slog.String("error", err.Error()),
			slog.String("subject", sub),
			slog.String("path", r.URL.Path),
		)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
