// Package models holds wire types for audit-compliance-service.
//
// Absorbed sub-modules: audit ledger + retention policies + sds
// (sensitive-data scanning) + lineage_deletion + saga audit log.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ListResponse is the canonical envelope.
type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// ─── Audit ledger (audit_events, hash-chained, append-only) ────────

type AuditEvent struct {
	ID               uuid.UUID       `json:"id"`
	EventID          uuid.UUID       `json:"event_id"`
	LogEntryID       uuid.UUID       `json:"log_entry_id"`
	SequenceID       *uuid.UUID      `json:"sequence_id,omitempty"`
	Sequence         int64           `json:"sequence"`
	PreviousHash     string          `json:"previous_hash"`
	EntryHash        string          `json:"entry_hash"`
	SourceService    string          `json:"source_service"`
	Product          string          `json:"product"`
	ProductVersion   string          `json:"product_version"`
	ProducerType     string          `json:"producer_type"`
	Channel          string          `json:"channel"`
	Actor            string          `json:"actor"`
	ActorID          string          `json:"actor_id"`
	ActorType        string          `json:"actor_type"`
	SessionID        *string         `json:"session_id,omitempty"`
	ServiceAccountID *string         `json:"service_account_id,omitempty"`
	TokenID          *string         `json:"token_id,omitempty"`
	Action           string          `json:"action"`
	Categories       []string        `json:"categories"`
	ResourceType     string          `json:"resource_type"`
	ResourceID       string          `json:"resource_id"`
	Entities         json.RawMessage `json:"entities"`
	Origins          []string        `json:"origins"`
	Origin           *string         `json:"origin,omitempty"`
	SourceOrigin     *string         `json:"source_origin,omitempty"`
	TraceID          *string         `json:"trace_id,omitempty"`
	Status           string          `json:"status"`
	Outcome          string          `json:"outcome"`
	Severity         string          `json:"severity"`
	Classification   string          `json:"classification"`
	SubjectID        *string         `json:"subject_id"`
	IPAddress        *string         `json:"ip_address"`
	Location         *string         `json:"location"`
	Metadata         json.RawMessage `json:"metadata"`
	ErrorMetadata    json.RawMessage `json:"error_metadata"`
	RequestFields    json.RawMessage `json:"request_fields"`
	ResultFields     json.RawMessage `json:"result_fields"`
	Labels           json.RawMessage `json:"labels"`
	ParentEventID    *uuid.UUID      `json:"parent_event_id,omitempty"`
	InitiatorType    string          `json:"initiator_type"`
	AuditAccessTier  string          `json:"audit_access_tier"`
	RetentionUntil   time.Time       `json:"retention_until"`
	OccurredAt       time.Time       `json:"occurred_at"`
	IngestedAt       time.Time       `json:"ingested_at"`
}

// ─── Audit policies (per-classification retention rules) ────────────

