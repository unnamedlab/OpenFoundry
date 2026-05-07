package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/accesspatterns"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

// AccessPatternHandlers wires the access-pattern HTTP surface.
// Construct via NewAccessPatternHandlers in main.go and mount on the
// /api/v1 sub-router.
type AccessPatternHandlers struct {
	Service *accesspatterns.Service
}

// auditCtxFromRequest builds an audit-trail context from the
// authenticated principal and the standard tracing headers.
// Mirrors media-sets-service Rust handlers/audit::from_request.
func auditCtxFromRequest(claims *authmw.Claims, r *http.Request) audittrail.AuditContext {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = uuid.New().String()
	}
	return audittrail.AuditContext{
		ActorID:       claims.Sub.String(),
		IP:            clientIP(r),
		UserAgent:     r.Header.Get("User-Agent"),
		RequestID:     requestID,
		CorrelationID: r.Header.Get("X-Correlation-Id"),
		SourceService: "media-sets-service",
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.SplitN(xff, ",", 2)[0]; strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	return r.Header.Get("X-Real-Ip")
}

// ListAccessPatterns — GET /api/v1/media-sets/{rid}/access-patterns.
func (h *AccessPatternHandlers) ListAccessPatterns(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	rows, err := h.Service.List(r.Context(), rid)
	if err != nil {
		slog.Error("list access patterns", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list access patterns")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.AccessPattern]{Items: rows})
}

// RegisterAccessPattern — POST /api/v1/media-sets/{rid}/access-patterns.
func (h *AccessPatternHandlers) RegisterAccessPattern(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	var body models.RegisterAccessPatternRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := h.Service.Register(r.Context(), rid, body, caller.Sub.String(), auditCtxFromRequest(caller, r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// RunAccessPattern — POST /api/v1/access-patterns/{id}/run?item_rid=...
func (h *AccessPatternHandlers) RunAccessPattern(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	patternID := strings.TrimSpace(chi.URLParam(r, "id"))
	itemRID := strings.TrimSpace(r.URL.Query().Get("item_rid"))
	if patternID == "" || itemRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "id and item_rid query param required")
		return
	}
	resp, err := h.Service.Run(r.Context(), accesspatterns.RunInput{
		PatternID: patternID,
		ItemRID:   itemRID,
		InvokedBy: caller.Sub.String(),
		AuditCtx:  auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ItemAccessPatternShortcut — GET /api/v1/items/{rid}/access-patterns/{kind}/url
func (h *AccessPatternHandlers) ItemAccessPatternShortcut(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	itemRID := strings.TrimSpace(chi.URLParam(r, "rid"))
	kind := strings.TrimSpace(chi.URLParam(r, "kind"))
	if itemRID == "" || kind == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid and kind required")
		return
	}
	resp, err := h.Service.RunByKind(r.Context(), itemRID, kind, accesspatterns.RunInput{
		InvokedBy: caller.Sub.String(),
		AuditCtx:  auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeServiceError maps the service-layer error categories to HTTP
// status codes. Anything else surfaces as 500 with the error message
// (the slog.Error inside callers covers operator visibility).
func writeServiceError(w http.ResponseWriter, err error) {
	var bad *accesspatterns.ErrBadRequest
	var notFound *accesspatterns.ErrNotFound
	switch {
	case errors.As(err, &bad):
		writeJSONErr(w, http.StatusBadRequest, bad.Msg)
	case errors.As(err, &notFound):
		writeJSONErr(w, http.StatusNotFound, notFound.Error())
	default:
		slog.Error("access pattern handler", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
	}
}
