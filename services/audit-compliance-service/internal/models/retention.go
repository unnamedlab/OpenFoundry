// Retention-policy + retention-runner types.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// RetentionSelector mirrors the Rust struct of the same name.
//
// All fields are optional. AllDatasets is the catch-all flag used by
// the built-in DELETE_ABORTED_TRANSACTIONS system policy.
type RetentionSelector struct {
	DatasetRid  *string    `json:"dataset_rid,omitempty"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	MarkingID   *uuid.UUID `json:"marking_id,omitempty"`
	AllDatasets bool       `json:"all_datasets,omitempty"`
}

// RetentionCriteria mirrors the Rust struct of the same name.
type RetentionCriteria struct {
	TransactionAgeSeconds *int64  `json:"transaction_age_seconds,omitempty"`
	TransactionState      *string `json:"transaction_state,omitempty"`
	ViewAgeSeconds        *int64  `json:"view_age_seconds,omitempty"`
	LastAccessedSeconds   *int64  `json:"last_accessed_seconds,omitempty"`
}

// RetentionDatasetSelector mirrors Foundry's Retention application
// dataset-selector model while preserving the older selector JSON for
// compatibility. Selectors are cumulative: at least one Select-mode
// selector is normally needed before any dataset can match, and Exclude
// selectors subtract from that set.
type RetentionDatasetSelector struct {
	Mode        string   `json:"mode"` // select | exclude
	Kind        string   `json:"kind"` // all | dataset_rids | folder_rids | derived | trash
	DatasetRIDs []string `json:"dataset_rids,omitempty"`
	FolderRIDs  []string `json:"folder_rids,omitempty"`
	WorkerTypes []string `json:"worker_types,omitempty"`
}

// RetentionTransactionSelector captures transaction narrowing rules.
// These selectors narrow the already-selected dataset transactions.
type RetentionTransactionSelector struct {
	Kind             string   `json:"kind"` // only_branch | not_branch | transaction_count | view_count | older_than | ...
	Branch           string   `json:"branch,omitempty"`
	Count            int32    `json:"count,omitempty"`
	DurationSeconds  int64    `json:"duration_seconds,omitempty"`
	TransactionTypes []string `json:"transaction_types,omitempty"`
}

// RetentionPolicyWarning is returned with policy rows to make dangerous
// or deprecated retention semantics impossible to miss in admin UIs.
type RetentionPolicyWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// SelectorFromRaw decodes a JSONB column into the typed selector.
func SelectorFromRaw(raw json.RawMessage) (RetentionSelector, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return RetentionSelector{}, nil
	}
	var s RetentionSelector
	if err := json.Unmarshal(raw, &s); err != nil {
		return RetentionSelector{}, err
	}
	return s, nil
}

// CriteriaFromRaw decodes a JSONB column into the typed criteria.
func CriteriaFromRaw(raw json.RawMessage) (RetentionCriteria, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return RetentionCriteria{}, nil
	}
	var c RetentionCriteria
	if err := json.Unmarshal(raw, &c); err != nil {
		return RetentionCriteria{}, err
	}
	return c, nil
}

// DatasetRetentionView mirrors the Rust struct.
type DatasetRetentionView struct {
	DatasetID uuid.UUID         `json:"dataset_id"`
	Policies  []RetentionPolicy `json:"policies"`
}

// TransactionRetentionView mirrors the Rust struct.
type TransactionRetentionView struct {
	TransactionID uuid.UUID         `json:"transaction_id"`
	Policies      []RetentionPolicy `json:"policies"`
}

// RunRetentionJobRequest mirrors the Rust struct.
type RunRetentionJobRequest struct {
	PolicyID            uuid.UUID  `json:"policy_id"`
	TargetDatasetID     *uuid.UUID `json:"target_dataset_id,omitempty"`
	TargetTransactionID *uuid.UUID `json:"target_transaction_id,omitempty"`
}

// ListRetentionPoliciesQuery mirrors `handlers::retention::ListPoliciesQuery`.
type ListRetentionPoliciesQuery struct {
	DatasetRid *string
	ProjectID  *uuid.UUID
	MarkingID  *uuid.UUID
	SpaceID    *uuid.UUID
	Active     *bool
	SystemOnly *bool
}

// ResolutionContext mirrors the Rust struct (carries the inheritance
// breadcrumbs project_id / marking_id / space_id / org_id).
type ResolutionContext struct {
	ProjectID *uuid.UUID `json:"project_id"`
	MarkingID *uuid.UUID `json:"marking_id"`
	SpaceID   *uuid.UUID `json:"space_id"`
	OrgID     *uuid.UUID `json:"org_id"`
}

// InheritedPolicies mirrors the Rust struct.
type InheritedPolicies struct {
	Org     []RetentionPolicy `json:"org"`
	Space   []RetentionPolicy `json:"space"`
	Project []RetentionPolicy `json:"project"`
}

// PolicyConflict mirrors the Rust struct.
type PolicyConflict struct {
	WinnerID uuid.UUID `json:"winner_id"`
	LoserID  uuid.UUID `json:"loser_id"`
	Reason   string    `json:"reason"`
}

// ApplicablePoliciesResponse mirrors the Rust struct.
type ApplicablePoliciesResponse struct {
	DatasetRid string            `json:"dataset_rid"`
	Context    ResolutionContext `json:"context"`
	Inherited  InheritedPolicies `json:"inherited"`
	Explicit   []RetentionPolicy `json:"explicit"`
	Effective  *RetentionPolicy  `json:"effective"`
	Conflicts  []PolicyConflict  `json:"conflicts"`
}

// RetentionPreviewQuery mirrors the Rust struct.
type RetentionPreviewQuery struct {
	AsOfDays  *int64
	ProjectID *uuid.UUID
	MarkingID *uuid.UUID
	SpaceID   *uuid.UUID
	OrgID     *uuid.UUID
}

// RetentionPreviewTransaction mirrors the Rust struct.
type RetentionPreviewTransaction struct {
	ID          uuid.UUID  `json:"id"`
	TxType      string     `json:"tx_type"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CommittedAt *time.Time `json:"committed_at"`
	WouldDelete bool       `json:"would_delete"`
	PolicyID    *uuid.UUID `json:"policy_id"`
	PolicyName  *string    `json:"policy_name"`
	Reason      *string    `json:"reason"`
}

