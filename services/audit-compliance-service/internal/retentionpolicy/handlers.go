// Retention-policy HTTP handlers — mirrors `handlers/retention.rs`.

package retentionpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// Handlers wires the retention-policy HTTP endpoints.
type Handlers struct {
	Pool *pgxpool.Pool
}

// New wires a Handlers bound to the given pool.
func New(pool *pgxpool.Pool) *Handlers { return &Handlers{Pool: pool} }

// ListPolicies ports `handlers::retention::list_policies`.
func (h *Handlers) ListPolicies(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	policies, err := LoadPolicies(r.Context(), h.Pool, scope)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := parseListQuery(r)
	writeJSON(w, http.StatusOK, FilterPolicies(policies, &q))
}

// CreatePolicyHandler ports `handlers::retention::create_policy`.
func (h *Handlers) CreatePolicyHandler(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	var body CreateRetentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	orgID := resolveWriteOrgID(scope)
	policy, err := CreatePolicy(r.Context(), h.Pool, &body, orgID)
	if err != nil {
		writeRetentionPolicyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

// UpdatePolicyHandler ports `handlers::retention::update_policy`.
func (h *Handlers) UpdatePolicyHandler(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body UpdateRetentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	policy, err := UpdatePolicy(r.Context(), h.Pool, id, &body, scope)
	if err != nil {
		writeRetentionPolicyErr(w, err)
		return
	}
	if policy == nil {
		writeJSONErr(w, http.StatusNotFound, "retention policy not found")
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

// GetPolicyHandler ports `handlers::retention::get_policy`.
func (h *Handlers) GetPolicyHandler(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	policy, err := LoadPolicy(r.Context(), h.Pool, id, scope)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if policy == nil {
		writeJSONErr(w, http.StatusNotFound, "retention policy not found")
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

// DeletePolicyHandler ports `handlers::retention::delete_policy`.
//
// System policies (is_system=true) emit a 409 Conflict mirroring the
// Rust impl; missing rows emit 404.
func (h *Handlers) DeletePolicyHandler(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := DeletePolicy(r.Context(), h.Pool, id, scope); err != nil {
		switch {
		case errors.Is(err, ErrPolicyNotFound):
			writeJSONErr(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrPolicyIsSystem):
			writeJSONErr(w, http.StatusConflict, err.Error())
		default:
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListJobs ports `handlers::retention::list_jobs`. Jobs are joined to
// `retention_policies` so the tenant filter cascades transparently;
// admins see every job.
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	sql := `SELECT j.id, j.policy_id, j.target_dataset_id, j.target_transaction_id,
		        j.status, j.action_summary, j.affected_record_count,
		        j.created_at, j.completed_at
		   FROM retention_jobs j
		   JOIN retention_policies p ON p.id = j.policy_id`
	var args []any
	if !scope.AllOrgs {
		args = append(args, scope.OrgID)
		sql += " WHERE (p.org_id = $1 OR p.is_system = TRUE)"
	}
	sql += " ORDER BY j.created_at DESC"
	rows, err := h.Pool.Query(r.Context(), sql, args...)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := make([]models.RetentionJob, 0)
	for rows.Next() {
		var v models.RetentionJob
		if err := rows.Scan(&v.ID, &v.PolicyID, &v.TargetDatasetID,
			&v.TargetTransactionID, &v.Status, &v.ActionSummary,
			&v.AffectedRecordCount, &v.CreatedAt, &v.CompletedAt); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// RunJobHandler ports `handlers::retention::run_job`.
func (h *Handlers) RunExecutionHandler(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	var body models.RunRetentionExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	body.DatasetRid = strings.TrimSpace(body.DatasetRid)
	if body.DatasetRid == "" {
		writeJSONErr(w, http.StatusBadRequest, "dataset_rid is required")
		return
	}
	run, err := RunExecution(r.Context(), h.Pool, &body, scope, claims)
	if err != nil {
		slog.Error("retention execution", slog.String("summary", func() string {
			if run != nil {
				return executionSummary(run)
			}
			return ""
		}()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handlers) ListExecutions(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	sql := `SELECT id, org_id, dataset_rid, status, dry_run, marked_transaction_count, swept_transaction_count, delete_transaction_count, recovery_window_days, remediation_deadline, irreversible_after, warnings, created_by, created_at, completed_at FROM retention_execution_runs`
	args := []any{}
	if !scope.AllOrgs {
		args = append(args, scope.OrgID)
		sql += " WHERE org_id = $1 OR org_id IS NULL"
	}
	sql += " ORDER BY created_at DESC LIMIT 100"
	rows, err := h.Pool.Query(r.Context(), sql, args...)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []models.RetentionExecutionRun{}
	for rows.Next() {
		var run models.RetentionExecutionRun
		var warnings json.RawMessage
		if err := rows.Scan(&run.ID, &run.OrgID, &run.DatasetRid, &run.Status, &run.DryRun, &run.MarkedTransactionCount, &run.SweptTransactionCount, &run.DeleteTransactionCount, &run.RecoveryWindowDays, &run.RemediationDeadline, &run.IrreversibleAfter, &warnings, &run.CreatedBy, &run.CreatedAt, &run.CompletedAt); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.Unmarshal(warnings, &run.Warnings)
		run.Items = []models.RetentionExecutionItem{}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for idx := range out {
		items, err := loadExecutionItems(r.Context(), h.Pool, out[idx].ID)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out[idx].Items = items
	}
	writeJSON(w, http.StatusOK, out)
}

func loadExecutionItems(ctx context.Context, db *pgxpool.Pool, runID uuid.UUID) ([]models.RetentionExecutionItem, error) {
	rows, err := db.Query(ctx, `SELECT id, run_id, policy_id, transaction_id, action, reason, marked_at, recoverable_until, swept_at, requires_delete_transaction FROM retention_execution_items WHERE run_id = $1 ORDER BY marked_at NULLS LAST`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.RetentionExecutionItem{}
	for rows.Next() {
		var item models.RetentionExecutionItem
		if err := rows.Scan(&item.ID, &item.RunID, &item.PolicyID, &item.TransactionID, &item.Action, &item.Reason, &item.MarkedAt, &item.RecoverableUntil, &item.SweptAt, &item.RequiresDeleteTransaction); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (h *Handlers) RunJobHandler(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	var body models.RunRetentionJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := RunJob(r.Context(), h.Pool, &body, scope)
	if err != nil {
		switch {
		case errors.Is(err, ErrPolicyNotFound):
			writeJSONErr(w, http.StatusNotFound, err.Error())
		default:
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// GetDatasetRetention ports `handlers::retention::get_dataset_retention`.
func (h *Handlers) GetDatasetRetention(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	datasetID, err := uuid.Parse(chi.URLParam(r, "dataset_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid dataset_id")
		return
	}
	policies, err := LoadPolicies(r.Context(), h.Pool, scope)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	filtered := make([]models.RetentionPolicy, 0, len(policies))
	for i := range policies {
		if policies[i].TargetKind == "dataset" || strings.Contains(policies[i].Scope, "dataset") {
			filtered = append(filtered, policies[i])
		}
	}
	writeJSON(w, http.StatusOK, models.DatasetRetentionView{
		DatasetID: datasetID,
		Policies:  filtered,
	})
}

// GetTransactionRetention ports `handlers::retention::get_transaction_retention`.
func (h *Handlers) GetTransactionRetention(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	txID, err := uuid.Parse(chi.URLParam(r, "transaction_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid transaction_id")
		return
	}
	policies, err := LoadPolicies(r.Context(), h.Pool, scope)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	filtered := make([]models.RetentionPolicy, 0, len(policies))
	for i := range policies {
		if policies[i].TargetKind == "transaction" || strings.Contains(policies[i].Scope, "transaction") {
			filtered = append(filtered, policies[i])
		}
	}
	writeJSON(w, http.StatusOK, models.TransactionRetentionView{
		TransactionID: txID,
		Policies:      filtered,
	})
}

// ApplicablePolicies ports `handlers::retention::applicable_policies`.
func (h *Handlers) ApplicablePolicies(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	rid := chi.URLParam(r, "rid")
	policies, err := LoadPolicies(r.Context(), h.Pool, scope)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx := parseResolutionContext(r)
	resolved := ResolveApplicable(policies, rid, &ctx)
	writeJSON(w, http.StatusOK, models.ApplicablePoliciesResponse{
		DatasetRid: rid,
		Context:    ctx,
		Inherited:  resolved.Inherited,
		Explicit:   resolved.Explicit,
		Effective:  resolved.Effective,
		Conflicts:  resolved.Conflicts,
	})
}

// RetentionPreviewHandler ports `handlers::retention::retention_preview`.
func (h *Handlers) RetentionPreviewHandler(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveOrgScope(w, r)
	if !ok {
		return
	}
	rid := chi.URLParam(r, "rid")
	policies, err := LoadPolicies(r.Context(), h.Pool, scope)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := parsePreviewQuery(r)
	asOfDays := int64(0)
	if q.AsOfDays != nil {
		asOfDays = *q.AsOfDays
		if asOfDays < 0 {
			asOfDays = 0
		}
	}
	ctx := models.ResolutionContext{
		ProjectID: q.ProjectID,
		MarkingID: q.MarkingID,
		SpaceID:   q.SpaceID,
		OrgID:     q.OrgID,
	}
	resolved := ResolveApplicable(policies, rid, &ctx)
	preview, err := RunPreview(r.Context(), h.Pool, rid, asOfDays, &resolved)
	if err != nil {
		slog.Error("retention preview", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// resolveOrgScope resolves the caller's tenant boundary from the
// authenticated claims. Non-admin callers are pinned to their own
// `claims.OrgID`; the `?org_id=…` query parameter is intentionally
// ignored on this path so a tenant cannot escape isolation by
// rewriting the URL.
//
// Admin callers (RoleAdmin or the `retention-policies:admin`
// permission) may pass `?org_id=<uuid>` to scope to a specific tenant
// or `?all_orgs=true` to drop the filter entirely.
//
// Errors are written directly to `w`. The second return is false when
// the request was rejected and the handler must abort.
func resolveOrgScope(w http.ResponseWriter, r *http.Request) (OrgScope, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing authentication")
		return OrgScope{}, false
	}
	admin := isRetentionAdmin(claims)
	requestedOrg, requestedOrgPresent, err := parseOrgIDQuery(r)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid org_id")
		return OrgScope{}, false
	}
	if !admin {
		if requestedOrgPresent && (claims.OrgID == nil || *requestedOrg != *claims.OrgID) {
			writeJSONErr(w, http.StatusForbidden, "cross-org access requires retention-policies:admin")
			return OrgScope{}, false
		}
		if claims.OrgID == nil {
			writeJSONErr(w, http.StatusForbidden, "tenant context required")
			return OrgScope{}, false
		}
		return OrgScope{OrgID: claims.OrgID}, true
	}
	if requestedOrgPresent {
		return OrgScope{OrgID: requestedOrg}, true
	}
	if allOrgs, _ := strconv.ParseBool(r.URL.Query().Get("all_orgs")); allOrgs {
		return OrgScope{AllOrgs: true}, true
	}
	// Admin without an explicit scope hint defaults to their own org
	// if they carry one — least privilege for routine admin reads —
	// and falls back to cross-org for platform operators that lack a
	// concrete org.
	if claims.OrgID != nil {
		return OrgScope{OrgID: claims.OrgID}, true
	}
	return OrgScope{AllOrgs: true}, true
}

// resolveWriteOrgID returns the org_id a Create call should persist.
// For non-admins this is scope.OrgID; for admins with AllOrgs=true the
// row is created globally (org_id NULL).
func resolveWriteOrgID(scope OrgScope) *uuid.UUID {
	if scope.AllOrgs {
		return nil
	}
	return scope.OrgID
}

// isRetentionAdmin reports whether the claims grant cross-org access
// to retention policies. Either RoleAdmin or the explicit
// `retention-policies:admin` permission qualifies.
func isRetentionAdmin(c *authmw.Claims) bool {
	if c.HasRole(authmw.RoleAdmin) {
		return true
	}
	return c.HasPermissionKey("retention-policies:admin")
}

func parseOrgIDQuery(r *http.Request) (*uuid.UUID, bool, error) {
	raw := r.URL.Query().Get("org_id")
	if raw == "" {
		return nil, false, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, true, err
	}
	return &id, true, nil
}

func parseListQuery(r *http.Request) models.ListRetentionPoliciesQuery {
	q := models.ListRetentionPoliciesQuery{}
	if v := r.URL.Query().Get("dataset_rid"); v != "" {
		q.DatasetRid = &v
	}
	if v := r.URL.Query().Get("project_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.ProjectID = &id
		}
	}
	if v := r.URL.Query().Get("marking_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.MarkingID = &id
		}
	}
	if v := r.URL.Query().Get("space_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.SpaceID = &id
		}
	}
	if v := r.URL.Query().Get("active"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			q.Active = &b
		}
	}
	if v := r.URL.Query().Get("system_only"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			q.SystemOnly = &b
		}
	}
	return q
}

func parseResolutionContext(r *http.Request) models.ResolutionContext {
	q := models.ResolutionContext{}
	if v := r.URL.Query().Get("project_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.ProjectID = &id
		}
	}
	if v := r.URL.Query().Get("marking_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.MarkingID = &id
		}
	}
	if v := r.URL.Query().Get("space_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.SpaceID = &id
		}
	}
	if v := r.URL.Query().Get("org_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.OrgID = &id
		}
	}
	return q
}

func parsePreviewQuery(r *http.Request) models.RetentionPreviewQuery {
	q := models.RetentionPreviewQuery{}
	if v := r.URL.Query().Get("as_of_days"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.AsOfDays = &n
		}
	}
	if v := r.URL.Query().Get("project_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.ProjectID = &id
		}
	}
	if v := r.URL.Query().Get("marking_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.MarkingID = &id
		}
	}
	if v := r.URL.Query().Get("space_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.SpaceID = &id
		}
	}
	if v := r.URL.Query().Get("org_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.OrgID = &id
		}
	}
	return q
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeRetentionPolicyErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPolicyManagedByPlatform):
		writeJSONErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrMaxCustomPoliciesPerSpace):
		writeJSONErr(w, http.StatusConflict, err.Error())
	case strings.Contains(err.Error(), "required"),
		strings.Contains(err.Error(), "must be"),
		strings.Contains(err.Error(), "not supported"),
		strings.Contains(err.Error(), "at most"):
		writeJSONErr(w, http.StatusBadRequest, err.Error())
	default:
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
	}
}
