// Package handlers wires the HTTP surface for function-runtime-service.
//
// Every route under /api/v1/functions sits behind libs/auth-middleware
// — handlers pull the caller's tenant + actor from the validated
// Claims via authmw.FromContext.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/executor"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/repo"
)

// Handlers bundles the dependencies the HTTP layer needs.
//
// Now / NewID are overridable so tests can pin time + IDs without
// monkey-patching the package. AsyncQueue is a hook into the
// fire-and-forget executor goroutine; tests may swap it with a
// synchronous shim.
type Handlers struct {
	Store          repo.Store
	Exec           executor.Executor
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	Now            func() time.Time
	NewID          func() uuid.UUID
	AsyncQueue     func(run func())
}

// time + id defaults.
func (h *Handlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

func (h *Handlers) newID() uuid.UUID {
	if h.NewID != nil {
		return h.NewID()
	}
	id, _ := uuid.NewV7()
	return id
}

func (h *Handlers) enqueue(fn func()) {
	if h.AsyncQueue != nil {
		h.AsyncQueue(fn)
		return
	}
	go fn()
}

// ─── definitions ──────────────────────────────────────────────────────

// CreateFunction handles POST /api/v1/functions.
func (h *Handlers) CreateFunction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	var body models.RegisterFunctionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" || body.Namespace == "" {
		writeError(w, http.StatusBadRequest, "namespace and name are required")
		return
	}
	if !body.Runtime.Valid() {
		writeError(w, http.StatusBadRequest, "unsupported runtime")
		return
	}
	fn := &models.FunctionDefinition{
		ID:        h.newID(),
		TenantID:  tenantID,
		Namespace: body.Namespace,
		Name:      body.Name,
		Runtime:   body.Runtime,
		Signature: body.Signature,
		Status:    models.StatusDraft,
	}
	if err := h.Store.CreateFunction(r.Context(), fn); err != nil {
		h.mapStoreError(w, "create function", err)
		return
	}

	// Optional: when SourceURI is supplied at creation time, register
	// the first version immediately so the function is invokable
	// without a second round-trip.
	if body.SourceURI != "" {
		v, err := h.Store.AppendVersion(r.Context(), tenantID, fn.ID, body.SourceURI, body.EntryPoint)
		if err != nil {
			h.mapStoreError(w, "append version", err)
			return
		}
		fn.LatestVersion = v.Version
	}

	writeJSON(w, http.StatusCreated, fn)
}

// ListFunctions handles GET /api/v1/functions.
func (h *Handlers) ListFunctions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := h.Store.ListFunctions(r.Context(), repo.ListFunctionsFilter{
		TenantID:  tenantID,
		Namespace: q.Get("namespace"),
		Status:    models.Status(q.Get("status")),
		Runtime:   models.Runtime(q.Get("runtime")),
		Limit:     parseLimit(q.Get("limit")),
	})
	if err != nil {
		h.mapStoreError(w, "list functions", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GetFunction handles GET /api/v1/functions/{id}.
func (h *Handlers) GetFunction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	fn, err := h.Store.GetFunction(r.Context(), tenantID, id)
	if err != nil {
		h.mapStoreError(w, "get function", err)
		return
	}
	writeJSON(w, http.StatusOK, fn)
}

// PublishVersion handles POST /api/v1/functions/{id}/versions.
func (h *Handlers) PublishVersion(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	var body models.PublishVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.SourceURI == "" {
		writeError(w, http.StatusBadRequest, "source_uri is required")
		return
	}
	v, err := h.Store.AppendVersion(r.Context(), tenantID, id, body.SourceURI, body.EntryPoint)
	if err != nil {
		h.mapStoreError(w, "append version", err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// Activate handles POST /api/v1/functions/{id}/activate.
func (h *Handlers) Activate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	versionStr := r.URL.Query().Get("version")
	if versionStr == "" {
		writeError(w, http.StatusBadRequest, "version query parameter is required")
		return
	}
	version, err := strconv.Atoi(versionStr)
	if err != nil || version <= 0 {
		writeError(w, http.StatusBadRequest, "version must be a positive integer")
		return
	}
	if _, err := h.Store.GetVersion(r.Context(), id, version); err != nil {
		h.mapStoreError(w, "get version", err)
		return
	}
	fn, err := h.Store.UpdateFunctionStatus(r.Context(), tenantID, id, models.StatusActive, &version)
	if err != nil {
		h.mapStoreError(w, "activate", err)
		return
	}
	writeJSON(w, http.StatusOK, fn)
}

// Deprecate handles POST /api/v1/functions/{id}/deprecate.
func (h *Handlers) Deprecate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	fn, err := h.Store.UpdateFunctionStatus(r.Context(), tenantID, id, models.StatusDeprecated, nil)
	if err != nil {
		h.mapStoreError(w, "deprecate", err)
		return
	}
	writeJSON(w, http.StatusOK, fn)
}

// ─── runs ─────────────────────────────────────────────────────────────

// Invoke handles POST /api/v1/functions/{id}/invoke (synchronous).
func (h *Handlers) Invoke(w http.ResponseWriter, r *http.Request) {
	h.invoke(w, r, true)
}

// InvokeAsync handles POST /api/v1/functions/{id}/invoke-async.
func (h *Handlers) InvokeAsync(w http.ResponseWriter, r *http.Request) {
	h.invoke(w, r, false)
}

func (h *Handlers) invoke(w http.ResponseWriter, r *http.Request, sync bool) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	actorID, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	var body models.InvokeRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
	}

	fn, err := h.Store.GetFunction(r.Context(), tenantID, id)
	if err != nil {
		h.mapStoreError(w, "get function", err)
		return
	}
	version := 0
	switch {
	case body.Version != nil:
		version = *body.Version
	case fn.ActiveVersion != nil:
		version = *fn.ActiveVersion
	default:
		writeError(w, http.StatusPreconditionFailed, "function has no active version; pass version explicitly")
		return
	}
	v, err := h.Store.GetVersion(r.Context(), id, version)
	if err != nil {
		h.mapStoreError(w, "get version", err)
		return
	}

	timeout := h.DefaultTimeout
	if body.TimeoutSeconds > 0 {
		timeout = time.Duration(body.TimeoutSeconds) * time.Second
	}
	if h.MaxTimeout > 0 && timeout > h.MaxTimeout {
		timeout = h.MaxTimeout
	}

	run := &models.FunctionRun{
		ID:              h.newID(),
		FunctionID:      fn.ID,
		FunctionVersion: v.Version,
		TenantID:        tenantID,
		ActorID:         actorID,
		Status:          models.RunStatusRunning,
		Input:           body.Input,
		StartedAt:       h.now(),
	}
	if err := h.Store.CreateRun(r.Context(), run); err != nil {
		h.mapStoreError(w, "create run", err)
		return
	}

	if !sync {
		h.enqueue(func() {
			h.executeRun(*fn, *v, *run, timeout)
		})
		writeJSON(w, http.StatusAccepted, run)
		return
	}

	finished, execErr := h.executeRun(*fn, *v, *run, timeout)
	if finished == nil {
		writeError(w, http.StatusInternalServerError, "execution dropped")
		return
	}
	status := http.StatusOK
	switch {
	case errors.Is(execErr, executor.ErrNotImplemented), errors.Is(execErr, domain.ErrExecutorNotAvailable):
		status = http.StatusNotImplemented
	case finished.Status == models.RunStatusTimeout:
		status = http.StatusGatewayTimeout
	case finished.Status == models.RunStatusFailed:
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, finished)
}

