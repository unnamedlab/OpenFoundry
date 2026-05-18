// Top-level handlers — overview, list with filters, get, append,
// anomalies, collectors.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/alerting"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/auditmonitoring"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/collector"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/security"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// GetOverview ports `handlers::events::get_overview`.
func (h *Handlers) GetOverview(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	all, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		slog.Error("get overview", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	events := security.FilterEventsForClaims(all, claims)
	anomalies := alerting.DetectAnomalies(events)
	collectors := collector.CollectorCatalog(events)

	criticalCount := int64(0)
	subjectCount := int64(0)
	for i := range events {
		if models.AuditSeverity(events[i].Severity).IsCritical() {
			criticalCount++
		}
		if events[i].SubjectID != nil && *events[i].SubjectID != "" {
			subjectCount++
		}
	}
	var latest *models.AuditEvent
	if len(events) > 0 {
		first := events[0]
		latest = &first
	}
	writeJSON(w, http.StatusOK, models.AuditOverview{
		EventCount:         int64(len(events)),
		CriticalEventCount: criticalCount,
		CollectorCount:     int64(len(collectors)),
		ActivePolicyCount:  0,
		AnomalyCount:       int64(len(anomalies)),
		GDPRSubjectCount:   subjectCount,
		LatestEvent:        latest,
	})
}

// ListEvents ports `handlers::events::list_events` with filtering.
func (h *Handlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	all, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	events := security.FilterEventsForClaims(all, claims)
	q := parseEventQuery(r)
	filtered := events[:0:0]
	for _, e := range events {
		if q.SourceService != nil && e.SourceService != *q.SourceService {
			continue
		}
		if q.SubjectID != nil {
			if e.SubjectID == nil || *e.SubjectID != *q.SubjectID {
				continue
			}
		}
		if q.Classification != nil && e.Classification != *q.Classification {
			continue
		}
		if q.ResourceID != nil && e.ResourceID != *q.ResourceID {
			continue
		}
		if q.TraceID != nil && (e.TraceID == nil || *e.TraceID != *q.TraceID) {
			continue
		}
		if q.EventID != nil && e.EventID.String() != *q.EventID {
			continue
		}
		if q.Category != nil && !stringInSlice(e.Categories, *q.Category) {
			continue
		}
		filtered = append(filtered, e)
	}
	anomalies := alerting.DetectAnomalies(events)
	writeJSON(w, http.StatusOK, models.EventListResponse{
		Items:     filtered,
		Anomalies: anomalies,
	})
}

func (h *Handlers) GetAuditMonitoringStarterPack(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	all, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	events := security.FilterEventsForClaims(all, claims)
	writeJSON(w, http.StatusOK, auditmonitoring.StarterPack(events))
}

// GetEvent ports `handlers::events::get_event`.
func (h *Handlers) GetEvent(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	event, err := h.Repo.GetAuditEvent(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if event == nil {
		writeJSONErr(w, http.StatusNotFound, "audit event not found")
		return
	}
	if !security.CanAccessEvent(event, claims) {
		writeJSONErr(w, http.StatusNotFound, "audit event not found")
		return
	}
	writeJSON(w, http.StatusOK, event)
}

// AppendEvent ports `handlers::events::append_event`.
//
// Anonymous (no JWT) — gateway and other in-cluster services POST
// here without a token. The Rust handler mirrors this behaviour.
func (h *Handlers) AppendEvent(w http.ResponseWriter, r *http.Request) {
	var body models.AppendAuditEventRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeAppendAuditEventRequest(r, &body)
	if body.Action == "" {
		writeJSONErr(w, http.StatusBadRequest, "action is required")
		return
	}
	if !validOptionalJSONObject(body.Metadata) ||
		!validOptionalJSONObject(body.ErrorMetadata) ||
		!validOptionalJSONObject(body.RequestFields) ||
		!validOptionalJSONObject(body.ResultFields) ||
		!validOptionalJSONArray(body.Entities) {
		writeJSONErr(w, http.StatusBadRequest, "metadata, error_metadata, request_fields, and result_fields must be JSON objects; entities must be a JSON array")
		return
	}
	event, err := h.Repo.PersistAuditEvent(r.Context(), &body)
	if err != nil {
		slog.Error("append audit event", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, event)
}

// ListAnomalies ports `handlers::events::list_anomalies`.
func (h *Handlers) ListAnomalies(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	events, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	events = security.FilterEventsForClaims(events, claims)
	writeJSON(w, http.StatusOK, alerting.DetectAnomalies(events))
}

// ListCollectors ports `handlers::events::list_collectors`.
func (h *Handlers) ListCollectors(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	events, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	events = security.FilterEventsForClaims(events, claims)
	writeJSON(w, http.StatusOK, collector.CollectorCatalog(events))
}

func requireAuditLogAccess(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	if !security.CanViewAuditLogs(claims) {
		writeJSONErr(w, http.StatusForbidden, "audit log access requires audit-logs:view")
		return nil, false
	}
	return claims, true
}

func normalizeAppendAuditEventRequest(r *http.Request, body *models.AppendAuditEventRequest) {
	if body.Product == "" {
		body.Product = body.SourceService
	}
	if body.ProducerType == "" {
		body.ProducerType = "SERVER"
	}
	if body.ActorID == "" {
		body.ActorID = body.Actor
	}
	if body.ActorType == "" {
		body.ActorType = "user"
		if body.ServiceAccountID != nil && *body.ServiceAccountID != "" {
			body.ActorType = "service"
		}
	}
	if body.InitiatorType == "" {
		body.InitiatorType = "user"
	}
	if body.AuditAccessTier == "" {
		body.AuditAccessTier = "security_sensitive"
	}
	if len(body.Origins) == 0 {
		body.Origins = requestOrigins(r)
	}
	if body.Origin == nil {
		if origin := firstNonEmpty(r.Header.Get("X-Forwarded-For"), r.RemoteAddr); origin != "" {
			body.Origin = &origin
		}
	}
	if body.SourceOrigin == nil {
		if origin := firstNonEmpty(r.Header.Get("X-Real-IP"), r.RemoteAddr); origin != "" {
			body.SourceOrigin = &origin
		}
	}
	if body.TraceID == nil {
		if trace := traceIDFromRequest(r); trace != "" {
			body.TraceID = &trace
		}
	}
	if body.SessionID == nil {
		if sid := r.Header.Get("X-Session-Id"); sid != "" {
			body.SessionID = &sid
		}
	}
	if body.TokenID == nil {
		if token := r.Header.Get("X-Token-Id"); token != "" {
			body.TokenID = &token
		}
	}
	if body.Outcome == "" {
		switch body.Status {
		case models.StatusSuccess:
			body.Outcome = "success"
		case models.StatusDenied:
			body.Outcome = "unauthorized"
		default:
			body.Outcome = "error"
		}
	}
}

func requestOrigins(r *http.Request) []string {
	values := []string{}
	for _, header := range []string{"X-Forwarded-For", "Forwarded", "Origin", "Referer"} {
		raw := r.Header.Get(header)
		if raw == "" {
			continue
		}
		for _, part := range strings.Split(raw, ",") {
			if v := strings.TrimSpace(part); v != "" {
				values = append(values, v)
			}
		}
	}
	return values
}

func traceIDFromRequest(r *http.Request) string {
	if raw := strings.TrimSpace(r.Header.Get("X-Trace-Id")); raw != "" {
		return raw
	}
	traceparent := strings.TrimSpace(r.Header.Get("Traceparent"))
	parts := strings.Split(traceparent, "-")
	if len(parts) >= 2 && parts[1] != "" {
		return parts[1]
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func validOptionalJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var holder map[string]any
	return json.Unmarshal(raw, &holder) == nil
}

func validOptionalJSONArray(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var holder []any
	return json.Unmarshal(raw, &holder) == nil
}

func stringInSlice(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func parseEventQuery(r *http.Request) models.EventQuery {
	q := models.EventQuery{}
	if v := r.URL.Query().Get("source_service"); v != "" {
		q.SourceService = &v
	}
	if v := r.URL.Query().Get("subject_id"); v != "" {
		q.SubjectID = &v
	}
	if v := r.URL.Query().Get("classification"); v != "" {
		q.Classification = &v
	}
	if v := r.URL.Query().Get("resource_id"); v != "" {
		q.ResourceID = &v
	}
	if v := r.URL.Query().Get("category"); v != "" {
		q.Category = &v
	}
	if v := r.URL.Query().Get("trace_id"); v != "" {
		q.TraceID = &v
	}
	if v := r.URL.Query().Get("event_id"); v != "" {
		q.EventID = &v
	}
	return q
}
