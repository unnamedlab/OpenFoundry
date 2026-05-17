package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

var ErrAuditDeliveryDestinationInvalid = errors.New("audit delivery destination invalid")

const auditDeliveryDestinationSelect = `SELECT id, organization_id, name,
	destination_type, schema_version, endpoint_url, dataset_rid, enabled,
	validation_status, validation_message, last_validated_at,
	last_backfill_status, last_backfill_started_at, last_backfill_completed_at,
	metadata, created_by, created_at, updated_at
	FROM audit_delivery_destinations`

func (r *Repo) ListAuditDeliveryDestinations(ctx context.Context) ([]models.AuditDeliveryDestination, error) {
	rows, err := r.Pool.Query(ctx, auditDeliveryDestinationSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AuditDeliveryDestination, 0)
	for rows.Next() {
		item, err := scanAuditDeliveryDestination(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (r *Repo) GetAuditDeliveryDestination(ctx context.Context, id uuid.UUID) (*models.AuditDeliveryDestination, error) {
	row := r.Pool.QueryRow(ctx, auditDeliveryDestinationSelect+` WHERE id = $1`, id)
	item, err := scanAuditDeliveryDestination(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *Repo) CreateAuditDeliveryDestination(ctx context.Context, body *models.CreateAuditDeliveryDestinationRequest, createdBy string) (*models.AuditDeliveryDestination, error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, errors.New("name is required")
	}
	body.DestinationType = strings.TrimSpace(body.DestinationType)
	if body.DestinationType != "siem_api" && body.DestinationType != "openfoundry_dataset" {
		return nil, errors.New("destination_type must be siem_api or openfoundry_dataset")
	}
	schemaVersion := strings.TrimSpace(body.SchemaVersion)
	if schemaVersion == "" {
		schemaVersion = "audit.3"
	}
	if schemaVersion != "audit.3" {
		return nil, errors.New("schema_version must be audit.3")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	metadata := defaultJSON(body.Metadata, "{}")
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO audit_delivery_destinations
		    (id, organization_id, name, destination_type, schema_version,
		     endpoint_url, dataset_rid, enabled, metadata, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)
		 RETURNING id, organization_id, name, destination_type, schema_version,
		           endpoint_url, dataset_rid, enabled, validation_status,
		           validation_message, last_validated_at, last_backfill_status,
		           last_backfill_started_at, last_backfill_completed_at,
		           metadata, created_by, created_at, updated_at`,
		uuid.New(), body.OrganizationID, strings.TrimSpace(body.Name),
		body.DestinationType, schemaVersion, cleanStringPtr(body.EndpointURL),
		cleanStringPtr(body.DatasetRID), enabled, metadata, createdBy,
	)
	return scanAuditDeliveryDestination(row)
}

func (r *Repo) ValidateAuditDeliveryDestination(ctx context.Context, id uuid.UUID) (*models.AuditDeliveryValidationResponse, error) {
	dest, err := r.GetAuditDeliveryDestination(ctx, id)
	if err != nil || dest == nil {
		return nil, err
	}
	status, msg := validateAuditDeliveryDestination(dest)
	var validatedAt *time.Time
	if status == "valid" {
		now := time.Now().UTC()
		validatedAt = &now
	}
	if _, err := r.Pool.Exec(ctx,
		`UPDATE audit_delivery_destinations
		    SET validation_status = $2, validation_message = $3,
		        last_validated_at = $4, updated_at = NOW()
		  WHERE id = $1`,
		id, status, msg, validatedAt,
	); err != nil {
		return nil, err
	}
	return &models.AuditDeliveryValidationResponse{
		DestinationID:    id,
		ValidationStatus: status,
		Message:          msg,
		ValidatedAt:      validatedAt,
	}, nil
}

func (r *Repo) BackfillAuditDeliveryDestination(ctx context.Context, id uuid.UUID, body *models.AuditDeliveryBackfillRequest) (*models.AuditDeliveryFile, error) {
	if body.EndTime.IsZero() || body.StartTime.IsZero() || !body.EndTime.After(body.StartTime) {
		return nil, errors.New("start_time and end_time are required and end_time must be after start_time")
	}
	dest, err := r.GetAuditDeliveryDestination(ctx, id)
	if err != nil || dest == nil {
		return nil, err
	}
	status, msg := validateAuditDeliveryDestination(dest)
	if status != "valid" {
		if _, updateErr := r.Pool.Exec(ctx,
			`UPDATE audit_delivery_destinations
			    SET validation_status = $2, validation_message = $3,
			        last_backfill_status = 'failed',
			        last_backfill_started_at = NOW(),
			        last_backfill_completed_at = NOW(),
			        updated_at = NOW()
			  WHERE id = $1`,
			id, status, msg,
		); updateErr != nil {
			return nil, updateErr
		}
		return nil, fmt.Errorf("%w: %s", ErrAuditDeliveryDestinationInvalid, msg)
	}
	if _, err := r.Pool.Exec(ctx,
		`UPDATE audit_delivery_destinations
		    SET validation_status = 'valid', validation_message = $2,
		        last_backfill_status = 'running',
		        last_backfill_started_at = NOW(),
		        last_backfill_completed_at = NULL,
		        updated_at = NOW()
		  WHERE id = $1`,
		id, msg,
	); err != nil {
		return nil, err
	}
	events, err := r.auditEventsForDelivery(ctx, dest.OrganizationID, body.StartTime.UTC(), body.EndTime.UTC())
	if err != nil {
		_ = r.markAuditDeliveryBackfillFailed(ctx, id, err.Error())
		return nil, err
	}
	content, err := auditEventsNDJSON(events)
	if err != nil {
		_ = r.markAuditDeliveryBackfillFailed(ctx, id, err.Error())
		return nil, err
	}
	duplicateCount := duplicateLogEntryCount(events)
	sum := sha256.Sum256([]byte(content))
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO audit_delivery_files
		    (id, destination_id, organization_id, schema_version, content_format,
		     start_time, end_time, event_count, duplicate_count, content_sha256,
		     content_bytes, status, content)
		 VALUES ($1,$2,$3,$4,'application/x-ndjson',$5,$6,$7,$8,$9,$10,'available',$11)
		 RETURNING id, destination_id, organization_id, schema_version,
		           content_format, start_time, end_time, event_count,
		           duplicate_count, content_sha256, content_bytes, status,
		           error_message, created_at`,
		uuid.New(), id, dest.OrganizationID, dest.SchemaVersion,
		body.StartTime.UTC(), body.EndTime.UTC(), int64(len(events)),
		duplicateCount, hex.EncodeToString(sum[:]), int64(len(content)), content,
	)
	file, err := scanAuditDeliveryFile(row)
	if err != nil {
		_ = r.markAuditDeliveryBackfillFailed(ctx, id, err.Error())
		return nil, err
	}
	if _, err := r.Pool.Exec(ctx,
		`UPDATE audit_delivery_destinations
		    SET last_backfill_status = 'completed',
		        last_backfill_completed_at = NOW(),
		        updated_at = NOW()
		  WHERE id = $1`,
		id,
	); err != nil {
		return nil, err
	}
	return file, nil
}

func (r *Repo) ListAuditDeliveryFiles(ctx context.Context, organizationID *uuid.UUID, start, end *time.Time, schemaVersion string) ([]models.AuditDeliveryFile, error) {
	conds := []string{"1=1"}
	args := []any{}
	if organizationID != nil {
		args = append(args, *organizationID)
		conds = append(conds, fmt.Sprintf("organization_id = $%d", len(args)))
	}
	if start != nil {
		args = append(args, start.UTC())
		conds = append(conds, fmt.Sprintf("end_time > $%d", len(args)))
	}
	if end != nil {
		args = append(args, end.UTC())
		conds = append(conds, fmt.Sprintf("start_time < $%d", len(args)))
	}
	if strings.TrimSpace(schemaVersion) != "" {
		args = append(args, strings.TrimSpace(schemaVersion))
		conds = append(conds, fmt.Sprintf("schema_version = $%d", len(args)))
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, destination_id, organization_id, schema_version,
		        content_format, start_time, end_time, event_count,
		        duplicate_count, content_sha256, content_bytes, status,
		        error_message, created_at
		   FROM audit_delivery_files
		  WHERE `+strings.Join(conds, " AND ")+`
		  ORDER BY start_time DESC, created_at DESC
		  LIMIT 500`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AuditDeliveryFile, 0)
	for rows.Next() {
		item, err := scanAuditDeliveryFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (r *Repo) GetAuditDeliveryFileContent(ctx context.Context, id uuid.UUID) (*models.AuditDeliveryFileContent, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, destination_id, organization_id, schema_version,
		        content_format, start_time, end_time, event_count,
		        duplicate_count, content_sha256, content_bytes, status,
		        error_message, created_at, content
		   FROM audit_delivery_files
		  WHERE id = $1`,
		id,
	)
	file, content, err := scanAuditDeliveryFileWithContent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.AuditDeliveryFileContent{File: *file, Content: content}, nil
}

func (r *Repo) markAuditDeliveryBackfillFailed(ctx context.Context, id uuid.UUID, msg string) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE audit_delivery_destinations
		    SET last_backfill_status = 'failed',
		        last_backfill_completed_at = NOW(),
		        validation_message = $2,
		        updated_at = NOW()
		  WHERE id = $1`,
		id, msg,
	)
	return err
}

func (r *Repo) auditEventsForDelivery(ctx context.Context, organizationID *uuid.UUID, start, end time.Time) ([]models.AuditEvent, error) {
	sql := auditEventSelect + ` WHERE occurred_at >= $1 AND occurred_at < $2`
	args := []any{start, end}
	if organizationID != nil {
		args = append(args, organizationID.String())
		sql += fmt.Sprintf(" AND (metadata->>'organization_id' = $%d OR metadata->>'org_id' = $%d)", len(args), len(args))
	}
	sql += ` ORDER BY occurred_at ASC, log_entry_id ASC`
	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AuditEvent, 0)
	for rows.Next() {
		var event models.AuditEvent
		if err := scanAuditEvent(rows, &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func auditEventsNDJSON(events []models.AuditEvent) (string, error) {
	var b strings.Builder
	for i := range events {
		raw, err := json.Marshal(events[i])
		if err != nil {
			return "", err
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func duplicateLogEntryCount(events []models.AuditEvent) int64 {
	seen := map[uuid.UUID]int{}
	var dup int64
	for _, event := range events {
		seen[event.LogEntryID]++
		if seen[event.LogEntryID] > 1 {
			dup++
		}
	}
	return dup
}

func validateAuditDeliveryDestination(dest *models.AuditDeliveryDestination) (string, string) {
	if dest == nil {
		return "invalid", "destination not found"
	}
	if !dest.Enabled {
		return "invalid", "destination is disabled"
	}
	if dest.SchemaVersion != "audit.3" {
		return "invalid", "only audit.3 delivery is supported"
	}
	switch dest.DestinationType {
	case "siem_api":
		if dest.EndpointURL == nil || strings.TrimSpace(*dest.EndpointURL) == "" {
			return "invalid", "siem_api destinations require endpoint_url"
		}
		u, err := url.Parse(strings.TrimSpace(*dest.EndpointURL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "invalid", "endpoint_url must be an absolute http(s) URL"
		}
		return "valid", "SIEM API destination is syntactically valid"
	case "openfoundry_dataset":
		if dest.DatasetRID == nil || strings.TrimSpace(*dest.DatasetRID) == "" {
			return "invalid", "openfoundry_dataset destinations require dataset_rid"
		}
		return "valid", "OpenFoundry dataset destination is configured"
	default:
		return "invalid", "unsupported destination_type"
	}
}

func scanAuditDeliveryDestination(row rowLikeT) (*models.AuditDeliveryDestination, error) {
	item := &models.AuditDeliveryDestination{}
	if err := row.Scan(&item.ID, &item.OrganizationID, &item.Name,
		&item.DestinationType, &item.SchemaVersion, &item.EndpointURL,
		&item.DatasetRID, &item.Enabled, &item.ValidationStatus,
		&item.ValidationMessage, &item.LastValidatedAt,
		&item.LastBackfillStatus, &item.LastBackfillStartedAt,
		&item.LastBackfillCompletedAt, &item.Metadata, &item.CreatedBy,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return item, nil
}

func scanAuditDeliveryFile(row rowLikeT) (*models.AuditDeliveryFile, error) {
	item := &models.AuditDeliveryFile{}
	if err := row.Scan(&item.ID, &item.DestinationID, &item.OrganizationID,
		&item.SchemaVersion, &item.ContentFormat, &item.StartTime,
		&item.EndTime, &item.EventCount, &item.DuplicateCount,
		&item.ContentSHA256, &item.ContentBytes, &item.Status,
		&item.ErrorMessage, &item.CreatedAt); err != nil {
		return nil, err
	}
	return item, nil
}

func scanAuditDeliveryFileWithContent(row rowLikeT) (*models.AuditDeliveryFile, string, error) {
	item := &models.AuditDeliveryFile{}
	var content string
	if err := row.Scan(&item.ID, &item.DestinationID, &item.OrganizationID,
		&item.SchemaVersion, &item.ContentFormat, &item.StartTime,
		&item.EndTime, &item.EventCount, &item.DuplicateCount,
		&item.ContentSHA256, &item.ContentBytes, &item.Status,
		&item.ErrorMessage, &item.CreatedAt, &content); err != nil {
		return nil, "", err
	}
	return item, content, nil
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
