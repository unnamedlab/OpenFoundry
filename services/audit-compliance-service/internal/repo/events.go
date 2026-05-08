// Audit-event hash-chain insert + single-event lookup. The list-all
// helper lives in repo.go (pre-existing).

package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/immutablelog"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// LatestSequenceAndHash returns the head of the audit chain. Both
// fields are nil for an empty table.
func (r *Repo) LatestSequenceAndHash(ctx context.Context) (*int64, *string, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT MAX(sequence) AS sequence,
		        (ARRAY_AGG(entry_hash ORDER BY sequence DESC))[1] AS entry_hash
		   FROM audit_events`,
	)
	var (
		seq  *int64
		hash *string
	)
	if err := row.Scan(&seq, &hash); err != nil {
		return nil, nil, err
	}
	return seq, hash, nil
}

// GetAuditEvent fetches a single event by id (nil + nil error when
// missing).
func (r *Repo) GetAuditEvent(ctx context.Context, id uuid.UUID) (*models.AuditEvent, error) {
	row := r.Pool.QueryRow(ctx, auditEventSelect+` WHERE id = $1`, id)
	var e models.AuditEvent
	if err := row.Scan(&e.ID, &e.Sequence, &e.PreviousHash, &e.EntryHash,
		&e.SourceService, &e.Channel, &e.Actor, &e.Action,
		&e.ResourceType, &e.ResourceID, &e.Status, &e.Severity,
		&e.Classification, &e.SubjectID, &e.IPAddress, &e.Location,
		&e.Metadata, &e.Labels, &e.RetentionUntil, &e.OccurredAt,
		&e.IngestedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// PersistAuditEvent ports `handlers::events::persist_event`. Computes
// sequence + previous_hash + entry_hash, sorts/dedups labels, applies
// the retention TTL and inserts the row.
func (r *Repo) PersistAuditEvent(ctx context.Context, request *models.AppendAuditEventRequest) (*models.AuditEvent, error) {
	if request.Action == "" {
		return nil, errors.New("action is required")
	}

	prevSeq, prevHash, err := r.LatestSequenceAndHash(ctx)
	if err != nil {
		return nil, err
	}

	sequence := immutablelog.NextSequence(prevSeq)
	previousHash := immutablelog.PreviousHashValue(prevHash)
	entryHash := immutablelog.ChainHash(sequence, previousHash, request.SourceService, request.Action)

	now := time.Now().UTC()
	id := uuid.New()
	labels := immutablelog.SortedUniqueLabels(request.Labels)
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	metadata := request.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	retentionUntil := now.AddDate(0, 0, int(request.EffectiveRetentionDays()))

	if _, err := r.Pool.Exec(ctx,
		`INSERT INTO audit_events
		      (id, sequence, previous_hash, entry_hash, source_service, channel,
		       actor, action, resource_type, resource_id, status, severity,
		       classification, subject_id, ip_address, location, metadata, labels,
		       retention_until, occurred_at, ingested_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		            $14, $15, $16, $17::jsonb, $18::jsonb, $19, $20, $21)`,
		id, sequence, previousHash, entryHash, request.SourceService, request.Channel,
		request.Actor, request.Action, request.ResourceType, request.ResourceID,
		string(request.Status), string(request.Severity), string(request.Classification),
		request.SubjectID, request.IPAddress, request.Location,
		metadata, labelsJSON, retentionUntil, now, now,
	); err != nil {
		return nil, err
	}
	stored, err := r.GetAuditEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, errors.New("created audit event could not be reloaded")
	}
	// Mirror the Rust impl: re-derive labels post-write so `contains-
	// sensitive-data` / `gdpr-subject-linked` flags are present in the
	// response without a second SQL round-trip.
	enriched, err := immutablelog.LabelEvent(stored, labels)
	if err != nil {
		return nil, err
	}
	if err := stored.SetLabels(enriched); err != nil {
		return nil, err
	}
	stored.RetentionUntil = retentionUntil
	return stored, nil
}
