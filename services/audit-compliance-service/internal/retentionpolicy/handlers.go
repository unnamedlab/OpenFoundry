// Retention-policy HTTP handlers — mirrors `handlers/retention.rs`.

package retentionpolicy

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
	policies, err := LoadPolicies(r.Context(), h.Pool)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := parseListQuery(r)
	writeJSON(w, http.StatusOK, FilterPolicies(policies, &q))
}

// CreatePolicyHandler ports `handlers::retention::create_policy`.
func (h *Handlers) CreatePolicyHandler(w http.ResponseWriter, r *http.Request) {
	var body CreateRetentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	policy, err := CreatePolicy(r.Context(), h.Pool, &body)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errors.New("name is required")) ||
			strings.Contains(err.Error(), "is required") {
			status = http.StatusBadRequest
		}
		writeJSONErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

// UpdatePolicyHandler ports `handlers::retention::update_policy`.
func (h *Handlers) UpdatePolicyHandler(w http.ResponseWriter, r *http.Request) {
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
	policy, err := UpdatePolicy(r.Context(), h.Pool, id, &body)
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

// GetPolicyHandler ports `handlers::retention::get_policy`.
func (h *Handlers) GetPolicyHandler(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	policy, err := LoadPolicy(r.Context(), h.Pool, id)
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
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := DeletePolicy(r.Context(), h.Pool, id); err != nil {
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

// ListJobs ports `handlers::retention::list_jobs`.
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, policy_id, target_dataset_id, target_transaction_id, status,
		        action_summary, affected_record_count, created_at, completed_at
		   FROM retention_jobs
		  ORDER BY created_at DESC`)
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
func (h *Handlers) RunJobHandler(w http.ResponseWriter, r *http.Request) {
	var body models.RunRetentionJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := RunJob(r.Context(), h.Pool, &body)
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
	datasetID, err := uuid.Parse(chi.URLParam(r, "dataset_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid dataset_id")
		return
	}
	policies, err := LoadPolicies(r.Context(), h.Pool)
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
	txID, err := uuid.Parse(chi.URLParam(r, "transaction_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid transaction_id")
		return
	}
	policies, err := LoadPolicies(r.Context(), h.Pool)
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
	rid := chi.URLParam(r, "rid")
	policies, err := LoadPolicies(r.Context(), h.Pool)
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
	rid := chi.URLParam(r, "rid")
	policies, err := LoadPolicies(r.Context(), h.Pool)
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
