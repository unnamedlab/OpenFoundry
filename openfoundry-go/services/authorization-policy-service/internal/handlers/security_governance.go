package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── Governance template applications ─────────────────────────────

func (h *Handlers) ListGovernanceTemplateApplications(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListGovernanceTemplateApplications(r.Context())
	if err != nil {
		slog.Error("list governance template applications", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list applications")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.GovernanceTemplateApplication]{Items: items})
}

func (h *Handlers) ApplyGovernanceTemplate(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.ApplyGovernanceTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.TemplateSlug == "" || body.TemplateName == "" || body.Scope == "" || body.DefaultReportStandard == "" {
		writeJSONErr(w, http.StatusBadRequest, "template_slug, template_name, scope, default_report_standard required")
		return
	}
	v, err := h.Repo.ApplyGovernanceTemplate(r.Context(), &body, caller.Sub.String())
	if err != nil {
		slog.Error("apply governance template", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) DeleteGovernanceTemplateApplication(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := h.Repo.DeleteGovernanceTemplateApplication(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "application not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Project constraints ──────────────────────────────────────────

func (h *Handlers) ListProjectConstraints(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListProjectConstraints(r.Context())
	if err != nil {
		slog.Error("list project constraints", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list constraints")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ProjectConstraint]{Items: items})
}

func (h *Handlers) CreateProjectConstraint(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateProjectConstraintRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" || body.Scope == "" || body.ResourceType == "" {
		writeJSONErr(w, http.StatusBadRequest, "name, scope, resource_type required")
		return
	}
	v, err := h.Repo.CreateProjectConstraint(r.Context(), &body, caller.Sub.String())
	if err != nil {
		slog.Error("create project constraint", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) UpdateProjectConstraint(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.UpdateProjectConstraintRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Repo.UpdateProjectConstraint(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "constraint not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) DeleteProjectConstraint(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := h.Repo.DeleteProjectConstraint(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "constraint not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Structural security rules ────────────────────────────────────

func (h *Handlers) ListStructuralSecurityRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListStructuralSecurityRules(r.Context())
	if err != nil {
		slog.Error("list structural security rules", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.StructuralSecurityRule]{Items: items})
}

func (h *Handlers) CreateStructuralSecurityRule(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateStructuralSecurityRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" || body.ResourceType == "" || body.ConditionKind == "" {
		writeJSONErr(w, http.StatusBadRequest, "name, resource_type, condition_kind required")
		return
	}
	v, err := h.Repo.CreateStructuralSecurityRule(r.Context(), &body, caller.Sub.String())
	if err != nil {
		slog.Error("create structural security rule", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) UpdateStructuralSecurityRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.UpdateStructuralSecurityRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Repo.UpdateStructuralSecurityRule(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "rule not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) DeleteStructuralSecurityRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := h.Repo.DeleteStructuralSecurityRule(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
