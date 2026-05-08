// Audit-policy CRUD (create + update + get) — the list path lives in repo.go.

package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

const auditPolicyColumns = `id, name, description, scope, classification,
        retention_days, legal_hold, purge_mode, active, rules, updated_by,
        created_at, updated_at`

// GetAuditPolicy fetches a single audit policy by id (nil + nil error
// when missing).
func (r *Repo) GetAuditPolicy(ctx context.Context, id uuid.UUID) (*models.AuditPolicy, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+auditPolicyColumns+` FROM audit_policies WHERE id = $1`, id)
	v := models.AuditPolicy{}
	if err := row.Scan(&v.ID, &v.Name, &v.Description, &v.Scope, &v.Classification,
		&v.RetentionDays, &v.LegalHold, &v.PurgeMode, &v.Active, &v.Rules,
		&v.UpdatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// CreateAuditPolicy mirrors `handlers::policies::create_policy`.
func (r *Repo) CreateAuditPolicy(ctx context.Context, request *models.CreateAuditPolicyRequest) (*models.AuditPolicy, error) {
	id := uuid.New()
	now := time.Now().UTC()
	rulesJSON, err := models.MarshalRules(request.Rules)
	if err != nil {
		return nil, err
	}
	if _, err := r.Pool.Exec(ctx,
		`INSERT INTO audit_policies (id, name, description, scope, classification,
		       retention_days, legal_hold, purge_mode, active, rules, updated_by,
		       created_at, updated_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13)`,
		id, request.Name, request.Description, request.Scope,
		string(request.Classification), request.RetentionDays, request.LegalHold,
		request.PurgeMode, request.EffectiveActive(), rulesJSON, request.UpdatedBy,
		now, now,
	); err != nil {
		return nil, err
	}
	return r.GetAuditPolicy(ctx, id)
}

// UpdateAuditPolicy mirrors `handlers::policies::update_policy`.
//
// Returns nil + nil error when the policy does not exist (so the
// handler can emit a 404). The Rust impl loads the row, applies the
// optional fields and writes back the merged row.
func (r *Repo) UpdateAuditPolicy(ctx context.Context, id uuid.UUID, request *models.UpdateAuditPolicyRequest) (*models.AuditPolicy, error) {
	current, err := r.GetAuditPolicy(ctx, id)
	if err != nil || current == nil {
		return current, err
	}

	merged := *current
	if request.Name != nil {
		merged.Name = *request.Name
	}
	if request.Description != nil {
		merged.Description = *request.Description
	}
	if request.Scope != nil {
		merged.Scope = *request.Scope
	}
	if request.Classification != nil {
		merged.Classification = string(*request.Classification)
	}
	if request.RetentionDays != nil {
		merged.RetentionDays = *request.RetentionDays
	}
	if request.LegalHold != nil {
		merged.LegalHold = *request.LegalHold
	}
	if request.PurgeMode != nil {
		merged.PurgeMode = *request.PurgeMode
	}
	if request.Active != nil {
		merged.Active = *request.Active
	}
	if request.Rules != nil {
		raw, err := models.MarshalRules(*request.Rules)
		if err != nil {
			return nil, err
		}
		merged.Rules = raw
	}
	if request.UpdatedBy != nil {
		merged.UpdatedBy = *request.UpdatedBy
	}

	now := time.Now().UTC()
	rulesJSON := merged.Rules
	if len(rulesJSON) == 0 {
		rulesJSON = json.RawMessage(`[]`)
	}
	if _, err := r.Pool.Exec(ctx,
		`UPDATE audit_policies
		    SET name = $2, description = $3, scope = $4, classification = $5,
		        retention_days = $6, legal_hold = $7, purge_mode = $8, active = $9,
		        rules = $10::jsonb, updated_by = $11, updated_at = $12
		  WHERE id = $1`,
		id, merged.Name, merged.Description, merged.Scope, merged.Classification,
		merged.RetentionDays, merged.LegalHold, merged.PurgeMode, merged.Active,
		rulesJSON, merged.UpdatedBy, now,
	); err != nil {
		return nil, err
	}
	return r.GetAuditPolicy(ctx, id)
}

// InsertComplianceReport persists a built ComplianceReport. Used by
// the reports handler after `domain/export.BuildReport`.
func (r *Repo) InsertComplianceReport(ctx context.Context, report *models.ComplianceReport) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO compliance_reports
		      (id, standard, title, scope, window_start, window_end, generated_at,
		       status, findings, artifact, relevant_event_count, policy_count,
		       control_summary, expires_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, $11, $12, $13, $14)`,
		report.ID, report.Standard, report.Title, report.Scope,
		report.WindowStart, report.WindowEnd, report.GeneratedAt, report.Status,
		report.Findings, report.Artifact, report.RelevantEventCount,
		report.PolicyCount, report.ControlSummary, report.ExpiresAt,
	)
	return err
}
