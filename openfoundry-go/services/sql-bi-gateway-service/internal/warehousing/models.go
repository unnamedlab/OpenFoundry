// Package warehousing ports services/sql-bi-gateway-service/src/warehousing/.
//
// Persistent models and HTTP handlers for the warehousing bounded
// context, ported verbatim from the retired sql-warehousing-service
// when the source service was absorbed by the SQL/BI gateway
// (ADR-0030, S8). Schema lives in
// migrations/20260427090000_sql_warehousing_foundation.sql.
package warehousing

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WarehouseJob is a persistent SQL warehousing job: large-scale SQL
// execution against the warehouse with intermediate persistence
// guarantees and lineage back to source datasets.
type WarehouseJob struct {
	ID              uuid.UUID       `json:"id"`
	Slug            string          `json:"slug"`
	SQLText         string          `json:"sql_text"`
	Status          string          `json:"status"`
	SourceDatasets  json.RawMessage `json:"source_datasets"`
	TargetDatasetID *uuid.UUID      `json:"target_dataset_id"`
	TargetStorageID *uuid.UUID      `json:"target_storage_id"`
	SubmittedBy     *uuid.UUID      `json:"submitted_by"`
	ErrorMessage    *string         `json:"error_message"`
	StartedAt       *time.Time      `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// SubmitWarehouseJobRequest is the body for POST /api/v1/warehouse/jobs.
type SubmitWarehouseJobRequest struct {
	Slug            string      `json:"slug"`
	SQLText         string      `json:"sql_text"`
	SourceDatasets  []uuid.UUID `json:"source_datasets,omitempty"`
	TargetDatasetID *uuid.UUID  `json:"target_dataset_id,omitempty"`
	TargetStorageID *uuid.UUID  `json:"target_storage_id,omitempty"`
}

// WarehouseTransformation is a reusable SQL transformation declaration
// (templated SQL with bindings).
type WarehouseTransformation struct {
	ID          uuid.UUID       `json:"id"`
	Slug        string          `json:"slug"`
	Description *string         `json:"description"`
	SQLTemplate string          `json:"sql_template"`
	Bindings    json.RawMessage `json:"bindings"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// RegisterTransformationRequest is the body for
// POST /api/v1/warehouse/transformations.
type RegisterTransformationRequest struct {
	Slug        string          `json:"slug"`
	Description *string         `json:"description,omitempty"`
	SQLTemplate string          `json:"sql_template"`
	Bindings    json.RawMessage `json:"bindings,omitempty"`
}

// WarehouseStorageArtifact is an intermediate storage artifact
// produced by a warehouse job (parquet, table, materialized view).
type WarehouseStorageArtifact struct {
	ID           uuid.UUID  `json:"id"`
	JobID        *uuid.UUID `json:"job_id"`
	Slug         string     `json:"slug"`
	ArtifactKind string     `json:"artifact_kind"`
	StorageURI   string     `json:"storage_uri"`
	ByteSize     *int64     `json:"byte_size"`
	RowCount     *int64     `json:"row_count"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
