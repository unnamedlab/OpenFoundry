// Package handlers wires the HTTP endpoints for authorization-policy-service.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/repo"
)

// CedarPolicyValidator captures the narrow surface the handlers need
// from libs/authz-cedar-go to validate a policy source against the
// bundled schema before persisting.
//
// Decoupled via interface so tests can drive the handlers without
// spinning up a full PolicyStore.
type CedarPolicyValidator interface {
	ReplacePolicies(records []cedarauthz.PolicyRecord) error
}

// validatorFactory is the constructor used to obtain a fresh validator
// per write request. Defaults to a brand-new in-memory PolicyStore so
// validation is hermetic — a buggy in-flight write can't corrupt the
// active validator state.
type validatorFactory func() (CedarPolicyValidator, error)

func defaultValidatorFactory() (CedarPolicyValidator, error) {
	return cedarauthz.NewEmpty()
}

// Handlers groups every endpoint so server.go has a single dependency.
//
// Optional: when NATS is non-nil, every successful CRUD write publishes
// `authz.policy.changed` so subscribers (other services running
// libs/authz-cedar-go's PolicyReloadSubscriber) re-pull immediately.
type Handlers struct {
	Repo            *repo.Repo
	NATS            *nats.Conn
	ReloadSubject   string
	ValidateFactory validatorFactory
}

// NewHandlers builds a Handlers wired with sensible defaults.
func NewHandlers(r *repo.Repo, nc *nats.Conn) *Handlers {
	return &Handlers{
		Repo:            r,
		NATS:            nc,
		ReloadSubject:   cedarauthz.DefaultReloadSubject,
		ValidateFactory: defaultValidatorFactory,
	}
}

// ─── Cedar policies CRUD ────────────────────────────────────────────

// ListCedarPolicies handles GET /api/v1/cedar-policies. Scoped to the
// caller's tenant (claims.OrgID): tenant callers see their own rows
// plus platform-global rows; platform admins (no OrgID claim) see only
// the global rows.
func (h *Handlers) ListCedarPolicies(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListCedarPolicies(r.Context(), caller.OrgID)
	if err != nil {
		slog.Error("list cedar policies", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list policies")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.CedarPolicy]{Items: items})
}

// GetCedarPolicy handles GET /api/v1/cedar-policies/{id}. Scoped to
// the caller's tenant — see [ListCedarPolicies] for the rules.
func (h *Handlers) GetCedarPolicy(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "id required")
		return
	}
	p, err := h.Repo.GetCedarPolicy(r.Context(), caller.OrgID, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeJSONErr(w, http.StatusNotFound, "policy not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// CreateCedarPolicy handles POST /api/v1/cedar-policies.
//
// Validation gate: the policy source MUST parse + validate against the
// bundled Cedar schema in strict mode before persistence. We delegate
// to a fresh in-memory PolicyStore via [validatorFactory] so a bad
// source can't poison the active validator state.
func (h *Handlers) CreateCedarPolicy(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateCedarPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	if body.ID == "" {
		writeJSONErr(w, http.StatusBadRequest, "id required")
		return
	}
	if strings.TrimSpace(body.Source) == "" {
		writeJSONErr(w, http.StatusBadRequest, "source required")
		return
	}
	if err := h.validateSource(body.ID, body.Source); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Tenant is sealed from JWT claims — body cannot override. nil OrgID
	// means a platform/admin caller writing a global row.
	p, err := h.Repo.CreateCedarPolicy(r.Context(), &body, caller.Sub, caller.OrgID)
	if err != nil {
		slog.Error("create cedar policy", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.publishReload(r.Context(), p.ID)
	writeJSON(w, http.StatusCreated, p)
}

// UpdateCedarPolicy handles PATCH /api/v1/cedar-policies/{id}.
//
// Bumping `source` re-validates against the schema and increments the
// version. Toggling `active` does NOT bump the version — it's a state
// flip, not a source change. Writes are confined to rows owned by the
// caller's tenant (or globals when no OrgID claim is present).
func (h *Handlers) UpdateCedarPolicy(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "id required")
		return
	}
	var body models.UpdateCedarPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Source != nil {
		if strings.TrimSpace(*body.Source) == "" {
			writeJSONErr(w, http.StatusBadRequest, "source must not be blank")
			return
		}
		if err := h.validateSource(id, *body.Source); err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	p, err := h.Repo.UpdateCedarPolicy(r.Context(), caller.OrgID, id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		// 404 (not 403) so the absence of another tenant's row is
		// indistinguishable from a missing id — cross-tenant probing
		// returns no signal.
		writeJSONErr(w, http.StatusNotFound, "policy not found")
		return
	}
	h.publishReload(r.Context(), p.ID)
	writeJSON(w, http.StatusOK, p)
}

// DeleteCedarPolicy handles DELETE /api/v1/cedar-policies/{id}. Scoped
// to rows owned by the caller's tenant.
func (h *Handlers) DeleteCedarPolicy(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "id required")
		return
	}
	deleted, err := h.Repo.DeleteCedarPolicy(r.Context(), caller.OrgID, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "policy not found")
		return
	}
	h.publishReload(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ────────────────────────────────────────────────────────

// validateSource builds a fresh in-memory PolicyStore and ReplacePolicies
// against the bundled schema. Surfaces a clean message via the typed
// PolicyParseError / ValidationError variants so the client sees what
// went wrong without engine internals.
func (h *Handlers) validateSource(id, source string) error {
	factory := h.ValidateFactory
	if factory == nil {
		factory = defaultValidatorFactory
	}
	v, err := factory()
	if err != nil {
		return err
	}
	if err := v.ReplacePolicies([]cedarauthz.PolicyRecord{{
		ID:     id,
		Source: source,
	}}); err != nil {
		var ppe *cedarauthz.PolicyParseError
		if errors.As(err, &ppe) {
			return ppe
		}
		return err
	}
	return nil
}

// publishReload signals subscribers (other services) that the active
// policy bundle changed. Best-effort — failures are logged and never
// surface to the caller; the row is already persisted by the time we
// publish.
func (h *Handlers) publishReload(_ context.Context, policyID string) {
	if h.NATS == nil {
		return
	}
	subj := h.ReloadSubject
	if subj == "" {
		subj = cedarauthz.DefaultReloadSubject
	}
	if err := h.NATS.Publish(subj, []byte(policyID)); err != nil {
		slog.Warn("authz: reload publish failed",
			slog.String("subject", subj),
			slog.String("policy_id", policyID),
			slog.String("error", err.Error()),
		)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
