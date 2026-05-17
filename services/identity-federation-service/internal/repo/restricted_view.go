package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// ListRestrictedViews returns the most recent 200 entries owned by tenantID.
func (r *Repo) ListRestrictedViews(ctx context.Context, tenantID uuid.UUID) ([]models.RestrictedView, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, tenant_id, name, description, resource, action, conditions, row_filter,
		        hidden_columns, marking_columns, allowed_org_ids, allowed_markings,
		        consumer_mode_enabled, allow_guest_access, enabled,
		        created_by, created_at, updated_at
		 FROM restricted_views
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC LIMIT 200`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.RestrictedView, 0)
	for rows.Next() {
		v, err := scanRestrictedView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// GetRestrictedView returns the row by id, or nil when the row is absent
// or owned by a different tenant.
func (r *Repo) GetRestrictedView(ctx context.Context, id, tenantID uuid.UUID) (*models.RestrictedView, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, description, resource, action, conditions, row_filter,
		        hidden_columns, marking_columns, allowed_org_ids, allowed_markings,
		        consumer_mode_enabled, allow_guest_access, enabled,
		        created_by, created_at, updated_at
		 FROM restricted_views
		 WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	v, err := scanRestrictedView(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// CreateRestrictedView inserts a row owned by tenantID, returning the
// persisted version. `creator` is the JWT subject of the admin who
// issued the call.
func (r *Repo) CreateRestrictedView(ctx context.Context, body *models.CreateRestrictedViewRequest, creator, tenantID uuid.UUID) (*models.RestrictedView, error) {
	id := uuid.New()
	conditions := defaultJSON(body.Conditions, "{}")
	hidden := defaultJSON(body.HiddenColumns, "[]")
	markingColumns := defaultJSON(body.MarkingColumns, "[]")
	orgIDs := defaultJSON(body.AllowedOrgIDs, "[]")
	markings := defaultJSON(body.AllowedMarkings, "[]")
	consumerMode := derefBool(body.ConsumerModeEnabled, false)
	allowGuest := derefBool(body.AllowGuestAccess, false)
	enabled := derefBool(body.Enabled, true)

	_, err := r.Pool.Exec(ctx,
		`INSERT INTO restricted_views (
		    id, tenant_id, name, description, resource, action, conditions, row_filter,
		    hidden_columns, marking_columns, allowed_org_ids, allowed_markings,
		    consumer_mode_enabled, allow_guest_access, enabled, created_by
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		id, tenantID, body.Name, body.Description, body.Resource, body.Action,
		conditions, body.RowFilter, hidden, markingColumns, orgIDs, markings,
		consumerMode, allowGuest, enabled, creator,
	)
	if err != nil {
		return nil, fmt.Errorf("insert restricted view: %w", err)
	}
	return r.GetRestrictedView(ctx, id, tenantID)
}

// UpdateRestrictedView applies non-nil fields to the row owned by
// tenantID. Returns nil when the row is missing or owned by another
// tenant — same shape as a 404 from the handler. The owning
// tenant_id is intentionally immutable: a view cannot be transferred
// across tenants via the public API.
func (r *Repo) UpdateRestrictedView(ctx context.Context, id, tenantID uuid.UUID, body *models.UpdateRestrictedViewRequest) (*models.RestrictedView, error) {
	current, err := r.GetRestrictedView(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	merged := *current
	if body.Name != nil {
		merged.Name = *body.Name
	}
	if body.Description != nil {
		merged.Description = body.Description
	}
	if body.Resource != nil {
		merged.Resource = *body.Resource
	}
	if body.Action != nil {
		merged.Action = *body.Action
	}
	if len(body.Conditions) > 0 {
		merged.Conditions = body.Conditions
	}
	if body.RowFilter != nil {
		merged.RowFilter = body.RowFilter
	}
	if len(body.HiddenColumns) > 0 {
		merged.HiddenColumns = body.HiddenColumns
	}
	if len(body.MarkingColumns) > 0 {
		merged.MarkingColumns = body.MarkingColumns
	}
	if len(body.AllowedOrgIDs) > 0 {
		merged.AllowedOrgIDs = body.AllowedOrgIDs
	}
	if len(body.AllowedMarkings) > 0 {
		merged.AllowedMarkings = body.AllowedMarkings
	}
	if body.ConsumerModeEnabled != nil {
		merged.ConsumerModeEnabled = *body.ConsumerModeEnabled
	}
	if body.AllowGuestAccess != nil {
		merged.AllowGuestAccess = *body.AllowGuestAccess
	}
	if body.Enabled != nil {
		merged.Enabled = *body.Enabled
	}

	_, err = r.Pool.Exec(ctx,
		`UPDATE restricted_views SET
		    name=$3, description=$4, resource=$5, action=$6, conditions=$7, row_filter=$8,
		    hidden_columns=$9, marking_columns=$10, allowed_org_ids=$11, allowed_markings=$12,
		    consumer_mode_enabled=$13, allow_guest_access=$14, enabled=$15,
		    updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2`,
		id, tenantID, merged.Name, merged.Description, merged.Resource, merged.Action,
		merged.Conditions, merged.RowFilter, merged.HiddenColumns,
		merged.MarkingColumns, merged.AllowedOrgIDs, merged.AllowedMarkings,
		merged.ConsumerModeEnabled, merged.AllowGuestAccess, merged.Enabled,
	)
	if err != nil {
		return nil, fmt.Errorf("update restricted view: %w", err)
	}
	return r.GetRestrictedView(ctx, id, tenantID)
}

// DeleteRestrictedView removes a row owned by tenantID. Cross-tenant
// deletes are silent no-ops.
func (r *Repo) DeleteRestrictedView(ctx context.Context, id, tenantID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM restricted_views WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return err
}

func scanRestrictedView(r rowLikeRV) (*models.RestrictedView, error) {
	v := &models.RestrictedView{}
	err := r.Scan(
		&v.ID, &v.TenantID, &v.Name, &v.Description, &v.Resource, &v.Action,
		&v.Conditions, &v.RowFilter, &v.HiddenColumns,
		&v.MarkingColumns, &v.AllowedOrgIDs, &v.AllowedMarkings,
		&v.ConsumerModeEnabled, &v.AllowGuestAccess, &v.Enabled,
		&v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return v, nil
}

type rowLikeRV interface {
	Scan(...any) error
}

func defaultJSON(b json.RawMessage, fallback string) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(fallback)
	}
	return b
}

func derefBool(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}
