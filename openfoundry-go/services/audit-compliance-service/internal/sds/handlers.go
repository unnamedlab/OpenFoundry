// SDS HTTP handlers — mirrors `services/audit-compliance-service/src/sds/handlers/sds.rs`.

package sds

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// Handlers wires the SDS HTTP endpoints.
type Handlers struct {
	Pool *pgxpool.Pool
}

// New wires the SDS handler set.
func New(pool *pgxpool.Pool) *Handlers { return &Handlers{Pool: pool} }

// ScanSensitiveData ports `handlers::sds::scan_sensitive_data` —
// scan-only, no DB write.
func (h *Handlers) ScanSensitiveData(w http.ResponseWriter, r *http.Request) {
	var body models.SensitiveDataScanRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Content == "" {
		writeJSONErr(w, http.StatusBadRequest, "content is required")
		return
	}
	writeJSON(w, http.StatusOK, Scan(&body))
}

// RunScanJob ports `handlers::sds::run_scan_job` — persists a job +
// per-finding issues.
func (h *Handlers) RunScanJob(w http.ResponseWriter, r *http.Request) {
	var body models.RunSensitiveDataScanRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.TargetName == "" {
		writeJSONErr(w, http.StatusBadRequest, "target_name is required")
		return
	}
	if body.Content == "" {
		writeJSONErr(w, http.StatusBadRequest, "content is required")
		return
	}
	job, err := CreateScanJob(r.Context(), h.Pool, &body)
	if err != nil {
		slog.Error("create SDS scan job", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// MarkIssue ports `handlers::sds::mark_issue`.
func (h *Handlers) MarkIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "issue_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid issue_id")
		return
	}
	var body models.MarkSensitiveIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	issue, err := loadIssue(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if issue == nil {
		writeJSONErr(w, http.StatusNotFound, "issue not found")
		return
	}
	markings, remediations, status, err := ApplyMarkings(issue, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	row := h.Pool.QueryRow(r.Context(),
		`UPDATE sds_issues
		    SET status = $2, markings = $3::jsonb, remediation_actions = $4::jsonb,
		        updated_at = NOW()
		  WHERE id = $1
		  RETURNING id, job_id, kind, severity, status, matched_value, redacted_value,
		            match_count, markings, remediation_actions, created_at, updated_at`,
		id, status, markings, remediations,
	)
	updated := models.SDSIssue{}
	if err := row.Scan(&updated.ID, &updated.JobID, &updated.Kind, &updated.Severity,
		&updated.Status, &updated.MatchedValue, &updated.RedactedValue,
		&updated.MatchCount, &updated.Markings, &updated.RemediationActions,
		&updated.CreatedAt, &updated.UpdatedAt); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// CreateRemediationRule ports `handlers::sds::create_remediation_rule`.
func (h *Handlers) CreateRemediationRule(w http.ResponseWriter, r *http.Request) {
	var body models.CreateRemediationRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Scope == "" {
		writeJSONErr(w, http.StatusBadRequest, "scope is required")
		return
	}
	matchConditions, remediations, err := RulePayload(&body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := uuid.New()
	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO sds_remediation_rules (id, name, scope, match_conditions,
		       remediation_actions, updated_by, created_at, updated_at)
		    VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, NOW(), NOW())
		    RETURNING id, name, scope, match_conditions, remediation_actions,
		              updated_by, created_at, updated_at`,
		id, body.Name, body.Scope, matchConditions, remediations, body.UpdatedBy,
	)
	rule := models.SDSRemediationRule{}
	if err := row.Scan(&rule.ID, &rule.Name, &rule.Scope, &rule.MatchConditions,
		&rule.RemediationActions, &rule.UpdatedBy, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func loadIssue(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*models.SDSIssue, error) {
	row := db.QueryRow(ctx,
		`SELECT id, job_id, kind, severity, status, matched_value, redacted_value,
		        match_count, markings, remediation_actions, created_at, updated_at
		   FROM sds_issues WHERE id = $1`, id)
	v := models.SDSIssue{}
	if err := row.Scan(&v.ID, &v.JobID, &v.Kind, &v.Severity, &v.Status,
		&v.MatchedValue, &v.RedactedValue, &v.MatchCount, &v.Markings,
		&v.RemediationActions, &v.CreatedAt, &v.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = time.Now // keep time import alive if helpers ever need it