// RetentionPreviewFile mirrors the Rust struct.
type RetentionPreviewFile struct {
	ID            uuid.UUID `json:"id"`
	TransactionID uuid.UUID `json:"transaction_id"`
	LogicalPath   string    `json:"logical_path"`
	PhysicalURI   string    `json:"physical_uri"`
	SizeBytes     int64     `json:"size_bytes"`
	PolicyID      uuid.UUID `json:"policy_id"`
	PolicyName    string    `json:"policy_name"`
	Reason        string    `json:"reason"`
}

// RetentionPreviewSummary mirrors the Rust struct.
type RetentionPreviewSummary struct {
	TransactionsTotal       int   `json:"transactions_total"`
	TransactionsWouldDelete int   `json:"transactions_would_delete"`
	FilesTotal              int   `json:"files_total"`
	BytesTotal              int64 `json:"bytes_total"`
}

// RetentionPreviewResponse mirrors the Rust struct.
type RetentionPreviewResponse struct {
	DatasetRid      string                        `json:"dataset_rid"`
	AsOfDays        int64                         `json:"as_of_days"`
	AsOf            time.Time                     `json:"as_of"`
	EffectivePolicy *RetentionPolicy              `json:"effective_policy"`
	Transactions    []RetentionPreviewTransaction `json:"transactions"`
	Files           []RetentionPreviewFile        `json:"files"`
	Summary         RetentionPreviewSummary       `json:"summary"`
	Warnings        []string                      `json:"warnings"`
}

// RetentionPolicySnapshot mirrors the Rust struct used by the
// retention runner. Slim projection of the catalog row.
type RetentionPolicySnapshot struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	IsSystem           bool      `json:"is_system"`
	GracePeriodMinutes int32     `json:"grace_period_minutes"`
	DatasetRid         *string   `json:"dataset_rid"`
	TransactionState   *string   `json:"transaction_state"`
}

