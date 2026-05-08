// GDPR subject-export handler — mirrors `handlers/gdpr.rs`.

package handlers

import (
	"encoding/json"
	"net/http"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/gdpr"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/security"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// ExportSubjectData ports `handlers::gdpr::export_subject_data`.
func (h *Handlers) ExportSubjectData(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.GdprExportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.SubjectID == "" {
		writeJSONErr(w, http.StatusBadRequest, "subject_id is required")
		return
	}
	if !security.CanAccessSubject(claims, body.SubjectID) {
		writeJSONErr(w, http.StatusBadRequest, "session scope does not allow this subject export")
		return
	}
	all, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	events := security.FilterEventsForClaims(all, claims)
	writeJSON(w, http.StatusOK, gdpr.ExportPayload(&body, events))
}
