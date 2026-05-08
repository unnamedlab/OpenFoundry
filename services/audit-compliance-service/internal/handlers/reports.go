// Compliance-report handlers — mirrors `handlers/reports.rs`.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/export"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/security"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// GenerateReport ports `handlers::reports::generate_report`.
func (h *Handlers) GenerateReport(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.ComplianceReportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	all, err := h.Repo.ListAuditEvents(r.Context(), 1000)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	events := security.FilterEventsForClaims(all, claims)
	report, err := export.BuildReport(&body, events, nil)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.Repo.InsertComplianceReport(r.Context(), &report); err != nil {
		slog.Error("insert compliance report", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, report)
}
