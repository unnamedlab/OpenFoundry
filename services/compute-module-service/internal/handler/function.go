package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/pagination"
	domainmode "github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/executionmode"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	dispatch "github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/executionmode"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/repo"
)

// InvokeFunctionRequest is the wire shape posted to /invoke and
// /invoke-async. Payload is forwarded verbatim to the runtime; the
// optional module_version pins the target binary when the registry
// resolves the module to multiple versions.
type InvokeFunctionRequest struct {
	Payload       json.RawMessage `json:"payload,omitempty"`
	ModuleVersion string          `json:"module_version,omitempty"`
}

// SyncInvocationResponse is the body returned by POST .../invoke.
type SyncInvocationResponse struct {
	Invocation *function.FunctionInvocation `json:"invocation"`
	Result     json.RawMessage              `json:"result,omitempty"`
}

// AsyncInvocationResponse is the body returned by POST .../invoke-async.
type AsyncInvocationResponse struct {
	InvocationID uuid.UUID       `json:"invocation_id"`
	Status       function.Status `json:"status"`
}

// InvokeFunction handles
// POST /api/v1/compute-modules/{module_id}/functions/{name}/invoke.
//
// The call is synchronous: the handler blocks on the dispatcher until
// the runtime replies or the timeout elapses. 504 is returned on
// timeout; the persisted row still reflects the final status so
// callers can poll via GET /invocations/{id}.
func (s *State) InvokeFunction(w http.ResponseWriter, r *http.Request) {
	caller, tenant, ok := s.requireFunctionCaller(w, r)
	if !ok {
		return
	}
	moduleID, name, ok := s.parseFunctionPath(w, r)
	if !ok {
		return
	}
	payload, version, ok := s.readInvocationPayload(w, r)
	if !ok {
		return
	}

	inv, err := s.runFunctionMode(r.Context(), moduleID, name, version, payload, caller, tenant)
	if err != nil {
		s.writeInvocationError(w, err)
		return
	}

	dispCtx := r.Context()
	timeout := s.DispatchTimeout
	if timeout <= 0 {
		timeout = dispatch.DefaultDispatchTimeout
	}
	if timeout > dispatch.MaxDispatchTimeout {
		timeout = dispatch.MaxDispatchTimeout
	}
	ctx, cancel := context.WithTimeout(dispCtx, timeout)
	defer cancel()

	running, _ := s.Repo.MarkInvocationRunning(ctx, inv.ID)
	if running != nil {
		inv = running
	}

	result, dispErr := s.Dispatcher.Dispatch(ctx, inv)
	completed := s.completeInvocation(ctx, inv, result, dispErr)
	s.emitAudit(r, completed, dispErr)

	body := SyncInvocationResponse{Invocation: completed}
	if len(result.Payload) > 0 {
		body.Result = json.RawMessage(append([]byte(nil), result.Payload...))
	}

	switch {
	case errors.Is(dispErr, function.ErrInvocationTimeout):
		writeJSON(w, http.StatusGatewayTimeout, body)
	case errors.Is(dispErr, function.ErrPayloadTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, body)
	case errors.Is(dispErr, function.ErrFunctionNotFound):
		writeJSON(w, http.StatusNotFound, body)
	case errors.Is(dispErr, function.ErrModuleVersionInactive):
		writeJSON(w, http.StatusConflict, body)
	case dispErr != nil:
		writeJSON(w, http.StatusBadGateway, body)
	default:
		writeJSON(w, http.StatusOK, body)
	}
}

// InvokeFunctionAsync handles
// POST /api/v1/compute-modules/{module_id}/functions/{name}/invoke-async.
//
// The handler enqueues the invocation, returns immediately, and hands
// the dispatch off to a goroutine. The caller polls via GET
// /invocations/{id}.
func (s *State) InvokeFunctionAsync(w http.ResponseWriter, r *http.Request) {
	caller, tenant, ok := s.requireFunctionCaller(w, r)
	if !ok {
		return
	}
	moduleID, name, ok := s.parseFunctionPath(w, r)
	if !ok {
		return
	}
	payload, version, ok := s.readInvocationPayload(w, r)
	if !ok {
		return
	}

	inv, err := s.runFunctionMode(r.Context(), moduleID, name, version, payload, caller, tenant)
	if err != nil {
		s.writeInvocationError(w, err)
		return
	}

	// Detach from the request lifecycle so the dispatch outlives a
	// disconnecting client. The dispatcher applies its own per-call
	// timeout so the goroutine cannot wedge indefinitely.
	go s.runAsync(inv)

	writeJSON(w, http.StatusAccepted, AsyncInvocationResponse{
		InvocationID: inv.ID,
		Status:       inv.Status,
	})
}

