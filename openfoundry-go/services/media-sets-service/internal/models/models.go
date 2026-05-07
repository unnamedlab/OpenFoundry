// Package models holds wire types for media-sets-service.
package models

import (
	"encoding/json"
	"time"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// MediaSet mirrors `media_sets` rows. RID-addressed, schema-locked.
type MediaSet struct {
	RID                string    `json:"rid"`
	ProjectRID         string    `json:"project_rid"`
	Name               string    `json:"name"`
	Schema             string    `json:"schema"`
	AllowedMimeTypes   []string  `json:"allowed_mime_types"`
	TransactionPolicy  string    `json:"transaction_policy"`
	RetentionSeconds   int64     `json:"retention_seconds"`
	Virtual            bool      `json:"virtual"`
	SourceRID          *string   `json:"source_rid"`
	Markings           []string  `json:"markings"`
	CreatedAt          time.Time `json:"created_at"`
	CreatedBy          string    `json:"created_by"`
}

// CreateMediaSetRequest is POST /api/v1/media-sets.
type CreateMediaSetRequest struct {
	ProjectRID        string   `json:"project_rid"`
	Name              string   `json:"name"`
	Schema            string   `json:"schema"`
	AllowedMimeTypes  []string `json:"allowed_mime_types,omitempty"`
	TransactionPolicy *string  `json:"transaction_policy,omitempty"`
	RetentionSeconds  *int64   `json:"retention_seconds,omitempty"`
	Virtual           *bool    `json:"virtual,omitempty"`
	SourceRID         *string  `json:"source_rid,omitempty"`
	Markings          []string `json:"markings,omitempty"`
}

// UpdateMediaSetRequest mirrors PATCH semantics.
type UpdateMediaSetRequest struct {
	AllowedMimeTypes []string `json:"allowed_mime_types,omitempty"`
	RetentionSeconds *int64   `json:"retention_seconds,omitempty"`
	Markings         []string `json:"markings,omitempty"`
}

// PersistencePolicy mirrors the Rust enum (SCREAMING_SNAKE_CASE
// strings on the wire). See models/access_pattern.rs.
type PersistencePolicy string

const (
	PersistenceRecompute PersistencePolicy = "RECOMPUTE"
	PersistencePersist   PersistencePolicy = "PERSIST"
	PersistenceCacheTTL  PersistencePolicy = "CACHE_TTL"
)

// IsValid reports whether p is one of the three documented policies.
func (p PersistencePolicy) IsValid() bool {
	switch p {
	case PersistenceRecompute, PersistencePersist, PersistenceCacheTTL:
		return true
	}
	return false
}

// AccessPattern mirrors `media_set_access_patterns` rows.
type AccessPattern struct {
	ID           string          `json:"id"`
	MediaSetRID  string          `json:"media_set_rid"`
	Kind         string          `json:"kind"`
	Params       json.RawMessage `json:"params"`
	Persistence  string          `json:"persistence"`
	TTLSeconds   int64           `json:"ttl_seconds"`
	CreatedAt    time.Time       `json:"created_at"`
	CreatedBy    string          `json:"created_by"`
}

// RegisterAccessPatternRequest is POST /media-sets/{rid}/access-patterns.
type RegisterAccessPatternRequest struct {
	Kind        string            `json:"kind"`
	Params      json.RawMessage   `json:"params,omitempty"`
	Persistence PersistencePolicy `json:"persistence,omitempty"`
	TTLSeconds  *int64            `json:"ttl_seconds,omitempty"`
}

// AccessPatternRunResponse mirrors the Rust struct of the same name.
// Fields are omitempty per the Rust skip_serializing_if = Option::is_none.
type AccessPatternRunResponse struct {
	PatternID            string `json:"pattern_id"`
	Kind                 string `json:"kind"`
	ItemRID              string `json:"item_rid"`
	Persistence          string `json:"persistence"`
	CacheHit             bool   `json:"cache_hit"`
	ComputeSeconds       uint64 `json:"compute_seconds"`
	OutputMimeType       string `json:"output_mime_type"`
	OutputStorageURI     string `json:"output_storage_uri,omitempty"`
	OutputBytesBase64    string `json:"output_bytes_base64,omitempty"`
	NotImplementedReason string `json:"not_implemented_reason,omitempty"`
}

// MediaItem mirrors `media_items` rows. Fields ported 1:1 from the
// Rust struct of the same name in services/media-sets-service/src/
// models/media_item.rs.
type MediaItem struct {
	RID              string          `json:"rid"`
	MediaSetRID      string          `json:"media_set_rid"`
	Branch           string          `json:"branch"`
	TransactionRID   string          `json:"transaction_rid"`
	Path             string          `json:"path"`
	MimeType         string          `json:"mime_type"`
	SizeBytes        int64           `json:"size_bytes"`
	SHA256           string          `json:"sha256"`
	Metadata         json.RawMessage `json:"metadata"`
	StorageURI       string          `json:"storage_uri"`
	DeduplicatedFrom *string         `json:"deduplicated_from,omitempty"`
	DeletedAt        *time.Time      `json:"deleted_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	Markings         []string        `json:"markings"`
}

// PresignedUploadRequest is POST /api/v1/media-sets/{rid}/items.
type PresignedUploadRequest struct {
	Path             string  `json:"path"`
	MimeType         string  `json:"mime_type"`
	SizeBytes        *int64  `json:"size_bytes,omitempty"`
	SHA256           *string `json:"sha256,omitempty"`
	Branch           *string `json:"branch,omitempty"`
	TransactionRID   *string `json:"transaction_rid,omitempty"`
	ExpiresInSeconds *uint64 `json:"expires_in_seconds,omitempty"`
}

// PresignedURLBody is the response body for upload + download endpoints.
type PresignedURLBody struct {
	URL       string            `json:"url"`
	ExpiresAt time.Time         `json:"expires_at"`
	Headers   map[string]string `json:"headers,omitempty"`
	Item      *MediaItem        `json:"item,omitempty"`
}

// RegisterVirtualItemRequest is POST /api/v1/media-sets/{rid}/virtual-items.
type RegisterVirtualItemRequest struct {
	PhysicalPath string  `json:"physical_path"`
	ItemPath     string  `json:"item_path"`
	MimeType     *string `json:"mime_type,omitempty"`
	SizeBytes    *int64  `json:"size_bytes,omitempty"`
	Branch       *string `json:"branch,omitempty"`
	SHA256       *string `json:"sha256,omitempty"`
}

// PatchItemMarkingsRequest is PATCH /api/v1/items/{rid}/markings.
type PatchItemMarkingsRequest struct {
	Markings []string `json:"markings"`
}

// ── Branches ──────────────────────────────────────────────────────

// MediaSetBranch mirrors `media_set_branches` rows after migration
// 0006_branching.sql. branch_rid is a generated stored column; the
// authoritative key is (media_set_rid, branch_name).
type MediaSetBranch struct {
	MediaSetRID        string    `json:"media_set_rid"`
	BranchName         string    `json:"branch_name"`
	BranchRID          string    `json:"branch_rid"`
	ParentBranchRID    *string   `json:"parent_branch_rid,omitempty"`
	HeadTransactionRID *string   `json:"head_transaction_rid,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	CreatedBy          string    `json:"created_by"`
}

// IsRoot reports whether the branch has no parent.
func (b *MediaSetBranch) IsRoot() bool { return b.ParentBranchRID == nil }

// CreateBranchRequest is POST /api/v1/media-sets/{rid}/branches.
type CreateBranchRequest struct {
	Name               string  `json:"name"`
	FromBranch         *string `json:"from_branch,omitempty"`
	FromTransactionRID *string `json:"from_transaction_rid,omitempty"`
}

// MergeResolution mirrors the Rust enum.
type MergeResolution string

const (
	MergeLatestWins     MergeResolution = "LATEST_WINS"
	MergeFailOnConflict MergeResolution = "FAIL_ON_CONFLICT"
)

// IsValid reports whether m is one of the documented strategies.
func (m MergeResolution) IsValid() bool {
	switch m {
	case MergeLatestWins, MergeFailOnConflict:
		return true
	}
	return false
}

// MergeBranchRequest is POST /api/v1/media-sets/{rid}/branches/{name}/merge.
type MergeBranchRequest struct {
	TargetBranch string          `json:"target_branch"`
	Resolution   MergeResolution `json:"resolution,omitempty"`
}

// ResetBranchResponse mirrors the Rust ResetBranchResponse struct.
type ResetBranchResponse struct {
	Branch           MediaSetBranch `json:"branch"`
	ItemsSoftDeleted int64          `json:"items_soft_deleted"`
}

// MergeBranchResponse mirrors the Rust MergeBranchResponse struct.
type MergeBranchResponse struct {
	SourceBranch     string `json:"source_branch"`
	TargetBranch     string `json:"target_branch"`
	Resolution       string `json:"resolution"`
	PathsCopied      int64  `json:"paths_copied"`
	PathsOverwritten int64  `json:"paths_overwritten"`
	PathsSkipped     int64  `json:"paths_skipped"`
}

// MergeConflictBody is the 409 envelope when FAIL_ON_CONFLICT trips.
type MergeConflictBody struct {
	Error         string   `json:"error"`
	ConflictPaths []string `json:"conflict_paths"`
}

// ── Transactions ──────────────────────────────────────────────────

// TransactionState is the SCREAMING_SNAKE_CASE enum carried in the
// `state` column. Mirrors the Rust enum.
type TransactionState string

const (
	TxStateOpen      TransactionState = "OPEN"
	TxStateCommitted TransactionState = "COMMITTED"
	TxStateAborted   TransactionState = "ABORTED"
)

// IsTerminal reports whether s is a closed state.
func (s TransactionState) IsTerminal() bool {
	return s == TxStateCommitted || s == TxStateAborted
}

// WriteMode mirrors Foundry's incremental write modes. MODIFY is the
// default; REPLACE is rejected on transactionless sets.
type WriteMode string

const (
	WriteModeModify  WriteMode = "MODIFY"
	WriteModeReplace WriteMode = "REPLACE"
)

// IsValid reports whether m is one of the documented write modes.
func (m WriteMode) IsValid() bool {
	switch m {
	case WriteModeModify, WriteModeReplace:
		return true
	}
	return false
}

// MediaSetTransaction mirrors `media_set_transactions` rows.
type MediaSetTransaction struct {
	RID         string     `json:"rid"`
	MediaSetRID string     `json:"media_set_rid"`
	Branch      string     `json:"branch"`
	State       string     `json:"state"`
	WriteMode   string     `json:"write_mode,omitempty"`
	OpenedAt    time.Time  `json:"opened_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	OpenedBy    string     `json:"opened_by"`
}

// OpenTransactionRequest is POST /api/v1/media-sets/{rid}/transactions.
type OpenTransactionRequest struct {
	Branch    *string    `json:"branch,omitempty"`
	WriteMode *WriteMode `json:"write_mode,omitempty"`
}

// TransactionHistoryEntry is one row of GET /api/v1/media-sets/{rid}/transactions.
type TransactionHistoryEntry struct {
	RID            string     `json:"rid"`
	MediaSetRID    string     `json:"media_set_rid"`
	Branch         string     `json:"branch"`
	State          string     `json:"state"`
	WriteMode      string     `json:"write_mode"`
	OpenedAt       time.Time  `json:"opened_at"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	OpenedBy       string     `json:"opened_by"`
	ItemsAdded     int64      `json:"items_added"`
	ItemsModified  int64      `json:"items_modified"`
	ItemsDeleted   int64      `json:"items_deleted"`
}
