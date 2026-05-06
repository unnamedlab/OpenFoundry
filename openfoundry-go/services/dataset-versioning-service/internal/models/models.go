// Package models holds wire types for dataset-versioning-service.
//
// Foundation slice scope: datasets table only. dataset_versions,
// dataset_branches, dataset_quality, lint, views, files, health,
// retention_worker, foundry-model surface land in follow-up slices —
// 25k LOC of Rust source, deferred per the >1500 LOC budget.
package models

import (
	"time"

	"github.com/google/uuid"
)

// ListResponse is the canonical envelope.
type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// Dataset mirrors the `datasets` row.
type Dataset struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Format         string    `json:"format"`
	StoragePath    string    `json:"storage_path"`
	SizeBytes      int64     `json:"size_bytes"`
	RowCount       int64     `json:"row_count"`
	OwnerID        uuid.UUID `json:"owner_id"`
	Tags           []string  `json:"tags"`
	CurrentVersion int32     `json:"current_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreateDatasetRequest is the body of POST /api/v1/datasets.
type CreateDatasetRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Format      *string  `json:"format,omitempty"`
	StoragePath string   `json:"storage_path"`
	Tags        []string `json:"tags,omitempty"`
}

// UpdateDatasetRequest mirrors PATCH semantics.
type UpdateDatasetRequest struct {
	Description *string  `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	SizeBytes   *int64   `json:"size_bytes,omitempty"`
	RowCount    *int64   `json:"row_count,omitempty"`
}
