package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ObjectTypeBindingSyncMode mirrors `enum ObjectTypeBindingSyncMode`
// in `libs/ontology-kernel/src/models/object_type_binding.rs`.
// `#[serde(rename_all = "snake_case")]` and `#[sqlx(type_name =
// "text", rename_all = "snake_case")]` so wire / DB tokens are
// snake_case verbatim.
type ObjectTypeBindingSyncMode string

const (
	ObjectTypeBindingSyncModeSnapshot    ObjectTypeBindingSyncMode = "snapshot"
	ObjectTypeBindingSyncModeIncremental ObjectTypeBindingSyncMode = "incremental"
	ObjectTypeBindingSyncModeView        ObjectTypeBindingSyncMode = "view"
)

// AsStr mirrors `impl ObjectTypeBindingSyncMode::as_str(self)`.
func (m ObjectTypeBindingSyncMode) AsStr() string { return string(m) }

// ParseObjectTypeBindingSyncMode mirrors `TryFrom<&str>` — trims
// whitespace and rejects unknown tokens with the exact Rust error
// message format.
func ParseObjectTypeBindingSyncMode(s string) (ObjectTypeBindingSyncMode, error) {
	switch strings.TrimSpace(s) {
	case "snapshot":
		return ObjectTypeBindingSyncModeSnapshot, nil
	case "incremental":
		return ObjectTypeBindingSyncModeIncremental, nil
	case "view":
		return ObjectTypeBindingSyncModeView, nil
	default:
		return "", fmt.Errorf(
			"sync_mode '%s' is not supported; expected one of: snapshot, incremental, view",
			s,
		)
	}
}

// ObjectTypeBindingPropertyMapping mirrors `struct
// ObjectTypeBindingPropertyMapping`.
type ObjectTypeBindingPropertyMapping struct {
	SourceField    string `json:"source_field"`
	TargetProperty string `json:"target_property"`
}

// ObjectTypeBindingRow mirrors `struct ObjectTypeBindingRow` —
// persisted shape with raw JSONB columns kept as `json.RawMessage`.
type ObjectTypeBindingRow struct {
	ID                  uuid.UUID       `db:"id"`
	ObjectTypeID        uuid.UUID       `db:"object_type_id"`
	DatasetID           uuid.UUID       `db:"dataset_id"`
	DatasetBranch       *string         `db:"dataset_branch"`
	DatasetVersion      *int32          `db:"dataset_version"`
	PrimaryKeyColumn    string          `db:"primary_key_column"`
	PropertyMapping     json.RawMessage `db:"property_mapping"`
	SyncMode            string          `db:"sync_mode"`
	DefaultMarking      string          `db:"default_marking"`
	PreviewLimit        int32           `db:"preview_limit"`
	OwnerID             uuid.UUID       `db:"owner_id"`
	LastMaterializedAt  *time.Time      `db:"last_materialized_at"`
	LastRunStatus       *string         `db:"last_run_status"`
	LastRunSummary      json.RawMessage `db:"last_run_summary"`
	CreatedAt           time.Time       `db:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at"`
}

// ObjectTypeBinding mirrors `struct ObjectTypeBinding` (public API
// representation).
type ObjectTypeBinding struct {
	ID                 uuid.UUID                          `json:"id"`
	ObjectTypeID       uuid.UUID                          `json:"object_type_id"`
	DatasetID          uuid.UUID                          `json:"dataset_id"`
	DatasetBranch      *string                            `json:"dataset_branch"`
	DatasetVersion     *int32                             `json:"dataset_version"`
	PrimaryKeyColumn   string                             `json:"primary_key_column"`
	PropertyMapping    []ObjectTypeBindingPropertyMapping `json:"property_mapping"`
	SyncMode           ObjectTypeBindingSyncMode          `json:"sync_mode"`
	DefaultMarking     string                             `json:"default_marking"`
	PreviewLimit       int32                              `json:"preview_limit"`
	OwnerID            uuid.UUID                          `json:"owner_id"`
	LastMaterializedAt *time.Time                         `json:"last_materialized_at"`
	LastRunStatus      *string                            `json:"last_run_status"`
	LastRunSummary     json.RawMessage                    `json:"last_run_summary"`
	CreatedAt          time.Time                          `json:"created_at"`
	UpdatedAt          time.Time                          `json:"updated_at"`
}