// executeRun drives the executor, then persists the terminal state.
// Returns the finished run (or the partially-updated run on store
// failure) plus the original executor error so the synchronous HTTP
// path can distinguish causes that share `RunStatusFailed` (e.g.
// `executor.ErrNotImplemented` → 501 vs user code crash → 500).
func (h *Handlers) executeRun(fn models.FunctionDefinition, version models.FunctionVersion, run models.FunctionRun, timeout time.Duration) (*models.FunctionRun, error) {
	execCtx, cancelExec := execContext(timeout)
	defer cancelExec()
	res, err := h.Exec.Execute(execCtx, fn, version, []byte(run.Input))

	upd := repo.RunUpdate{Status: models.RunStatusSucceeded}
	switch {
	case err == nil:
		upd.Output = res.Output
		upd.DurationMs = res.Duration.Milliseconds()
	case errors.Is(err, domain.ErrExecutionTimeout):
		upd.Status = models.RunStatusTimeout
		upd.Error = err.Error()
		if res != nil {
			upd.DurationMs = res.Duration.Milliseconds()
		}
	default:
		upd.Status = models.RunStatusFailed
		upd.Error = err.Error()
		if res != nil {
			upd.DurationMs = res.Duration.Milliseconds()
		}
	}

	finishCtx, cancelFinish := execContext(timeout)
	defer cancelFinish()
	finished, finishErr := h.Store.FinishRun(finishCtx, run.ID, upd)
	if finishErr != nil {
		slog.Error("finish run failed", slog.String("run_id", run.ID.String()), slog.String("error", finishErr.Error()))
		run.Status = upd.Status
		run.Output = upd.Output
		run.Error = upd.Error
		run.DurationMs = upd.DurationMs
		now := h.now()
		run.FinishedAt = &now
		return &run, err
	}
	return finished, err
}

// GetRun handles GET /api/v1/functions/runs/{run_id}.
func (h *Handlers) GetRun(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "run_id")
	if !ok {
		return
	}
	run, err := h.Store.GetRun(r.Context(), id)
	if err != nil {
		h.mapStoreError(w, "get run", err)
		return
	}
	if tenantID != uuid.Nil && run.TenantID != tenantID {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ListRuns handles GET /api/v1/functions/runs.
func (h *Handlers) ListRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := repo.ListRunsFilter{
		TenantID: tenantID,
		Status:   models.RunStatus(q.Get("status")),
		Limit:    parseLimit(q.Get("limit")),
	}
	if fnStr := q.Get("function_id"); fnStr != "" {
		fnID, err := uuid.Parse(fnStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid function_id")
			return
		}
		filter.FunctionID = fnID
	}
	out, err := h.Store.ListRuns(r.Context(), filter)
	if err != nil {
		h.mapStoreError(w, "list runs", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── helpers ──────────────────────────────────────────────────────────

func (h *Handlers) tenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	tenantID, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant context missing")
		return uuid.Nil, false
	}
	return tenantID, true
}

func (h *Handlers) actor(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "actor context missing")
		return uuid.Nil, false
	}
	return c.Sub, true
}

func parseID(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+key)
		return uuid.Nil, false
	}
	return id, true
}

func parseLimit(v string) int {
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (h *Handlers) mapStoreError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already exists")
	case errors.Is(err, domain.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrPreconditionFailed), errors.Is(err, domain.ErrNoActiveVersion):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, executor.ErrNotImplemented), errors.Is(err, domain.ErrExecutorNotAvailable):
		writeError(w, http.StatusNotImplemented, err.Error())
	default:
		slog.Error(op+" failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// execContext returns a fresh background context bounded by timeout.
// We use background (not r.Context()) so async runs and the final
// FinishRun call still complete after the HTTP request returns.
// Callers MUST defer the returned cancel.
func execContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}