type AuditPolicy struct {
	ID             uuid.UUID       `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Scope          string          `json:"scope"`
	Classification string          `json:"classification"`
	RetentionDays  int32           `json:"retention_days"`
	LegalHold      bool            `json:"legal_hold"`
	PurgeMode      string          `json:"purge_mode"`
	Active         bool            `json:"active"`
	Rules          json.RawMessage `json:"rules"`
	UpdatedBy      string          `json:"updated_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ─── Audit delivery (SIEM/API + governed dataset exports) ───────────

type AuditDeliveryDestination struct {
	ID                      uuid.UUID       `json:"id"`
	OrganizationID          *uuid.UUID      `json:"organization_id,omitempty"`
	Name                    string          `json:"name"`
	DestinationType         string          `json:"destination_type"`
	SchemaVersion           string          `json:"schema_version"`
	EndpointURL             *string         `json:"endpoint_url,omitempty"`
	DatasetRID              *string         `json:"dataset_rid,omitempty"`
	Enabled                 bool            `json:"enabled"`
	ValidationStatus        string          `json:"validation_status"`
	ValidationMessage       string          `json:"validation_message"`
	LastValidatedAt         *time.Time      `json:"last_validated_at,omitempty"`
	LastBackfillStatus      string          `json:"last_backfill_status"`
	LastBackfillStartedAt   *time.Time      `json:"last_backfill_started_at,omitempty"`
	LastBackfillCompletedAt *time.Time      `json:"last_backfill_completed_at,omitempty"`
	Metadata                json.RawMessage `json:"metadata"`
	CreatedBy               string          `json:"created_by"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
}

type CreateAuditDeliveryDestinationRequest struct {
	OrganizationID  *uuid.UUID      `json:"organization_id,omitempty"`
	Name            string          `json:"name"`
	DestinationType string          `json:"destination_type"`
	SchemaVersion   string          `json:"schema_version,omitempty"`
	EndpointURL     *string         `json:"endpoint_url,omitempty"`
	DatasetRID      *string         `json:"dataset_rid,omitempty"`
	Enabled         *bool           `json:"enabled,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type AuditDeliveryValidationResponse struct {
	DestinationID    uuid.UUID  `json:"destination_id"`
	ValidationStatus string     `json:"validation_status"`
	Message          string     `json:"message"`
	ValidatedAt      *time.Time `json:"validated_at,omitempty"`
}

type AuditDeliveryBackfillRequest struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

type AuditDeliveryFile struct {
	ID             uuid.UUID  `json:"id"`
	DestinationID  uuid.UUID  `json:"destination_id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	SchemaVersion  string     `json:"schema_version"`
	ContentFormat  string     `json:"content_format"`
	StartTime      time.Time  `json:"start_time"`
	EndTime        time.Time  `json:"end_time"`
	EventCount     int64      `json:"event_count"`
	DuplicateCount int64      `json:"duplicate_count"`
	ContentSHA256  string     `json:"content_sha256"`
	ContentBytes   int64      `json:"content_bytes"`
	Status         string     `json:"status"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type AuditDeliveryFileContent struct {
	File    AuditDeliveryFile `json:"file"`
	Content string            `json:"content"`
}

// ─── Compliance reports ────────────────────────────────────────────

type ComplianceReport struct {
	ID                 uuid.UUID       `json:"id"`
	Standard           string          `json:"standard"`
	Title              string          `json:"title"`
	Scope              string          `json:"scope"`
	WindowStart        time.Time       `json:"window_start"`
	WindowEnd          time.Time       `json:"window_end"`
	GeneratedAt        time.Time       `json:"generated_at"`
	Status             string          `json:"status"`
	Findings           json.RawMessage `json:"findings"`
	Artifact           json.RawMessage `json:"artifact"`
	RelevantEventCount int64           `json:"relevant_event_count"`
	PolicyCount        int64           `json:"policy_count"`
	ControlSummary     string          `json:"control_summary"`
	ExpiresAt          time.Time       `json:"expires_at"`
}

// ─── Retention policies + jobs ─────────────────────────────────────

type RetentionPolicy struct {
	ID                 uuid.UUID       `json:"id"`
	Name               string          `json:"name"`
	Scope              string          `json:"scope"`
	TargetKind         string          `json:"target_kind"`
	RetentionDays      int32           `json:"retention_days"`
	LegalHold          bool            `json:"legal_hold"`
	PurgeMode          string          `json:"purge_mode"`
	Rules              json.RawMessage `json:"rules"`
	UpdatedBy          string          `json:"updated_by"`
	Active             bool            `json:"active"`
	IsSystem           bool            `json:"is_system"`
	Selector           json.RawMessage `json:"selector"`
	Criteria           json.RawMessage `json:"criteria"`
	GracePeriodMinutes int32           `json:"grace_period_minutes"`
	LastAppliedAt      *time.Time      `json:"last_applied_at"`
	NextRunAt          *time.Time      `json:"next_run_at"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type CreateRetentionPolicyRequest struct {
	Name               string          `json:"name"`
	Scope              string          `json:"scope,omitempty"`
	TargetKind         string          `json:"target_kind"`
	RetentionDays      int32           `json:"retention_days"`
	LegalHold          *bool           `json:"legal_hold,omitempty"`
	PurgeMode          string          `json:"purge_mode"`
	Rules              json.RawMessage `json:"rules,omitempty"`
	Selector           json.RawMessage `json:"selector,omitempty"`
	Criteria           json.RawMessage `json:"criteria,omitempty"`
	GracePeriodMinutes *int32          `json:"grace_period_minutes,omitempty"`
}

type UpdateRetentionPolicyRequest struct {
	RetentionDays      *int32          `json:"retention_days,omitempty"`
	LegalHold          *bool           `json:"legal_hold,omitempty"`
	PurgeMode          *string         `json:"purge_mode,omitempty"`
	Rules              json.RawMessage `json:"rules,omitempty"`
	Selector           json.RawMessage `json:"selector,omitempty"`
	Criteria           json.RawMessage `json:"criteria,omitempty"`
	GracePeriodMinutes *int32          `json:"grace_period_minutes,omitempty"`
	Active             *bool           `json:"active,omitempty"`
}

type RetentionJob struct {
	ID                  uuid.UUID  `json:"id"`
	PolicyID            uuid.UUID  `json:"policy_id"`
	TargetDatasetID     *uuid.UUID `json:"target_dataset_id"`
	TargetTransactionID *uuid.UUID `json:"target_transaction_id"`
	Status              string     `json:"status"`
	ActionSummary       string     `json:"action_summary"`
	AffectedRecordCount int32      `json:"affected_record_count"`
	CreatedAt           time.Time  `json:"created_at"`
	CompletedAt         *time.Time `json:"completed_at"`
}

// ─── SDS (sensitive data scanning) ──────────────────────────────────

type SDSScanJob struct {
	ID              uuid.UUID       `json:"id"`
	TargetName      string          `json:"target_name"`
	Scope           string          `json:"scope"`
	Status          string          `json:"status"`
	RiskScore       int32           `json:"risk_score"`
	Findings        json.RawMessage `json:"findings"`
	IssueCount      int32           `json:"issue_count"`
	RedactedContent string          `json:"redacted_content"`
	Remediations    json.RawMessage `json:"remediations"`
	RequestedBy     *uuid.UUID      `json:"requested_by"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type SDSIssue struct {
	ID                 uuid.UUID       `json:"id"`
	JobID              uuid.UUID       `json:"job_id"`
	Kind               string          `json:"kind"`
	Severity           string          `json:"severity"`
	Status             string          `json:"status"`
	MatchedValue       string          `json:"matched_value"`
	RedactedValue      string          `json:"redacted_value"`
	MatchCount         int32           `json:"match_count"`
	Markings           json.RawMessage `json:"markings"`
	RemediationActions json.RawMessage `json:"remediation_actions"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type SDSRemediationRule struct {
	ID                 uuid.UUID       `json:"id"`
	Name               string          `json:"name"`
	Scope              string          `json:"scope"`
	MatchConditions    json.RawMessage `json:"match_conditions"`
	RemediationActions json.RawMessage `json:"remediation_actions"`
	UpdatedBy          *uuid.UUID      `json:"updated_by"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// ─── Lineage deletion (GDPR / right-to-be-forgotten) ────────────────

type LineageDeletionRequest struct {
	ID           uuid.UUID       `json:"id"`
	DatasetID    uuid.UUID       `json:"dataset_id"`
	SubjectID    *string         `json:"subject_id"`
	HardDelete   bool            `json:"hard_delete"`
	LegalHold    bool            `json:"legal_hold"`
	Impact       json.RawMessage `json:"impact"`
	Status       string          `json:"status"`
	DeletedPaths json.RawMessage `json:"deleted_paths"`
	AuditTrace   json.RawMessage `json:"audit_trace"`
	RequestedAt  time.Time       `json:"requested_at"`
	CompletedAt  *time.Time      `json:"completed_at"`
}

type CreateLineageDeletionRequest struct {
	DatasetID  uuid.UUID `json:"dataset_id"`
	SubjectID  *string   `json:"subject_id,omitempty"`
	HardDelete *bool     `json:"hard_delete,omitempty"`
	LegalHold  *bool     `json:"legal_hold,omitempty"`
}

// ─── Saga audit log ─────────────────────────────────────────────────

type SagaAuditEvent struct {
	EventID       uuid.UUID       `json:"event_id"`
	SagaID        uuid.UUID       `json:"saga_id"`
	SagaName      string          `json:"saga_name"`
	SourceService string          `json:"source_service"`
	Kind          string          `json:"kind"`
	StepName      *string         `json:"step_name"`
	Payload       json.RawMessage `json:"payload"`
	CorrelationID *uuid.UUID      `json:"correlation_id"`
	TenantID      *string         `json:"tenant_id"`
	ObservedAt    time.Time       `json:"observed_at"`
}