// IntoBinding mirrors `TryFrom<ObjectTypeBindingRow> for
// ObjectTypeBinding`. Returns the embedded JSON parse error or
// sync_mode validation error verbatim.
func (row ObjectTypeBindingRow) IntoBinding() (ObjectTypeBinding, error) {
	mapping := []ObjectTypeBindingPropertyMapping{}
	if len(row.PropertyMapping) > 0 {
		if err := json.Unmarshal(row.PropertyMapping, &mapping); err != nil {
			return ObjectTypeBinding{}, fmt.Errorf("failed to decode property_mapping: %s", err)
		}
	}
	mode, err := ParseObjectTypeBindingSyncMode(row.SyncMode)
	if err != nil {
		return ObjectTypeBinding{}, err
	}
	return ObjectTypeBinding{
		ID:                 row.ID,
		ObjectTypeID:       row.ObjectTypeID,
		DatasetID:          row.DatasetID,
		DatasetBranch:      row.DatasetBranch,
		DatasetVersion:     row.DatasetVersion,
		PrimaryKeyColumn:   row.PrimaryKeyColumn,
		PropertyMapping:    mapping,
		SyncMode:           mode,
		DefaultMarking:     row.DefaultMarking,
		PreviewLimit:       row.PreviewLimit,
		OwnerID:            row.OwnerID,
		LastMaterializedAt: row.LastMaterializedAt,
		LastRunStatus:      row.LastRunStatus,
		LastRunSummary:     row.LastRunSummary,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}, nil
}

// CreateObjectTypeBindingRequest mirrors the same struct in Rust.
// `property_mapping` is `#[serde(default)]` so missing key decodes
// to `[]`.
type CreateObjectTypeBindingRequest struct {
	DatasetID        uuid.UUID                          `json:"dataset_id"`
	DatasetBranch    *string                            `json:"dataset_branch,omitempty"`
	DatasetVersion   *int32                             `json:"dataset_version,omitempty"`
	PrimaryKeyColumn string                             `json:"primary_key_column"`
	PropertyMapping  []ObjectTypeBindingPropertyMapping `json:"property_mapping"`
	SyncMode         ObjectTypeBindingSyncMode          `json:"sync_mode"`
	DefaultMarking   *string                            `json:"default_marking,omitempty"`
	PreviewLimit     *int32                             `json:"preview_limit,omitempty"`
}

// UnmarshalJSON applies the `#[serde(default)]` for property_mapping.
func (r *CreateObjectTypeBindingRequest) UnmarshalJSON(b []byte) error {
	type alias CreateObjectTypeBindingRequest
	if err := json.Unmarshal(b, (*alias)(r)); err != nil {
		return err
	}
	if r.PropertyMapping == nil {
		r.PropertyMapping = []ObjectTypeBindingPropertyMapping{}
	}
	return nil
}

// UpdateObjectTypeBindingRequest mirrors `struct UpdateObjectTypeBindingRequest`.
type UpdateObjectTypeBindingRequest struct {
	DatasetBranch    *string                              `json:"dataset_branch,omitempty"`
	DatasetVersion   *int32                               `json:"dataset_version,omitempty"`
	PrimaryKeyColumn *string                              `json:"primary_key_column,omitempty"`
	PropertyMapping  *[]ObjectTypeBindingPropertyMapping  `json:"property_mapping,omitempty"`
	SyncMode         *ObjectTypeBindingSyncMode           `json:"sync_mode,omitempty"`
	DefaultMarking   *string                              `json:"default_marking,omitempty"`
	PreviewLimit     *int32                               `json:"preview_limit,omitempty"`
}

// MaterializeBindingRequest mirrors `struct MaterializeBindingRequest`.
// `dry_run` is `#[serde(default)]` so absent → false.
type MaterializeBindingRequest struct {
	DatasetBranch  *string `json:"dataset_branch,omitempty"`
	DatasetVersion *int32  `json:"dataset_version,omitempty"`
	Limit          *int32  `json:"limit,omitempty"`
	DryRun         bool    `json:"dry_run"`
}

// MaterializeBindingResponse mirrors `struct MaterializeBindingResponse`.
// `error_details` is `#[serde(skip_serializing_if = "Vec::is_empty")]`
// — `omitempty` honours that for `[]json.RawMessage` zero length.
type MaterializeBindingResponse struct {
	BindingID    uuid.UUID         `json:"binding_id"`
	Status       string            `json:"status"`
	RowsRead     int32             `json:"rows_read"`
	Inserted     int32             `json:"inserted"`
	Updated      int32             `json:"updated"`
	Skipped      int32             `json:"skipped"`
	Errors       int32             `json:"errors"`
	DryRun       bool              `json:"dry_run"`
	ErrorDetails []json.RawMessage `json:"error_details,omitempty"`
}

// ListObjectTypeBindingsResponse mirrors `struct ListObjectTypeBindingsResponse`.
type ListObjectTypeBindingsResponse struct {
	Data []ObjectTypeBinding `json:"data"`
}
