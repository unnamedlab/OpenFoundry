// Package streambranch holds the wire types and storage contract for
// IRF-8 stream branches. Mirrors
// services/ingestion-replication-service/src/event_streaming/{models,handlers}/branches
// from the Rust service.
package streambranch

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StreamBranch is the persisted shape of a row in
// streaming_stream_branches. Field order + JSON tags mirror the Rust
// `StreamBranch` struct so wire payloads stay byte-exact.
type StreamBranch struct {
	ID              uuid.UUID  `json:"id"`
	StreamID        uuid.UUID  `json:"stream_id"`
	Name            string     `json:"name"`
	ParentBranchID  *uuid.UUID `json:"parent_branch_id"`
	Status          string     `json:"status"`
	HeadSequenceNo  int64      `json:"head_sequence_no"`
	DatasetBranchID *string    `json:"dataset_branch_id"`
	Description     string     `json:"description"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	ArchivedAt      *time.Time `json:"archived_at"`
}

// CreateBranchRequest is the body of POST /streams/{id}/branches.
type CreateBranchRequest struct {
	Name            string     `json:"name"`
	ParentBranchID  *uuid.UUID `json:"parent_branch_id,omitempty"`
	Description     *string    `json:"description,omitempty"`
	DatasetBranchID *string    `json:"dataset_branch_id,omitempty"`
}

// MergeBranchRequest is the body of POST /streams/{id}/branches/{name}:merge.
// `target_branch` defaults to "main" when absent.
type MergeBranchRequest struct {
	TargetBranch *string `json:"target_branch,omitempty"`
}

// MergeBranchResponse is the success body returned by the merge
// endpoint. The `message` mirrors the Rust handler so audit replays
// stay byte-exact.
type MergeBranchResponse struct {
	SourceBranchID    uuid.UUID `json:"source_branch_id"`
	TargetBranchID    uuid.UUID `json:"target_branch_id"`
	MergedSequenceNo  int64     `json:"merged_sequence_no"`
	Message           string    `json:"message"`
}

// ArchiveBranchRequest is the body of POST /streams/{id}/branches/{name}:archive.
// When `commit_cold` is true the handler emits a best-effort HTTP call
// to dataset-versioning-service so the cold copy is committed.
type ArchiveBranchRequest struct {
	CommitCold bool `json:"commit_cold,omitempty"`
}

// Store is the persistence surface used by the branches handler.
// Implemented by repo.Repo and stubbed by tests.
type Store interface {
	StreamExists(ctx context.Context, streamID uuid.UUID) (bool, error)
	ParentBranchBelongsTo(ctx context.Context, parentID, streamID uuid.UUID) (bool, error)
	ListBranches(ctx context.Context, streamID uuid.UUID) ([]StreamBranch, error)
	GetBranchByName(ctx context.Context, streamID uuid.UUID, name string) (*StreamBranch, error)
	CreateBranch(ctx context.Context, streamID uuid.UUID, name, createdBy string, parent *uuid.UUID, datasetBranchID *string, description string) (*StreamBranch, error)
	DeleteBranch(ctx context.Context, branchID uuid.UUID) error
	MergeBranches(ctx context.Context, sourceID, targetID uuid.UUID, mergedSequenceNo int64) error
	ArchiveBranch(ctx context.Context, streamID, branchID uuid.UUID, name string) (*StreamBranch, error)
}

// ColdTierBridge ships archived branches to dataset-versioning-service.
// `CommitCold` is best-effort: implementations log failures rather than
// returning errors so the local archive stays the source of truth.
type ColdTierBridge interface {
	CommitCold(ctx context.Context, branch *StreamBranch, archivedAt time.Time) (accepted bool, err error)
}
