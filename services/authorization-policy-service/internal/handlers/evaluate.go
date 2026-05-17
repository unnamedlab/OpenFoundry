package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/domain"
)

// EvaluatePolicyRequest is the body of POST /api/v1/policy-evaluations.
type EvaluatePolicyRequest struct {
	Resource           string          `json:"resource"`
	Action             string          `json:"action"`
	ResourceAttributes json.RawMessage `json:"resource_attributes,omitempty"`
}

// EvaluatePolicy handles POST /api/v1/policy-evaluations.
//
// Returns the legacy ABAC EvaluationResult. Cedar evaluation lives
// in libs/authz-cedar-go and is wired by services that need
// write-path gating; this endpoint is for row filtering +
// restricted-view scoping.
func (h *Handlers) EvaluatePolicy(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tenantID, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusForbidden, "tenant scope required")
		return
	}
	var body EvaluatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Resource == "" || body.Action == "" {
		writeJSONErr(w, http.StatusBadRequest, "resource and action required")
		return
	}
	out, err := domain.Evaluate(r.Context(), h.Repo, caller, tenantID,
		body.Resource, body.Action, body.ResourceAttributes)
	if err != nil {
		slog.Error("evaluate policy", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "evaluation failed")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
