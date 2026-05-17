// Audit-event hash-chain insert + single-event lookup. The list-all
// helper lives in repo.go (pre-existing).

package repo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
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
	if err := scanAuditEvent(row, &e); err != nil {
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
	eventID := id
	if request.EventID != nil && *request.EventID != uuid.Nil {
		eventID = *request.EventID
	}
	logEntryID := id
	if request.LogEntryID != nil && *request.LogEntryID != uuid.Nil {
		logEntryID = *request.LogEntryID
	}
	sequenceID := request.SequenceID
	if sequenceID != nil && *sequenceID == uuid.Nil {
		sequenceID = nil
	}
	labels := immutablelog.SortedUniqueLabels(request.Labels)
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	metadata := defaultRawObject(request.Metadata)
	errorMetadata := defaultRawObject(request.ErrorMetadata)
	requestFields := defaultRawObject(request.RequestFields)
	if len(request.RequestFields) == 0 {
		requestFields = metadata
	}
	resultFields := defaultRawObject(request.ResultFields)
	entities := request.Entities
	if len(entities) == 0 {
		entities = defaultAuditEntities(request.ResourceType, request.ResourceID)
	}
	categories := normalizedStrings(request.Categories)
	origins := normalizedStrings(request.Origins)
	product := defaultString(request.Product, request.SourceService)
	producerType := defaultString(request.ProducerType, "SERVER")
	actorID := defaultString(request.ActorID, request.Actor)
	actorType := defaultString(request.ActorType, inferActorType(request.Actor, request.ServiceAccountID))
	actor := request.Actor
	if strings.TrimSpace(actor) == "" && request.ServiceAccountID != nil && strings.TrimSpace(*request.ServiceAccountID) != "" {
		actor = "service:" + strings.TrimSpace(*request.ServiceAccountID)
	}
	if strings.TrimSpace(actorID) == "" {
		actorID = actor
	}
	outcome := normalizeOutcome(request.Outcome, request.Status)
	initiatorType := defaultString(request.InitiatorType, inferInitiatorType(origins, actorType))
	auditAccessTier := defaultString(request.AuditAccessTier, "security_sensitive")
	retentionUntil := now.AddDate(0, 0, int(request.EffectiveRetentionDays()))

	if _, err := r.Pool.Exec(ctx,
		`INSERT INTO audit_events
		      (id, event_id, log_entry_id, sequence_id, sequence, previous_hash,
		       entry_hash, source_service, product, product_version, producer_type,
		       channel, actor, actor_id, actor_type, session_id, service_account_id,
		       token_id, action, categories, resource_type, resource_id, entities,
		       origins, origin, source_origin, trace_id, status, outcome, severity,
		       classification, subject_id, ip_address, location, metadata,
		       error_metadata, request_fields, result_fields, labels, parent_event_id,
		       initiator_type, audit_access_tier, retention_until, occurred_at, ingested_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		            $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
		            $21, $22, $23::jsonb, $24, $25, $26, $27, $28, $29, $30,
		            $31, $32, $33, $34, $35::jsonb, $36::jsonb, $37::jsonb,
		            $38::jsonb, $39::jsonb, $40, $41, $42, $43, $44, $45)`,
		id, eventID, logEntryID, sequenceID, sequence, previousHash, entryHash,
		request.SourceService, product, request.ProductVersion, producerType,
		request.Channel, actor, actorID, actorType, request.SessionID,
		request.ServiceAccountID, request.TokenID, request.Action, categories,
		request.ResourceType, request.ResourceID, entities, origins, request.Origin,
		request.SourceOrigin, request.TraceID, string(request.Status), outcome,
		string(request.Severity), string(request.Classification), request.SubjectID,
		request.IPAddress, request.Location, metadata, errorMetadata, requestFields,
		resultFields, labelsJSON, request.ParentEventID, initiatorType,
		auditAccessTier, retentionUntil, now, now,
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

func defaultRawObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	return raw
}

func defaultAuditEntities(resourceType, resourceID string) json.RawMessage {
	if strings.TrimSpace(resourceType) == "" && strings.TrimSpace(resourceID) == "" {
		return json.RawMessage(`[]`)
	}
	body, _ := json.Marshal([]map[string]string{{
		"kind": strings.TrimSpace(resourceType),
		"id":   strings.TrimSpace(resourceID),
		"rid":  strings.TrimSpace(resourceID),
	}})
	return body
}

func normalizedStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func inferActorType(actor string, serviceAccountID *string) string {
	if serviceAccountID != nil && strings.TrimSpace(*serviceAccountID) != "" {
		return "service"
	}
	actor = strings.TrimSpace(actor)
	if strings.HasPrefix(actor, "service:") || strings.HasPrefix(actor, "system:") {
		return "service"
	}
	return "user"
}

func inferInitiatorType(origins []string, actorType string) string {
	if len(origins) > 0 {
		return "user"
	}
	if actorType == "service" {
		return "service"
	}
	return "user"
}

func normalizeOutcome(value string, status models.AuditEventStatus) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "ok":
		return "success"
	case "denied", "unauthorized", "forbidden":
		return "unauthorized"
	case "failure", "failed", "error":
		return "error"
	}
	switch status {
	case models.StatusSuccess:
		return "success"
	case models.StatusDenied:
		return "unauthorized"
	default:
		return "error"
	}
}