// GetInvocation handles
// GET /api/v1/compute-modules/invocations/{invocation_id}.
func (s *State) GetInvocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := callerID(r); !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, ok := pathInvocationID(w, r)
	if !ok {
		return
	}
	row, err := s.Repo.GetInvocation(r.Context(), id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// CancelInvocation handles
// POST /api/v1/compute-modules/invocations/{invocation_id}/cancel.
//
// The handler flips the row to status=cancelled and fires a
// best-effort cancel at the dispatcher. Already-terminal rows return
// 409 so callers can tell the difference between "we cancelled in
// time" and "too late".
func (s *State) CancelInvocation(w http.ResponseWriter, r *http.Request) {
	caller, ok := callerID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, ok := pathInvocationID(w, r)
	if !ok {
		return
	}
	row, err := s.Repo.CancelInvocation(r.Context(), id, caller)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	if s.Dispatcher != nil {
		_ = s.Dispatcher.Cancel(r.Context(), id)
	}
	writeJSON(w, http.StatusOK, row)
}

// ListInvocations handles
// GET /api/v1/compute-modules/invocations.
//
// Query parameters:
//   - module_id (uuid)  — filter to one module
//   - tenant_id (uuid)  — admin-only override; defaults to the caller's tenant
//   - status (str)      — one of queued/running/succeeded/failed/cancelled/timeout
//   - cursor (str)      — opaque pagination cursor
//   - limit (uint)      — page size (default 50, max 200)
func (s *State) ListInvocations(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	q := r.URL.Query()
	filter := repo.InvocationFilter{TenantID: &tenant}
	if v := q.Get("module_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid module_id")
			return
		}
		filter.ModuleID = &id
	}
	if v := q.Get("status"); v != "" {
		st := function.Status(v)
		if !st.IsValid() {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		filter.Status = &st
	}
	page := repo.Page{}
	if v := q.Get("cursor"); v != "" {
		c := v
		page.Cursor = &c
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		page.Limit = uint32(n)
	}

	res, err := s.Repo.ListInvocations(r.Context(), filter, page)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pagination.PageResponse[*function.FunctionInvocation]{
		Items:      res.Items,
		NextCursor: res.NextCursor,
	})
}

func (s *State) requireFunctionCaller(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	caller, ok := callerID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return uuid.UUID{}, uuid.UUID{}, false
	}
	tenant, ok := tenantID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant claim required")
		return uuid.UUID{}, uuid.UUID{}, false
	}
	return caller, tenant, true
}

func (s *State) parseFunctionPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	rawID := chi.URLParam(r, "module_id")
	if rawID == "" {
		rawID = chi.URLParam(r, "id")
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid module id")
		return uuid.UUID{}, "", false
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "function name required")
		return uuid.UUID{}, "", false
	}
	return id, name, true
}

func (s *State) payloadLimit() int64 {
	if s.PayloadLimitBytes > 0 {
		return s.PayloadLimitBytes
	}
	return dispatch.DefaultBodyLimitBytes
}

func (s *State) readInvocationPayload(w http.ResponseWriter, r *http.Request) (json.RawMessage, string, bool) {
	limit := s.payloadLimit()
	r.Body = http.MaxBytesReader(w, r.Body, limit+1)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload exceeds configured limit")
			return nil, "", false
		}
		writeError(w, http.StatusBadRequest, "could not read request body")
		return nil, "", false
	}
	if int64(len(raw)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "payload exceeds configured limit")
		return nil, "", false
	}
	if len(raw) == 0 {
		return json.RawMessage("null"), "", true
	}
	var req InvokeFunctionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, "", false
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage("null")
	}
	return req.Payload, strings.TrimSpace(req.ModuleVersion), true
}

