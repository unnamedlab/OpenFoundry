// Audit-event wire types + helpers.
//
// The hash-chain projections (`previous_hash`, `entry_hash`,
// `sequence`) are populated by `domain/immutablelog`, never by callers.
package models

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// AuditEventStatus enumerates the success/failure/denied vocabulary.
type AuditEventStatus string

const (
	StatusSuccess AuditEventStatus = "success"
	StatusFailure AuditEventStatus = "failure"
	StatusDenied  AuditEventStatus = "denied"
)

// ParseAuditEventStatus mirrors `AuditEventStatus::from_str`.
func ParseAuditEventStatus(value string) (AuditEventStatus, error) {
	switch value {
	case "success":
		return StatusSuccess, nil
	case "failure":
		return StatusFailure, nil
	case "denied":
		return StatusDenied, nil
	default:
		return "", fmt.Errorf("unsupported audit event status: %s", value)
	}
}

// AuditSeverity enumerates the per-event severity bucket.
type AuditSeverity string

const (
	SeverityLow      AuditSeverity = "low"
	SeverityMedium   AuditSeverity = "medium"
	SeverityHigh     AuditSeverity = "high"
	SeverityCritical AuditSeverity = "critical"
)

// IsCritical mirrors `AuditSeverity::is_critical`.
func (s AuditSeverity) IsCritical() bool { return s == SeverityCritical }

// ParseAuditSeverity mirrors `AuditSeverity::from_str`.
func ParseAuditSeverity(value string) (AuditSeverity, error) {
	switch value {
	case "low":
		return SeverityLow, nil
	case "medium":
		return SeverityMedium, nil
	case "high":
		return SeverityHigh, nil
	case "critical":
		return SeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported audit severity: %s", value)
	}
}

// AppendAuditEventRequest mirrors the Rust struct of the same name.
//
// `RetentionDays` defaults to 365 when omitted (matches the Rust
// `default_retention_days`). Labels are deduplicated server-side.
type AppendAuditEventRequest struct {
	SourceService    string              `json:"source_service"`
	Product          string              `json:"product,omitempty"`
	ProductVersion   string              `json:"product_version,omitempty"`
	ProducerType     string              `json:"producer_type,omitempty"`
	Channel          string              `json:"channel"`
	EventID          *uuid.UUID          `json:"event_id,omitempty"`
	LogEntryID       *uuid.UUID          `json:"log_entry_id,omitempty"`
	SequenceID       *uuid.UUID          `json:"sequence_id,omitempty"`
	Actor            string              `json:"actor"`
	ActorID          string              `json:"actor_id,omitempty"`
	ActorType        string              `json:"actor_type,omitempty"`
	SessionID        *string             `json:"session_id,omitempty"`
	ServiceAccountID *string             `json:"service_account_id,omitempty"`
	TokenID          *string             `json:"token_id,omitempty"`
	Action           string              `json:"action"`
	Categories       []string            `json:"categories,omitempty"`
	ResourceType     string              `json:"resource_type"`
	ResourceID       string              `json:"resource_id"`
	Entities         json.RawMessage     `json:"entities,omitempty"`
	Origins          []string            `json:"origins,omitempty"`
	Origin           *string             `json:"origin,omitempty"`
	SourceOrigin     *string             `json:"source_origin,omitempty"`
	TraceID          *string             `json:"trace_id,omitempty"`
	Status           AuditEventStatus    `json:"status"`
	Outcome          string              `json:"outcome,omitempty"`
	Severity         AuditSeverity       `json:"severity"`
	Classification   ClassificationLevel `json:"classification"`
	SubjectID        *string             `json:"subject_id,omitempty"`
	IPAddress        *string             `json:"ip_address,omitempty"`
	Location         *string             `json:"location,omitempty"`
	Metadata         json.RawMessage     `json:"metadata,omitempty"`
	ErrorMetadata    json.RawMessage     `json:"error_metadata,omitempty"`
	RequestFields    json.RawMessage     `json:"request_fields,omitempty"`
	ResultFields     json.RawMessage     `json:"result_fields,omitempty"`
	Labels           []string            `json:"labels,omitempty"`
	ParentEventID    *uuid.UUID          `json:"parent_event_id,omitempty"`
	InitiatorType    string              `json:"initiator_type,omitempty"`
	AuditAccessTier  string              `json:"audit_access_tier,omitempty"`
	RetentionDays    int32               `json:"retention_days,omitempty"`
}

// EffectiveRetentionDays returns the retention TTL for a request,
// applying the Rust default of 365 when the caller did not supply one.
func (r *AppendAuditEventRequest) EffectiveRetentionDays() int32 {
	if r.RetentionDays <= 0 {
		return 365
	}
	return r.RetentionDays
}

// EventQuery mirrors `handlers::events::EventQuery`. Empty strings for
// missing query parameters are normalised to nil pointers.
type EventQuery struct {
	SourceService  *string
	SubjectID      *string
	Classification *string
	ResourceID     *string
	Category       *string
	TraceID        *string
	EventID        *string
}

// AuditOverview mirrors the Rust struct of the same name.
type AuditOverview struct {
	EventCount         int64       `json:"event_count"`
	CriticalEventCount int64       `json:"critical_event_count"`
	CollectorCount     int64       `json:"collector_count"`
	ActivePolicyCount  int64       `json:"active_policy_count"`
	AnomalyCount       int64       `json:"anomaly_count"`
	GDPRSubjectCount   int64       `json:"gdpr_subject_count"`
	LatestEvent        *AuditEvent `json:"latest_event"`
}

// EventListResponse mirrors the Rust struct: events + anomaly alerts.
type EventListResponse struct {
	Items     []AuditEvent   `json:"items"`
	Anomalies []AnomalyAlert `json:"anomalies"`
}

// LabelsAsList round-trips the `Labels` JSON column into a string slice.
//
// Rust persists the labels column as a `JSONB` array of strings; the Go
// repo reads it as `json.RawMessage` so callers can choose how to
// project it. Most domain helpers want the typed slice.
func (e *AuditEvent) LabelsAsList() ([]string, error) {
	if len(e.Labels) == 0 || string(e.Labels) == "null" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(e.Labels, &out); err != nil {
		return nil, fmt.Errorf("decode labels: %w", err)
	}
	return out, nil
}

// SetLabels marshals a string slice back into the `Labels` field.
func (e *AuditEvent) SetLabels(labels []string) error {
	raw, err := json.Marshal(labels)
	if err != nil {
		return err
	}
	e.Labels = raw
	return nil
}

// ClassificationLevelFromStatus is a small convenience that lets the
// hash-chain code disambiguate between an unparsed `Classification`
// column (string) and a typed enum value.
func (e *AuditEvent) ClassificationLevel() (ClassificationLevel, error) {
	if e.Classification == "" {
		return ClassificationPublic, errors.New("classification is empty")
	}
	return ParseClassificationLevel(e.Classification)
}
