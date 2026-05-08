// Top-level handlers — overview, list with filters, get, append,
// anomalies, collectors. Mirrors `handlers/events.rs` 1:1 (the Rust
// impl lives in src/handlers/events.rs).

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/alerting"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/collector"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/security"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// GetOverview ports `handlers::events::get_overview`.
func (h *Handlers) GetOverview(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
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
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
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
		filtered = append(filtered, e)
	}
	anomalies := alerting.DetectAnomalies(events)
	writeJSON(w, http.StatusOK, models.EventListResponse{
		Items:     filtered,
		Anomalies: anomalies,
	})
}

// GetEvent ports `handlers::events::get_event`.
func (h *Handlers) GetEvent(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
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
	if body.Action == "" {
		writeJSONErr(w, http.StatusBadRequest, "action is required")
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
	if !authed(r) {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	events, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, alerting.DetectAnomalies(events))
}

// ListCollectors ports `handlers::events::list_collectors`.
func (h *Handlers) ListCollectors(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	events, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, collector.CollectorCatalog(events))
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
	return q
}