func (s *State) runFunctionMode(ctx context.Context, moduleID uuid.UUID, name, version string, payload json.RawMessage, caller, tenant uuid.UUID) (*function.FunctionInvocation, error) {
	m, err := s.Repo.Get(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	if err := domainmode.EnsureFunctionMode(m); err != nil {
		return nil, err
	}
	inv := function.FunctionInvocation{
		ModuleID:      m.ID,
		ModuleVersion: version,
		FunctionName:  name,
		Payload:       append(json.RawMessage(nil), payload...),
		TenantID:      tenant,
		ActorID:       caller,
		Status:        function.StatusQueued,
	}
	return s.Repo.CreateInvocation(ctx, inv)
}

func (s *State) writeInvocationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domainmode.ErrFunctionOnly):
		writeExecutionModeError(w, err)
	case errors.Is(err, repo.ErrNotFound):
		writeError(w, http.StatusNotFound, "compute module not found")
	default:
		writeRepoError(w, err)
	}
}

func (s *State) completeInvocation(ctx context.Context, inv *function.FunctionInvocation, result dispatch.Result, dispErr error) *function.FunctionInvocation {
	if !result.Status.IsValid() {
		result.Status = function.StatusFailed
	}
	if !result.Status.IsTerminal() {
		result.Status = function.StatusFailed
	}
	update := repo.InvocationCompletion{
		Status:    result.Status,
		Result:    result.Payload,
		CostUnits: result.DurationMs,
	}
	if dispErr != nil {
		update.ErrorMessage = dispErr.Error()
	}
	completed, err := s.Repo.CompleteInvocation(ctx, inv.ID, update)
	if err != nil || completed == nil {
		// Repo failures here are surfaced via the audit log; the
		// in-memory snapshot we already hold remains the truthful
		// response shape for the caller.
		if completed == nil {
			completed = inv.Clone()
			completed.Status = update.Status
			now := timeFromState(s)
			completed.FinishedAt = &now
			completed.ErrorMessage = update.ErrorMessage
			completed.CostUnits = update.CostUnits
		}
	}
	return completed
}

func (s *State) runAsync(inv *function.FunctionInvocation) {
	ctx, cancel := context.WithTimeout(context.Background(), s.DispatchTimeout+5*time.Second)
	defer cancel()
	if s.DispatchTimeout <= 0 {
		ctx, cancel = context.WithTimeout(context.Background(), dispatch.DefaultDispatchTimeout+5*time.Second)
		defer cancel()
	}
	if running, err := s.Repo.MarkInvocationRunning(ctx, inv.ID); err == nil && running != nil {
		inv = running
	}
	result, dispErr := s.Dispatcher.Dispatch(ctx, inv)
	completed := s.completeInvocation(ctx, inv, result, dispErr)
	s.emitAsyncAudit(ctx, completed, dispErr)
}

func (s *State) emitAudit(r *http.Request, inv *function.FunctionInvocation, dispErr error) {
	lg := s.AuditLogger
	if lg == nil {
		lg = slog.Default()
	}
	kind := "compute.function.invoked"
	if dispErr != nil || (inv != nil && inv.Status == function.StatusFailed) {
		kind = "compute.function.failed"
	}
	attrs := invocationAuditAttrs(inv, kind, dispErr)
	lg.LogAttrs(r.Context(), slog.LevelInfo, kind, attrs...)
}

func (s *State) emitAsyncAudit(ctx context.Context, inv *function.FunctionInvocation, dispErr error) {
	lg := s.AuditLogger
	if lg == nil {
		lg = slog.Default()
	}
	kind := "compute.function.invoked"
	if dispErr != nil || (inv != nil && inv.Status == function.StatusFailed) {
		kind = "compute.function.failed"
	}
	lg.LogAttrs(ctx, slog.LevelInfo, kind, invocationAuditAttrs(inv, kind, dispErr)...)
}

func invocationAuditAttrs(inv *function.FunctionInvocation, kind string, err error) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("category", "audit"),
		slog.String("kind", kind),
	}
	if inv != nil {
		attrs = append(attrs,
			slog.String("invocation_id", inv.ID.String()),
			slog.String("module_id", inv.ModuleID.String()),
			slog.String("function_name", inv.FunctionName),
			slog.String("tenant_id", inv.TenantID.String()),
			slog.String("actor_id", inv.ActorID.String()),
			slog.String("status", string(inv.Status)),
			slog.Int64("cost_units", inv.CostUnits),
		)
		if inv.ModuleVersion != "" {
			attrs = append(attrs, slog.String("module_version", inv.ModuleVersion))
		}
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	return attrs
}

func pathInvocationID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "invocation_id")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid invocation id")
		return uuid.UUID{}, false
	}
	return id, true
}

func timeFromState(s *State) time.Time {
	if s == nil || s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now()
}