// RetentionTarget mirrors the Rust struct.
type RetentionTarget struct {
	DatasetRid    string    `json:"dataset_rid"`
	TransactionID uuid.UUID `json:"transaction_id"`
	FileRefs      []string  `json:"file_refs"`
	Bytes         uint64    `json:"bytes"`
}

// RetentionApplied mirrors the Rust struct returned by `apply_policy`.
type RetentionApplied struct {
	PolicyID                   uuid.UUID `json:"policy_id"`
	TargetsProcessed           int       `json:"targets_processed"`
	FilesMarked                int       `json:"files_marked"`
	BytesFreed                 uint64    `json:"bytes_freed"`
	PhysicalDeletes            int       `json:"physical_deletes"`
	PhysicalDeleteSkippedGrace int       `json:"physical_delete_skipped_grace"`
}

// RetentionAppliedEvent mirrors the Rust struct published to
// `dataset.retention.applied`.
type RetentionAppliedEvent struct {
	PolicyID          uuid.UUID `json:"policy_id"`
	PolicyName        string    `json:"policy_name"`
	DatasetRid        string    `json:"dataset_rid"`
	TransactionID     uuid.UUID `json:"transaction_id"`
	FilesCount        int       `json:"files_count"`
	BytesFreed        uint64    `json:"bytes_freed"`
	PhysicallyDeleted bool      `json:"physically_deleted"`
	OccurredAt        time.Time `json:"occurred_at"`
}

// RunRetentionExecutionRequest executes active retention policies against one dataset.
type RunRetentionExecutionRequest struct {
	DatasetRid         string     `json:"dataset_rid"`
	AsOfDays           int64      `json:"as_of_days,omitempty"`
	RecoveryWindowDays int64      `json:"recovery_window_days,omitempty"`
	DryRun             bool       `json:"dry_run,omitempty"`
	ProjectID          *uuid.UUID `json:"project_id,omitempty"`
	MarkingID          *uuid.UUID `json:"marking_id,omitempty"`
	SpaceID            *uuid.UUID `json:"space_id,omitempty"`
	OrgID              *uuid.UUID `json:"org_id,omitempty"`
}

type RetentionExecutionItem struct {
	ID                        uuid.UUID  `json:"id"`
	RunID                     uuid.UUID  `json:"run_id"`
	PolicyID                  *uuid.UUID `json:"policy_id,omitempty"`
	TransactionID             uuid.UUID  `json:"transaction_id"`
	Action                    string     `json:"action"`
	Reason                    string     `json:"reason"`
	MarkedAt                  *time.Time `json:"marked_at,omitempty"`
	RecoverableUntil          *time.Time `json:"recoverable_until,omitempty"`
	SweptAt                   *time.Time `json:"swept_at,omitempty"`
	RequiresDeleteTransaction bool       `json:"requires_delete_transaction"`
}

type RetentionExecutionRun struct {
	ID                     uuid.UUID                `json:"id"`
	OrgID                  *uuid.UUID               `json:"org_id,omitempty"`
	DatasetRid             string                   `json:"dataset_rid"`
	Status                 string                   `json:"status"`
	DryRun                 bool                     `json:"dry_run"`
	MarkedTransactionCount int                      `json:"marked_transaction_count"`
	SweptTransactionCount  int                      `json:"swept_transaction_count"`
	DeleteTransactionCount int                      `json:"delete_transaction_count"`
	RecoveryWindowDays     int64                    `json:"recovery_window_days"`
	RemediationDeadline    *time.Time               `json:"remediation_deadline,omitempty"`
	IrreversibleAfter      *time.Time               `json:"irreversible_after,omitempty"`
	Warnings               []string                 `json:"warnings"`
	Items                  []RetentionExecutionItem `json:"items"`
	CreatedBy              string                   `json:"created_by"`
	CreatedAt              time.Time                `json:"created_at"`
	CompletedAt            *time.Time               `json:"completed_at,omitempty"`
}
