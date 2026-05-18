// Package handler contains the HTTP handlers for compute-module-service.
//
// Handlers map repository sentinels (repo.ErrNotFound, …) and
// validation errors (models.ValidationError) to canonical HTTP
// responses. The wire shapes mirror libs/core-models so the frontend
// can reuse its shared TypeScript types.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	dispatch "github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/executionmode"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/repo"
)

// State carries the dependencies every Compute Module handler needs.
type State struct {
	Repo       repo.Repository
	Dispatcher dispatch.Dispatcher
	// PayloadLimitBytes caps function-mode payloads ingested by handlers.
	// Zero falls back to dispatch.DefaultBodyLimitBytes.
	PayloadLimitBytes int64
	// DispatchTimeout caps the per-call deadline; mirrors the dispatcher's
	// configured timeout so handler-side context wrapping stays in sync.
	DispatchTimeout time.Duration
	// AuditLogger is the slog handle structured audit lines are emitted
	// on. Falls back to slog.Default() when nil.
	AuditLogger *slog.Logger
	// Now overrides the wall clock for deterministic tests.
	Now func() time.Time
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

type errorBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func writeValidationError(w http.ResponseWriter, err error) {
	var v *models.ValidationError
	if errors.As(err, &v) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: v.Msg, Field: v.Field})
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeRepoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		writeError(w, http.StatusNotFound, "compute module not found")
	case errors.Is(err, repo.ErrNameConflict):
		writeError(w, http.StatusConflict, "name already in use in target folder")
	case errors.Is(err, repo.ErrAlreadyArchived):
		writeError(w, http.StatusConflict, "compute module is already archived")
	case errors.Is(err, repo.ErrNotArchived):
		writeError(w, http.StatusConflict, "compute module is not archived")
	case errors.Is(err, repo.ErrExecutionModeMismatch):
		writeError(w, http.StatusConflict, "operation not supported for this execution mode")
	case errors.Is(err, function.ErrInvocationNotFound):
		writeError(w, http.StatusNotFound, "invocation not found")
	case errors.Is(err, function.ErrInvocationTerminal):
		writeError(w, http.StatusConflict, "invocation has already reached a terminal status")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// tenantID resolves the caller's tenant (claims.OrgID) from the JWT.
// Function-mode invocations require a tenant claim; anonymous and
// pre-onboarding callers are rejected by the handler.
func tenantID(r *http.Request) (uuid.UUID, bool) {
	c, ok := authmw.FromContext(r.Context())
	if !ok || c.OrgID == nil || *c.OrgID == uuid.Nil {
		return uuid.UUID{}, false
	}
	return *c.OrgID, true
}

// callerID returns the authenticated caller's UUID, or false when the
// request is unauthenticated. Handlers reject anonymous requests with
// a 401.
func callerID(r *http.Request) (caller uuid.UUID, ok bool) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		return uuid.UUID{}, false
	}
	return c.Sub, true
}
